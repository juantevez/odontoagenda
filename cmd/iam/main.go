// Package main es el entry point del servicio IAM (Identity & Access Management).
//
// Responsabilidades de main.go:
//   1. Cargar configuración desde variables de entorno.
//   2. Delegar la construcción de dependencias a wire.go (initApp).
//   3. Iniciar el servidor HTTP.
//   4. Esperar señal OS y ejecutar graceful shutdown.
//
// main.go NO conoce repositorios, handlers ni domain services: eso es wire.go.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
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

	// Construir árbol de dependencias completo (wire.go).
	application, err := initApp(cfg)
	if err != nil {
		logger.Error("error inicializando aplicación", "error", err)
		os.Exit(1)
	}
	defer application.close()

	// Iniciar servidor HTTP en goroutine.
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

	// Bloquear hasta recibir señal del OS.
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	sig := <-quit

	logger.Info("señal recibida, iniciando graceful shutdown", "signal", sig)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := application.httpServer.Shutdown(ctx); err != nil {
		logger.Error("error durante shutdown del servidor HTTP", "error", err)
	}

	logger.Info("servicio detenido correctamente", "service", cfg.ServiceName)
}

// ── Config ────────────────────────────────────────────────────────

type config struct {
	ServiceName         string
	Port                string
	DatabaseURL         string
	NATSUrl             string
	JWTSecret           string
	JWTIssuer           string
	AccessTokenTTLMin   int
	RefreshTokenTTLDays int
}

func mustLoadConfig() config {
	cfg := config{
		ServiceName:         getEnv("SERVICE_NAME", "odontoagenda-iam"),
		Port:                getEnv("PORT", "8081"),
		DatabaseURL:         getEnv("DATABASE_URL", "postgres://odontoagenda:odontoagenda@localhost:5432/odontoagenda"),
		NATSUrl:             getEnv("NATS_URL", "nats://localhost:4222"),
		JWTSecret:           getEnv("JWT_SECRET", ""),
		JWTIssuer:           getEnv("JWT_ISSUER", "odontoagenda.iam"),
		AccessTokenTTLMin:   getEnvInt("ACCESS_TOKEN_TTL_MIN", 15),
		RefreshTokenTTLDays: getEnvInt("REFRESH_TOKEN_TTL_DAYS", 30),
	}

	// Validaciones obligatorias: fallan fast en startup.
	if cfg.JWTSecret == "" {
		fmt.Fprintln(os.Stderr, "FATAL: JWT_SECRET es requerido")
		os.Exit(1)
	}
	if len(cfg.JWTSecret) < 32 {
		fmt.Fprintln(os.Stderr, "FATAL: JWT_SECRET debe tener al menos 32 caracteres")
		os.Exit(1)
	}
	if cfg.DatabaseURL == "" {
		fmt.Fprintln(os.Stderr, "FATAL: DATABASE_URL es requerido")
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

func getEnvInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return fallback
}
