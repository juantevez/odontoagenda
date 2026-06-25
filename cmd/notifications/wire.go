// Package main contiene el wire-up del bounded context Notifications.
//
// Orden de construcción:
//  1. Infraestructura (NATS + PostgreSQL)
//  2. Senders (RouterSender con stubs o implementaciones reales según config)
//  3. Domain Services (TemplateService)
//  4. Application layer (SendNotificationHandler + WriteInboxHandler)
//  5. Adaptadores de entrada (NATS subscribers, HTTP router)
package main

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	notifcmd "github.com/juantevez/odontoagenda/context/notifications/application/command"
	notifsvc "github.com/juantevez/odontoagenda/context/notifications/domain/service"
	"github.com/juantevez/odontoagenda/context/notifications/domain/valueobject"
	notifhttp "github.com/juantevez/odontoagenda/context/notifications/infrastructure/http"
	notifnats "github.com/juantevez/odontoagenda/context/notifications/infrastructure/nats"
	notifpg "github.com/juantevez/odontoagenda/context/notifications/infrastructure/postgres"
	"github.com/juantevez/odontoagenda/context/notifications/infrastructure/sender"
	"github.com/juantevez/odontoagenda/pkg/events"
	"github.com/juantevez/odontoagenda/pkg/middleware"
)

// app agrupa todo lo que el servidor necesita en runtime.
type app struct {
	httpServer     *http.Server
	eventBus       *events.NATSBus
	subscribers    *notifnats.NotificationEventSubscriber
	inboxSubscriber *notifnats.InboxEventSubscriber
	dbPool         *pgxpool.Pool
}

func initApp(cfg config) (*app, error) {

	// ── 1. Infraestructura: NATS ──────────────────────────────────

	bus, err := events.New(events.Config{URL: cfg.NATSUrl})
	if err != nil {
		return nil, fmt.Errorf("wire: nats: %w", err)
	}

	// ── 2. Infraestructura: PostgreSQL ────────────────────────────

	pool, err := pgxpool.New(context.Background(), cfg.DatabaseURL)
	if err != nil {
		return nil, fmt.Errorf("wire: postgres: %w", err)
	}
	if err := pool.Ping(context.Background()); err != nil {
		return nil, fmt.Errorf("wire: postgres ping: %w", err)
	}

	// ── 3. Senders ────────────────────────────────────────────────

	var whatsappSender sender.Sender
	var emailSender sender.Sender
	var smsSender sender.Sender

	if cfg.LogOnlyMode {
		whatsappSender = sender.NewLogSender(valueobject.ChannelWhatsApp)
		emailSender = sender.NewLogSender(valueobject.ChannelEmail)
		smsSender = sender.NewLogSender(valueobject.ChannelSMS)
	} else {
		if cfg.WhatsAppEnabled && cfg.WhatsAppToken != "" {
			whatsappSender = sender.NewWhatsAppSender(cfg.WhatsAppProviderURL, cfg.WhatsAppToken)
		} else {
			whatsappSender = sender.NewLogSender(valueobject.ChannelWhatsApp)
		}
		if cfg.EmailEnabled && cfg.SendGridAPIKey != "" {
			emailSender = sender.NewEmailSender(cfg.EmailFrom, cfg.SendGridAPIKey)
		} else {
			emailSender = sender.NewLogSender(valueobject.ChannelEmail)
		}
		if cfg.SMSEnabled && cfg.TwilioAccountSID != "" {
			smsSender = sender.NewSMSSender(cfg.TwilioAccountSID, cfg.TwilioAuthToken, cfg.TwilioFromNumber)
		} else {
			smsSender = sender.NewLogSender(valueobject.ChannelSMS)
		}
	}

	logSender := sender.NewLogSender(valueobject.ChannelLog)
	router := sender.NewRouterSender(whatsappSender, emailSender, smsSender, logSender)

	// ── 4. Domain Services ────────────────────────────────────────

	templateSvc := notifsvc.NewTemplateService()

	// ── 5. Application layer ──────────────────────────────────────

	sendHandler := notifcmd.NewSendNotificationHandler(templateSvc, router)

	inboxRepo := notifpg.NewInboxPostgresRepository(pool)
	writeInboxHandler := notifcmd.NewWriteInboxHandler(inboxRepo)

	// ── 6. NATS Subscribers ───────────────────────────────────────

	// Subscriber existente: envía notificaciones salientes al paciente/profesional.
	subscribers := notifnats.NewNotificationEventSubscriber(bus, sendHandler)
	if err := subscribers.RegisterAll(context.Background()); err != nil {
		return nil, fmt.Errorf("wire: nats subscribers: %w", err)
	}

	// Subscriber de inbox: persiste los 4 eventos accionables en notifications.inbox.
	inboxSubscriber := notifnats.NewInboxEventSubscriber(bus, writeInboxHandler)
	if err := inboxSubscriber.RegisterAll(context.Background()); err != nil {
		return nil, fmt.Errorf("wire: inbox subscribers: %w", err)
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
		notifhttp.RegisterRoutes(r, jwtCfg, inboxRepo, writeInboxHandler)
	})

	srv := &http.Server{
		Addr:         ":" + cfg.Port,
		Handler:      r,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	return &app{
		httpServer:      srv,
		eventBus:        bus,
		subscribers:     subscribers,
		inboxSubscriber: inboxSubscriber,
		dbPool:          pool,
	}, nil
}

func (a *app) close() {
	if a.eventBus != nil {
		_ = a.eventBus.Close()
	}
	if a.dbPool != nil {
		a.dbPool.Close()
	}
}
