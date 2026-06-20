// Package main contiene el wire-up del bounded context Patient.
package main

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	patientcmd "github.com/juantevez/odontoagenda/context/patient/application/command"
	patientqry "github.com/juantevez/odontoagenda/context/patient/application/query"
	patientsvc "github.com/juantevez/odontoagenda/context/patient/domain/service"
	patienthttp "github.com/juantevez/odontoagenda/context/patient/infrastructure/http"
	patientnats "github.com/juantevez/odontoagenda/context/patient/infrastructure/nats"
	patientpostgres "github.com/juantevez/odontoagenda/context/patient/infrastructure/postgres"
	"github.com/juantevez/odontoagenda/pkg/events"
	"github.com/juantevez/odontoagenda/pkg/middleware"
)

type app struct {
	httpServer  *http.Server
	eventBus    *events.NATSBus
	pgPool      *pgxpool.Pool
	subscribers *patientnats.PatientEventSubscriber
}

func initApp(cfg config) (*app, error) {

	// ── 1. Infraestructura ────────────────────────────────────────

	pool, err := pgxpool.New(context.Background(), cfg.DatabaseURL)
	if err != nil {
		return nil, fmt.Errorf("wire: pgxpool: %w", err)
	}
	if err := pool.Ping(context.Background()); err != nil {
		return nil, fmt.Errorf("wire: postgres ping: %w", err)
	}

	bus, err := events.New(events.Config{URL: cfg.NATSUrl})
	if err != nil {
		return nil, fmt.Errorf("wire: nats: %w", err)
	}

	// ── 2. Repositorios ───────────────────────────────────────────

	patientRepo := patientpostgres.NewPatientPostgresRepository(pool)
	historyRepo := patientpostgres.NewCoverageHistoryPostgresRepository(pool)

	// ── 3. Domain Services ────────────────────────────────────────

	duplicateDetector := patientsvc.NewDuplicateDetector(patientRepo)

	// ── 4. Command Handlers ───────────────────────────────────────

	registerHandler := patientcmd.NewRegisterPatientHandler(patientRepo, duplicateDetector, bus)
	addCoverageHandler := patientcmd.NewAddCoverageHandler(patientRepo, historyRepo, bus)
	addAlertHandler := patientcmd.NewAddMedicalAlertHandler(patientRepo, bus)
	mergeHandler := patientcmd.NewMergePatientsHandler(patientRepo, bus)
	recordVisitHandler := patientcmd.NewRecordCompletedVisitHandler(patientRepo)

	// ── 5. Query Handlers ─────────────────────────────────────────

	getByIDHandler := patientqry.NewGetPatientByIDHandler(patientRepo)
	searchHandler := patientqry.NewSearchPatientsHandler(patientRepo)
	forBookingHandler := patientqry.NewGetPatientForBookingHandler(patientRepo)

	// ── 6. Subscribers NATS (adaptadores de entrada async) ────────

	subscribers := patientnats.NewPatientEventSubscriber(bus, recordVisitHandler)

	// Registrar consumers al iniciar (antes del servidor HTTP).
	if err := subscribers.RegisterAll(context.Background()); err != nil {
		return nil, fmt.Errorf("wire: nats subscribers: %w", err)
	}

	// ── 7. Adaptadores de entrada: HTTP ───────────────────────────

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
		patienthttp.RegisterRoutes(r, jwtCfg,
			registerHandler,
			addCoverageHandler,
			addAlertHandler,
			mergeHandler,
			getByIDHandler,
			searchHandler,
			forBookingHandler,
		)
	})

	srv := &http.Server{
		Addr:         ":" + cfg.Port,
		Handler:      r,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	return &app{
		httpServer:  srv,
		eventBus:    bus,
		pgPool:      pool,
		subscribers: subscribers,
	}, nil
}

func (a *app) close() {
	if a.eventBus != nil {
		_ = a.eventBus.Close()
	}
	if a.pgPool != nil {
		a.pgPool.Close()
	}
}
