// Package main es el entry point del servicio Coverage & Agreements.
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

	// Job scheduler: expirar autorizaciones stale cada hora.
	go application.runExpirationJob(context.Background())

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
	DatabaseURL string
	NATSUrl     string
	RedisURL    string
	JWTSecret   string
	JWTIssuer   string
}

func mustLoadConfig() config {
	cfg := config{
		ServiceName: getEnv("SERVICE_NAME", "odontoagenda-coverage"),
		Port:        getEnv("PORT", "8085"),
		DatabaseURL: getEnv("DATABASE_URL", "postgres://odontoagenda:odontoagenda@localhost:5432/odontoagenda"),
		NATSUrl:     getEnv("NATS_URL", "nats://localhost:4222"),
		RedisURL:    getEnv("REDIS_URL", "redis://localhost:6379"),
		JWTSecret:   getEnv("JWT_SECRET", ""),
		JWTIssuer:   getEnv("JWT_ISSUER", "odontoagenda.iam"),
	}
	if cfg.JWTSecret == "" {
		fmt.Fprintln(os.Stderr, "FATAL: JWT_SECRET es requerido")
		os.Exit(1)
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
