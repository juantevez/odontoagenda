// Package main contiene el wire-up del bounded context Coverage & Agreements.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	coveragecmd "github.com/juantevez/odontoagenda/context/coverage/application/command"
	coverageqry "github.com/juantevez/odontoagenda/context/coverage/application/query"
	coveragesvc "github.com/juantevez/odontoagenda/context/coverage/domain/service"
	coveragehttp "github.com/juantevez/odontoagenda/context/coverage/infrastructure/http"
	coveragenats "github.com/juantevez/odontoagenda/context/coverage/infrastructure/nats"
	coveragepg "github.com/juantevez/odontoagenda/context/coverage/infrastructure/postgres"
	coverageredis "github.com/juantevez/odontoagenda/context/coverage/infrastructure/redis"
	"github.com/juantevez/odontoagenda/pkg/events"
	"github.com/juantevez/odontoagenda/pkg/middleware"
)

type app struct {
	httpServer  *http.Server
	eventBus    *events.NATSBus
	pgPool      *pgxpool.Pool
	subscribers *coveragenats.CoverageEventSubscriber
	authSvc     *coveragesvc.AuthorizationService
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

	// Redis: stub noop para que compile.
	// En producción: reemplazar con adaptador go-redis real (igual que en scheduling).
	redisClient := &noopRedisClient{}
	cache := coverageredis.NewCoverageCacheRedis(redisClient)

	// ── 2. Repositorios ───────────────────────────────────────────

	agreementRepo := coveragepg.NewAgreementPostgresRepository(pool)
	authRepo := coveragepg.NewAuthorizationPostgresRepository(pool)
	affiliationRepo := coveragepg.NewPatientAffiliationPostgresRepository(pool)

	// ── 3. Domain Services ────────────────────────────────────────

	calculator := coveragesvc.NewCoverageCalculator(affiliationRepo)
	verifier := coveragesvc.NewAffiliationVerifier(agreementRepo, affiliationRepo)
	authSvc := coveragesvc.NewAuthorizationService(authRepo)

	// ── 4. Command Handlers ───────────────────────────────────────

	createAgreementH := coveragecmd.NewCreateAgreementHandler(agreementRepo, bus)
	addPlanH := coveragecmd.NewAddPlanHandler(agreementRepo, bus)
	upsertRuleH := coveragecmd.NewUpsertProcedureRuleHandler(agreementRepo, cache, bus)
	updateStatusH := coveragecmd.NewUpdateAgreementStatusHandler(agreementRepo, bus)
	requestAuthorizationH := coveragecmd.NewRequestAuthorizationHandler(authSvc, bus)
	resolveAuthorizationH := coveragecmd.NewResolveAuthorizationHandler(authSvc, bus)

	// ── 5. Query Handlers ─────────────────────────────────────────

	getAgreementH := coverageqry.NewGetAgreementHandler(agreementRepo)
	listAgreementsH := coverageqry.NewListAgreementsHandler(agreementRepo)
	calculateCoverageH := coverageqry.NewCalculateCoverageHandler(agreementRepo, calculator, cache)
	verifyAffiliationH := coverageqry.NewVerifyAffiliationHandler(verifier)
	getAuthorizationH := coverageqry.NewGetAuthorizationHandler(authRepo)
	listPendingH := coverageqry.NewListPendingAuthorizationsHandler(authRepo)

	// ── 6. NATS Subscribers ───────────────────────────────────────

	subscribers := coveragenats.NewCoverageEventSubscriber(bus, affiliationRepo, authRepo)
	if err := subscribers.RegisterAll(context.Background()); err != nil {
		return nil, fmt.Errorf("wire: nats subscribers: %w", err)
	}

	// ── 7. HTTP Router ────────────────────────────────────────────

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
		coveragehttp.RegisterRoutes(r, jwtCfg,
			createAgreementH,
			addPlanH,
			upsertRuleH,
			updateStatusH,
			requestAuthorizationH,
			resolveAuthorizationH,
			getAgreementH,
			listAgreementsH,
			calculateCoverageH,
			verifyAffiliationH,
			getAuthorizationH,
			listPendingH,
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
		authSvc:     authSvc,
	}, nil
}

// runExpirationJob ejecuta el job de expiración de autorizaciones cada hora.
func (a *app) runExpirationJob(ctx context.Context) {
	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if n, err := a.authSvc.ExpireStaleAuthorizations(ctx); err != nil {
				slog.Default().Error("error en job de expiración", "error", err)
			} else if n > 0 {
				slog.Default().Info("job expiración completado", "expired", n)
			}
		}
	}
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

type noopRedisClient struct{}

func (n *noopRedisClient) Get(_ context.Context, _ string) (string, error) {
	return "", fmt.Errorf("cache miss")
}
func (n *noopRedisClient) Set(_ context.Context, _, _ string, _ time.Duration) error { return nil }
func (n *noopRedisClient) Del(_ context.Context, _ ...string) error                  { return nil }
func (n *noopRedisClient) Keys(_ context.Context, _ string) ([]string, error)        { return nil, nil }
