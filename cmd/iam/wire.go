// Package main contiene el wire-up (DI manual) del bounded context IAM.
//
// wire.go centraliza la construcción del árbol de dependencias completo.
// Sigue el mismo patrón del posnet-backend: main.go queda limpio,
// wire.go es el único lugar donde se conocen las implementaciones concretas.
//
// Orden de construcción (de adentro hacia afuera, siguiendo la arquitectura hexagonal):
//  1. Infraestructura (pool PG, bus NATS)
//  2. Repositorios (adaptadores de salida)
//  3. Domain Services
//  4. Application Services (command/query handlers)
//  5. Adaptadores de entrada (HTTP handlers, NATS subscribers)
//  6. Router HTTP
package main

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	iamcmd "github.com/juantevez/odontoagenda/context/iam/application/command"
	iamsvc "github.com/juantevez/odontoagenda/context/iam/domain/service"
	iamhttp "github.com/juantevez/odontoagenda/context/iam/infrastructure/http"
	iampostgres "github.com/juantevez/odontoagenda/context/iam/infrastructure/postgres"
	"github.com/juantevez/odontoagenda/pkg/events"
	"github.com/juantevez/odontoagenda/pkg/middleware"
)

// app agrupa todo lo que el servidor necesita en runtime.
type app struct {
	httpServer *http.Server
	eventBus   *events.NATSBus
	pgPool     *pgxpool.Pool
}

// initApp construye el árbol completo de dependencias del BC IAM.
// Es el único lugar en el proyecto donde aparecen los tipos concretos
// de infraestructura (pgxpool, NATSBus, repositorios Postgres, etc.).
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

	// ── 2. Repositorios (adaptadores de salida) ───────────────────

	userRepo := iampostgres.NewUserPostgresRepository(pool)
	familyRepo := iampostgres.NewFamilyPostgresRepository(pool)

	// ── 3. Domain Services ────────────────────────────────────────

	tokenSvc := iamsvc.NewTokenService(iamsvc.TokenConfig{
		SecretKey:       []byte(cfg.JWTSecret),
		Issuer:          cfg.JWTIssuer,
		AccessTokenTTL:  time.Duration(cfg.AccessTokenTTLMin) * time.Minute,
		RefreshTokenTTL: time.Duration(cfg.RefreshTokenTTLDays) * 24 * time.Hour,
	})

	// ── 4. Command Handlers (application layer) ───────────────────

	registerHandler := iamcmd.NewRegisterUserHandler(userRepo, familyRepo, bus)
	loginHandler := iamcmd.NewLoginHandler(userRepo, familyRepo, tokenSvc)
	refreshHandler := iamcmd.NewRefreshTokensHandler(userRepo, familyRepo, tokenSvc)
	logoutHandler := iamcmd.NewLogoutHandler(userRepo, bus)

	// ── 5. Adaptadores de entrada: HTTP ───────────────────────────

	jwtCfg := middleware.JWTConfig{
		SecretKey: []byte(cfg.JWTSecret),
		Issuer:    cfg.JWTIssuer,
	}

	r := chi.NewRouter()

	// Middlewares globales
	r.Use(middleware.RequestID)
	r.Use(middleware.Logger(newLogger()))
	r.Use(middleware.Recoverer(newLogger()))

	// Health check (sin auth, sin BC)
	r.Get("/health", healthHandler(cfg.ServiceName))

	// Rutas del BC IAM
	r.Route("/api/v1", func(r chi.Router) {
		iamhttp.RegisterRoutes(r, jwtCfg,
			registerHandler,
			loginHandler,
			refreshHandler,
			logoutHandler,
		)
	})

	// ── 6. Servidor HTTP ──────────────────────────────────────────

	srv := &http.Server{
		Addr:         ":" + cfg.Port,
		Handler:      r,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	return &app{
		httpServer: srv,
		eventBus:   bus,
		pgPool:     pool,
	}, nil
}

// close libera todos los recursos en orden inverso al de inicialización.
func (a *app) close() {
	if a.eventBus != nil {
		_ = a.eventBus.Close()
	}
	if a.pgPool != nil {
		a.pgPool.Close()
	}
}

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
