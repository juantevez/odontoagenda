// Package main es el entry point del servicio Notifications.
//
// Responsabilidades:
//  1. Cargar configuración desde variables de entorno.
//  2. Delegar construcción de dependencias a wire.go (initApp).
//  3. Iniciar servidor HTTP (solo /health en el MVP).
//  4. Esperar señal OS y ejecutar graceful shutdown.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	logger := newLogger()
	slog.SetDefault(logger)

	cfg := mustLoadConfig()
	logger.Info("configuración cargada",
		"service", cfg.ServiceName,
		"port", cfg.Port,
	)

	application, err := initApp(cfg)
	if err != nil {
		logger.Error("error inicializando aplicación", "error", err)
		os.Exit(1)
	}
	defer application.close()

	go func() {
		logger.Info("servidor HTTP iniciado",
			"service", cfg.ServiceName,
			"addr", application.httpServer.Addr,
		)
		if err := application.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("error fatal en servidor HTTP", "error", err)
			os.Exit(1)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	sig := <-quit

	logger.Info("señal recibida, iniciando graceful shutdown", "signal", sig)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := application.httpServer.Shutdown(ctx); err != nil {
		logger.Error("error durante shutdown", "error", err)
	}

	logger.Info("servicio detenido correctamente", "service", cfg.ServiceName)
}

// ── Config ────────────────────────────────────────────────────────

type config struct {
	ServiceName string
	Port        string
	NATSUrl     string

	// Canales de envío — en el MVP son stubs; se activan con las vars de entorno.
	// WhatsApp (Twilio / Baileys)
	WhatsAppEnabled     bool
	WhatsAppProviderURL string
	WhatsAppToken       string

	// Email (SendGrid / SMTP)
	EmailEnabled  bool
	EmailFrom     string
	SendGridAPIKey string

	// SMS (Twilio)
	SMSEnabled        bool
	TwilioAccountSID  string
	TwilioAuthToken   string
	TwilioFromNumber  string

	// Opciones de comportamiento
	LogOnlyMode bool // true = solo loguea, no envía (útil en tests y CI)
}

func mustLoadConfig() config {
	return config{
		ServiceName: getEnv("SERVICE_NAME", "odontoagenda-notifications"),
		Port:        getEnv("PORT", "8086"),
		NATSUrl:     getEnv("NATS_URL", "nats://localhost:4222"),

		WhatsAppEnabled:     getEnvBool("WHATSAPP_ENABLED", false),
		WhatsAppProviderURL: getEnv("WHATSAPP_PROVIDER_URL", ""),
		WhatsAppToken:       getEnv("WHATSAPP_TOKEN", ""),

		EmailEnabled:   getEnvBool("EMAIL_ENABLED", false),
		EmailFrom:      getEnv("EMAIL_FROM", "noreply@odontoagenda.com"),
		SendGridAPIKey: getEnv("SENDGRID_API_KEY", ""),

		SMSEnabled:       getEnvBool("SMS_ENABLED", false),
		TwilioAccountSID: getEnv("TWILIO_ACCOUNT_SID", ""),
		TwilioAuthToken:  getEnv("TWILIO_AUTH_TOKEN", ""),
		TwilioFromNumber: getEnv("TWILIO_FROM_NUMBER", ""),

		// Por defecto en desarrollo: solo loguea.
		LogOnlyMode: getEnvBool("NOTIFICATIONS_LOG_ONLY", true),
	}
}

// ── Helpers ───────────────────────────────────────────────────────

func newLogger() *slog.Logger {
	return slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
}

func healthHandler(serviceName string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"status":  "ok",
			"service": serviceName,
		})
	}
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func getEnvBool(key string, fallback bool) bool {
	v := os.Getenv(key)
	switch v {
	case "true", "1", "yes":
		return true
	case "false", "0", "no":
		return false
	default:
		return fallback
	}
}

func getEnv2(key, fallback string) string {
	return getEnv(key, fallback)
}

// Silenciar warning de unused si algún helper queda sin usar en este archivo.
var _ = fmt.Sprintf
var _ = getEnv2
