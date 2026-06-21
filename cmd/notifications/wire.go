// Package main contiene el wire-up del bounded context Notifications.
//
// Orden de construcción:
//  1. Infraestructura (bus NATS)
//  2. Senders (RouterSender con stubs o implementaciones reales según config)
//  3. Domain Services (TemplateService)
//  4. Application layer (SendNotificationHandler)
//  5. Adaptadores de entrada (NATS subscribers, HTTP router)
package main

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	notifhttp "github.com/juantevez/odontoagenda/context/notifications/infrastructure/http"
	notifnats "github.com/juantevez/odontoagenda/context/notifications/infrastructure/nats"

	notifcmd "github.com/juantevez/odontoagenda/context/notifications/application/command"
	notifsvc "github.com/juantevez/odontoagenda/context/notifications/domain/service"
	"github.com/juantevez/odontoagenda/context/notifications/domain/valueobject"
	"github.com/juantevez/odontoagenda/context/notifications/infrastructure/sender"
	"github.com/juantevez/odontoagenda/pkg/events"
	"github.com/juantevez/odontoagenda/pkg/middleware"
)

// app agrupa todo lo que el servidor necesita en runtime.
type app struct {
	httpServer  *http.Server
	eventBus    *events.NATSBus
	subscribers *notifnats.NotificationEventSubscriber
}

func initApp(cfg config) (*app, error) {

	// ── 1. Infraestructura: NATS ──────────────────────────────────

	bus, err := events.New(events.Config{URL: cfg.NATSUrl})
	if err != nil {
		return nil, fmt.Errorf("wire: nats: %w", err)
	}

	// ── 2. Senders ────────────────────────────────────────────────
	// Lógica de selección: si NOTIFICATIONS_LOG_ONLY=true (default en dev),
	// todos los canales usan LogSender. Si no, se usan los adaptadores reales
	// según las variables de entorno de cada canal.

	var whatsappSender sender.Sender
	var emailSender sender.Sender
	var smsSender sender.Sender

	if cfg.LogOnlyMode {
		// Modo desarrollo: todo va a stdout.
		whatsappSender = sender.NewLogSender(valueobject.ChannelWhatsApp)
		emailSender = sender.NewLogSender(valueobject.ChannelEmail)
		smsSender = sender.NewLogSender(valueobject.ChannelSMS)
	} else {
		// Modo producción: usar adaptadores reales si están habilitados,
		// LogSender como fallback si el canal no está configurado.
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

	// El canal Log siempre usa LogSender (canal interno para no-shows y eventos de staff).
	logSender := sender.NewLogSender(valueobject.ChannelLog)

	router := sender.NewRouterSender(whatsappSender, emailSender, smsSender, logSender)

	// ── 3. Domain Services ────────────────────────────────────────

	templateSvc := notifsvc.NewTemplateService()

	// ── 4. Application layer ──────────────────────────────────────

	sendHandler := notifcmd.NewSendNotificationHandler(templateSvc, router)

	// ── 5. NATS Subscribers ───────────────────────────────────────

	subscribers := notifnats.NewNotificationEventSubscriber(bus, sendHandler)
	if err := subscribers.RegisterAll(context.Background()); err != nil {
		return nil, fmt.Errorf("wire: nats subscribers: %w", err)
	}

	// ── 6. HTTP Router ────────────────────────────────────────────

	jwtCfg := middleware.JWTConfig{} // Notifications no requiere JWT en el MVP

	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.Logger(newLogger()))
	r.Use(middleware.Recoverer(newLogger()))
	r.Get("/health", healthHandler(cfg.ServiceName))

	r.Route("/api/v1", func(r chi.Router) {
		notifhttp.RegisterRoutes(r, jwtCfg)
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
		subscribers: subscribers,
	}, nil
}

func (a *app) close() {
	if a.eventBus != nil {
		_ = a.eventBus.Close()
	}
}
