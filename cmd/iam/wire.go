// Package main contiene el wire-up (DI manual) del bounded context IAM.
//
// wire.go centraliza la construcción del árbol de dependencias completo.
// Orden de construcción (de adentro hacia afuera):
//  1. Infraestructura (pool PG, bus NATS)
//  2. Repositorios IAM (adaptadores de salida)
//  3. Domain Services IAM
//  4. Command Handlers IAM
//  5. Repositorios Patient (necesarios para el subscriber de provisión)
//  6. Domain Services Patient
//  7. Command Handlers Patient (RegisterPatientHandler para el subscriber)
//  8. Subscriber NATS: user.registered → crea Patient + vincula linked_id
//  9. Adaptadores de entrada HTTP
//
// 10. Router HTTP + Server
package main

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	// IAM — application
	iamcmd "github.com/juantevez/odontoagenda/context/iam/application/command"

	// IAM — domain
	iamsvc "github.com/juantevez/odontoagenda/context/iam/domain/service"

	// IAM — infrastructure
	iamhttp "github.com/juantevez/odontoagenda/context/iam/infrastructure/http"
	iamnats "github.com/juantevez/odontoagenda/context/iam/infrastructure/nats"
	iampostgres "github.com/juantevez/odontoagenda/context/iam/infrastructure/postgres"

	// Patient — application (solo RegisterPatientHandler para el subscriber)
	patientcmd "github.com/juantevez/odontoagenda/context/patient/application/command"

	// Patient — domain
	patientsvc "github.com/juantevez/odontoagenda/context/patient/domain/service"

	// Patient — infrastructure
	patientpostgres "github.com/juantevez/odontoagenda/context/patient/infrastructure/postgres"

	"github.com/juantevez/odontoagenda/pkg/events"
	"github.com/juantevez/odontoagenda/pkg/middleware"
)

// ── app ───────────────────────────────────────────────────────────

// app agrupa todo lo que el servidor necesita en runtime.
type app struct {
	httpServer          *http.Server
	eventBus            *events.NATSBus
	pgPool              *pgxpool.Pool
	patientProvisionSub *iamnats.PatientProvisionSubscriber
}

// ── initApp ───────────────────────────────────────────────────────

// initApp construye el árbol completo de dependencias del BC IAM.
func initApp(cfg config) (*app, error) {

	// ── 1. Infraestructura ────────────────────────────────────────

	pool, err := pgxpool.New(context.Background(), cfg.DatabaseURL)
	if err != nil {
		return nil, fmt.Errorf("wire: pgxpool: %w", err)
	}
	if err := waitForPostgres(pool, 30*time.Second); err != nil {
		return nil, fmt.Errorf("wire: postgres ping: %w", err)
	}

	bus, err := events.New(events.Config{URL: cfg.NATSUrl})
	if err != nil {
		return nil, fmt.Errorf("wire: nats: %w", err)
	}

	// ── 2. Repositorios IAM ───────────────────────────────────────

	userRepo := iampostgres.NewUserPostgresRepository(pool)
	familyRepo := iampostgres.NewFamilyPostgresRepository(pool)

	// ── 3. Domain Services IAM ────────────────────────────────────

	tokenSvc := iamsvc.NewTokenService(iamsvc.TokenConfig{
		SecretKey:       []byte(cfg.JWTSecret),
		Issuer:          cfg.JWTIssuer,
		AccessTokenTTL:  time.Duration(cfg.AccessTokenTTLMin) * time.Minute,
		RefreshTokenTTL: time.Duration(cfg.RefreshTokenTTLDays) * 24 * time.Hour,
	})

	// ── 4. Command Handlers IAM ───────────────────────────────────

	registerHandler := iamcmd.NewRegisterUserHandler(userRepo, familyRepo, bus)
	loginHandler := iamcmd.NewLoginHandler(userRepo, familyRepo, tokenSvc)
	refreshHandler := iamcmd.NewRefreshTokensHandler(userRepo, familyRepo, tokenSvc)
	logoutHandler := iamcmd.NewLogoutHandler(userRepo, bus)

	// ── 5. Repositorios Patient ───────────────────────────────────
	// IAM necesita crear el Patient al registrar un usuario con rol 'paciente'.
	// Sigue siendo responsabilidad del BC IAM orquestar esta provisión
	// porque es parte del flujo de registro de cuenta.

	patientRepo := patientpostgres.NewPatientPostgresRepository(pool)
	patientHistory := patientpostgres.NewCoverageHistoryPostgresRepository(pool)
	_ = patientHistory // usado en cmd/patient; aquí solo necesitamos patientRepo

	// ── 6. Domain Services Patient ────────────────────────────────

	duplicateDetector := patientsvc.NewDuplicateDetector(patientRepo)

	// ── 7. Command Handlers Patient ───────────────────────────────

	registerPatientH := patientcmd.NewRegisterPatientHandler(
		patientRepo, duplicateDetector, bus,
	)

	// ── 8. Subscriber NATS: user.registered → provisión Patient ──
	// Escucha el evento que el propio BC IAM publica al registrar un usuario.
	// Cuando role = 'paciente':
	//   a) Crea el registro Patient en patient.patients.
	//   b) Actualiza iam.users.linked_id con el nuevo patient_id.
	// Esto cierra el bug de diseño: un paciente que se auto-registra
	// queda automáticamente vinculado a su ficha clínica.

	patientProvisionSub := iamnats.NewPatientProvisionSubscriber(
		bus, registerPatientH, userRepo,
	)
	if err := patientProvisionSub.RegisterAll(context.Background()); err != nil {
		return nil, fmt.Errorf("wire: patient provision subscriber: %w", err)
	}

	// ── 9. Adaptadores de entrada HTTP ────────────────────────────

	jwtCfg := middleware.JWTConfig{
		SecretKey: []byte(cfg.JWTSecret),
		Issuer:    cfg.JWTIssuer,
	}

	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.Logger(newLogger()))
	r.Use(middleware.Recoverer(newLogger()))

	r.Get("/health", healthHandler(cfg.ServiceName))

	r.Route("/api/v1", func(r chi.Router) {
		iamhttp.RegisterRoutes(r, jwtCfg,
			registerHandler,
			loginHandler,
			refreshHandler,
			logoutHandler,
		)
	})

	// ── 10. Servidor HTTP ─────────────────────────────────────────

	srv := &http.Server{
		Addr:         ":" + cfg.Port,
		Handler:      r,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	return &app{
		httpServer:          srv,
		eventBus:            bus,
		pgPool:              pool,
		patientProvisionSub: patientProvisionSub,
	}, nil
}

// ── close ─────────────────────────────────────────────────────────

func (a *app) close() {
	if a.eventBus != nil {
		_ = a.eventBus.Close()
	}
	if a.pgPool != nil {
		a.pgPool.Close()
	}
}

// ── waitForPostgres ───────────────────────────────────────────────

func waitForPostgres(pool *pgxpool.Pool, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	for {
		if err := pool.Ping(ctx); err == nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(2 * time.Second):
			// reintentar
		}
	}
}
