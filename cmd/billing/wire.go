// Package main contiene el wire-up del bounded context Billing & Payments.
package main

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	billingcmd "github.com/juantevez/odontoagenda/context/billing/application/command"
	billingqry "github.com/juantevez/odontoagenda/context/billing/application/query"
	billingservice "github.com/juantevez/odontoagenda/context/billing/domain/service"
	billinghttp "github.com/juantevez/odontoagenda/context/billing/infrastructure/http"
	billingnats "github.com/juantevez/odontoagenda/context/billing/infrastructure/nats"
	billingpg "github.com/juantevez/odontoagenda/context/billing/infrastructure/postgres"
	coverageclient "github.com/juantevez/odontoagenda/context/billing/infrastructure/coverage"
	"github.com/juantevez/odontoagenda/pkg/events"
	"github.com/juantevez/odontoagenda/pkg/middleware"
)

type app struct {
	httpServer  *http.Server
	eventBus    *events.NATSBus
	pgPool      *pgxpool.Pool
	subscribers *billingnats.BillingEventSubscriber
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

	quoteRepo          := billingpg.NewQuotePostgresRepository(pool)
	cancellationRepo   := billingpg.NewClinicCancellationPolicyPostgresRepository(pool)

	// ── 3. Domain Services ────────────────────────────────────────

	calculator    := billingservice.NewBillingCalculator()
	policyService := billingservice.NewCancellationPolicyService(cancellationRepo)

	// ── 4. CoverageClient (adaptador HTTP hacia Coverage BC) ──────

	coverageClient := coverageclient.NewCoverageClient(cfg.CoverageBaseURL)

	// ── 5. Command Handlers ───────────────────────────────────────

	createQuoteH     := billingcmd.NewCreateQuoteHandler(quoteRepo, coverageClient, calculator, policyService, bus)
	confirmQuoteH    := billingcmd.NewConfirmQuoteHandler(quoteRepo, bus)
	voidQuoteH       := billingcmd.NewVoidQuoteHandler(quoteRepo, bus)
	applyLateFeeH    := billingcmd.NewApplyLateFeeHandler(quoteRepo, bus)
	registerPaymentH := billingcmd.NewRegisterPaymentHandler(quoteRepo, bus)
	waiveLateFeeH    := billingcmd.NewWaiveLateFeeHandler(quoteRepo, bus)
	setAuthCodeH     := billingcmd.NewSetAuthorizationCodeHandler(quoteRepo)

	// ── 6. Query Handlers ─────────────────────────────────────────

	getQuoteByIDH     := billingqry.NewGetQuoteByIDHandler(quoteRepo)
	getQuoteByApptH   := billingqry.NewGetQuoteByAppointmentHandler(quoteRepo)
	getPatientAcctH   := billingqry.NewGetPatientAccountHandler(quoteRepo)
	getPatientQuotesH := billingqry.NewGetPatientQuotesHandler(quoteRepo)
	getDailyReportH   := billingqry.NewGetDailyReportHandler(quoteRepo)

	// ── 7. NATS Subscribers ───────────────────────────────────────

	subscribers := billingnats.NewBillingEventSubscriber(
		bus,
		createQuoteH,
		confirmQuoteH,
		voidQuoteH,
		applyLateFeeH,
		setAuthCodeH,
	)
	if err := subscribers.RegisterAll(context.Background()); err != nil {
		return nil, fmt.Errorf("wire: nats subscribers: %w", err)
	}

	// ── 8. HTTP Handlers de infraestructura ───────────────────────

	cancellationPolicyH := billinghttp.NewCancellationPolicyHTTPHandler(cancellationRepo)

	// ── 9. HTTP Router ────────────────────────────────────────────

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
		billinghttp.RegisterRoutes(r, jwtCfg,
			registerPaymentH,
			voidQuoteH,
			waiveLateFeeH,
			getQuoteByIDH,
			getQuoteByApptH,
			getPatientAcctH,
			getPatientQuotesH,
			getDailyReportH,
			cancellationPolicyH,
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
