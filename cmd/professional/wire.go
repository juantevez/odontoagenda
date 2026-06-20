// Package main contiene el wire-up del bounded context Professional.
package main

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	profcmd "github.com/juantevez/odontoagenda/context/professional/application/command"
	profqry "github.com/juantevez/odontoagenda/context/professional/application/query"
	profsvc "github.com/juantevez/odontoagenda/context/professional/domain/service"
	profhttp "github.com/juantevez/odontoagenda/context/professional/infrastructure/http"
	profpostgres "github.com/juantevez/odontoagenda/context/professional/infrastructure/postgres"
	"github.com/juantevez/odontoagenda/pkg/events"
	"github.com/juantevez/odontoagenda/pkg/middleware"
)

type app struct {
	httpServer *http.Server
	eventBus   *events.NATSBus
	pgPool     *pgxpool.Pool
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

	profRepo := profpostgres.NewProfessionalPostgresRepository(pool)

	// ── 3. Domain Services ────────────────────────────────────────

	conflictChecker := profsvc.NewScheduleConflictChecker()
	// expirationChecker se usará en el job scheduler de vencimiento de matrículas.
	// Por ahora se instancia aquí para tenerlo disponible si algún handler lo necesita.
	_ = profsvc.NewLicenseExpirationChecker()

	// ── 4. Command Handlers ───────────────────────────────────────

	registerHandler := profcmd.NewRegisterProfessionalHandler(profRepo, bus)
	addLicenseHandler := profcmd.NewAddLicenseHandler(profRepo, bus)
	assignClinicHandler := profcmd.NewAssignToClinicHandler(profRepo, conflictChecker, bus)
	updateScheduleHandler := profcmd.NewUpdateClinicScheduleHandler(profRepo, conflictChecker, bus)
	addExceptionHandler := profcmd.NewAddExceptionHandler(profRepo, bus)
	setDurationHandler := profcmd.NewSetProcedureDurationHandler(profRepo, bus)
	suspendHandler := profcmd.NewSuspendProfessionalHandler(profRepo, bus)

	// ── 5. Query Handlers ─────────────────────────────────────────

	getByIDHandler := profqry.NewGetProfessionalByIDHandler(profRepo)
	findByClinicHandler := profqry.NewFindByClinicHandler(profRepo)
	availableAtHandler := profqry.NewFindAvailableAtHandler(profRepo)
	forSchedulingHandler := profqry.NewGetProfessionalForSchedulingHandler(profRepo)

	// ── 6. Adaptadores de entrada: HTTP ───────────────────────────

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
		profhttp.RegisterRoutes(r, jwtCfg,
			registerHandler,
			addLicenseHandler,
			assignClinicHandler,
			updateScheduleHandler,
			addExceptionHandler,
			setDurationHandler,
			suspendHandler,
			getByIDHandler,
			findByClinicHandler,
			availableAtHandler,
			forSchedulingHandler,
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
		httpServer: srv,
		eventBus:   bus,
		pgPool:     pool,
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
