// Package main contiene el wire-up del bounded context Scheduling.
package main

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	schedulingcmd "github.com/juantevez/odontoagenda/context/scheduling/application/command"
	schedulingqry "github.com/juantevez/odontoagenda/context/scheduling/application/query"
	schedulingsaga "github.com/juantevez/odontoagenda/context/scheduling/domain/saga"
	schedulingsvc "github.com/juantevez/odontoagenda/context/scheduling/domain/service"
	schedulinghttp "github.com/juantevez/odontoagenda/context/scheduling/infrastructure/http"
	schedulingnats "github.com/juantevez/odontoagenda/context/scheduling/infrastructure/nats"
	schedulingpg "github.com/juantevez/odontoagenda/context/scheduling/infrastructure/postgres"
	schedulingredis "github.com/juantevez/odontoagenda/context/scheduling/infrastructure/redis"
	"github.com/juantevez/odontoagenda/pkg/events"
	"github.com/juantevez/odontoagenda/pkg/middleware"
)

type app struct {
	httpServer  *http.Server
	eventBus    *events.NATSBus
	pgPool      *pgxpool.Pool
	subscribers *schedulingnats.SchedulingEventSubscriber
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

	// Cliente Redis real (go-redis o similar).
	// En producción se inyecta aquí la implementación concreta de RedisClient.
	// Para desarrollo, se puede usar una implementación en memoria.
	redisClient := &noopRedisClient{} // stub para compilación; reemplazar con go-redis

	// ── 2. Repositorios ───────────────────────────────────────────

	apptRepo     := schedulingpg.NewAppointmentPostgresRepository(pool)
	scheduleRepo := schedulingpg.NewAvailabilitySchedulePostgresRepository(pool)
	cache        := schedulingredis.NewAvailabilityCacheRedis(redisClient)

	// ── 3. Domain Services ────────────────────────────────────────

	slotCalculator := schedulingsvc.NewSlotCalculator()
	bookingPolicy  := schedulingsvc.NewBookingPolicy()

	// ── 4. Saga ───────────────────────────────────────────────────

	bookSaga := schedulingsaga.NewBookAppointmentSaga(
		apptRepo, scheduleRepo, cache, bookingPolicy, bus,
	)

	// ── 5. Command Handlers ───────────────────────────────────────

	bookHandler     := schedulingcmd.NewBookAppointmentHandler(bookSaga, apptRepo)
	cancelHandler   := schedulingcmd.NewCancelAppointmentHandler(apptRepo, scheduleRepo, cache, bus)
	completeHandler := schedulingcmd.NewCompleteAppointmentHandler(apptRepo, bus)
	checkInHandler  := schedulingcmd.NewCheckInAppointmentHandler(apptRepo, bus)
	noShowHandler   := schedulingcmd.NewMarkNoShowHandler(apptRepo, scheduleRepo, cache, bus)
	blockSlotHandler := schedulingcmd.NewBlockSlotHandler(scheduleRepo, cache, bus)

	// ── 6. Query Handlers ─────────────────────────────────────────

	getAvailHandler      := schedulingqry.NewGetAvailabilityHandler(scheduleRepo, cache, slotCalculator)
	getAvailRangeHandler := schedulingqry.NewGetAvailabilityRangeHandler(scheduleRepo, slotCalculator)
	getDayHandler        := schedulingqry.NewGetDayScheduleHandler(apptRepo, scheduleRepo, slotCalculator)
	getPatientHandler    := schedulingqry.NewGetPatientAppointmentsHandler(apptRepo)

	// ── 7. NATS Subscribers ───────────────────────────────────────

	subscribers := schedulingnats.NewSchedulingEventSubscriber(bus, scheduleRepo, cache, apptRepo)
	if err := subscribers.RegisterAll(context.Background()); err != nil {
		return nil, fmt.Errorf("wire: nats subscribers: %w", err)
	}

	// ── 8. HTTP Router ────────────────────────────────────────────

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
		schedulinghttp.RegisterRoutes(r, jwtCfg,
			bookHandler,
			cancelHandler,
			completeHandler,
			checkInHandler,
			noShowHandler,
			blockSlotHandler,
			getAvailHandler,
			getAvailRangeHandler,
			getDayHandler,
			getPatientHandler,
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

// ── noopRedisClient — stub para compilación ───────────────────────
// En producción: reemplazar con adaptador go-redis real.

type noopRedisClient struct{}

func (n *noopRedisClient) Get(_ context.Context, _ string) (string, error) {
	return "", fmt.Errorf("cache miss")
}
func (n *noopRedisClient) Set(_ context.Context, _, _ string, _ time.Duration) error { return nil }
func (n *noopRedisClient) Del(_ context.Context, _ ...string) error                  { return nil }
func (n *noopRedisClient) Keys(_ context.Context, _ string) ([]string, error)        { return nil, nil }
func (n *noopRedisClient) SetNX(_ context.Context, _, _ string, _ time.Duration) (bool, error) {
	return true, nil
}
func (n *noopRedisClient) GetDel(_ context.Context, _ string) (string, error) { return "", nil }
