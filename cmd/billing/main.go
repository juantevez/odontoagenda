// Package main es el entry point del servicio Billing & Payments.
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
	ServiceName     string
	Port            string
	DatabaseURL     string
	NATSUrl         string
	JWTSecret       string
	JWTIssuer       string
	CoverageBaseURL string

	// MercadoPago (Fase 5)
	// En MVP: un único access token global.
	// En producción: cada clínica provee el suyo via tabla billing.clinic_mp_config.
	MPAccessToken  string
	MPWebhookSecret string
}

func mustLoadConfig() config {
	cfg := config{
		ServiceName:     getEnv("SERVICE_NAME", "odontoagenda-billing"),
		Port:            getEnv("PORT", "8087"),
		DatabaseURL:     getEnv("DATABASE_URL", "postgres://odontoagenda:odontoagenda@localhost:5432/odontoagenda"),
		NATSUrl:         getEnv("NATS_URL", "nats://localhost:4222"),
		JWTSecret:       getEnv("JWT_SECRET", ""),
		JWTIssuer:       getEnv("JWT_ISSUER", "odontoagenda.iam"),
		CoverageBaseURL: getEnv("COVERAGE_BASE_URL", "http://localhost:8085"),
		MPAccessToken:   getEnv("MP_ACCESS_TOKEN", ""),
		MPWebhookSecret: getEnv("MP_WEBHOOK_SECRET", ""),
	}
	if cfg.JWTSecret == "" {
		fmt.Fprintln(os.Stderr, "FATAL: JWT_SECRET es requerido")
		os.Exit(1)
	}
	if cfg.MPAccessToken == "" {
		slog.Warn("MP_ACCESS_TOKEN no configurado — pagos MercadoPago deshabilitados")
	}
	if cfg.MPWebhookSecret == "" {
		slog.Warn("MP_WEBHOOK_SECRET no configurado — verificación HMAC desactivada (solo desarrollo)")
	}
	return cfg
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
