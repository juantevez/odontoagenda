// Package main es el entry point del API Gateway de OdontoAgenda.
// Responsabilidades:
//   - Conectar con PostgreSQL, NATS y otros recursos de infraestructura.
//   - Construir el árbol de dependencias (DI manual, sin framework).
//   - Montar todos los routers de los bounded contexts.
//   - Iniciar el servidor HTTP con graceful shutdown.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/juantevez/odontoagenda/context/iam/application/command"
	"github.com/juantevez/odontoagenda/context/iam/domain/service"
	iamhttp "github.com/juantevez/odontoagenda/context/iam/infrastructure/http"
	iampostgres "github.com/juantevez/odontoagenda/context/iam/infrastructure/postgres"
	"github.com/juantevez/odontoagenda/pkg/events"
	"github.com/juantevez/odontoagenda/pkg/middleware"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
	slog.SetDefault(logger)

	cfg := loadConfig()

	// ── Infraestructura ──────────────────────────────────────────
	pool, err := pgxpool.New(context.Background(), cfg.DatabaseURL)
	if err != nil {
		logger.Error("error conectando a PostgreSQL", "error", err)
		os.Exit(1)
	}
	defer pool.Close()

	if err := pool.Ping(context.Background()); err != nil {
		logger.Error("PostgreSQL no responde", "error", err)
		os.Exit(1)
	}
	logger.Info("PostgreSQL conectado")

	eventBus, err := events.New(events.Config{URL: cfg.NATSUrl})
	if err != nil {
		logger.Error("error conectando a NATS", "error", err)
		os.Exit(1)
	}
	defer eventBus.Close()
	logger.Info("NATS JetStream conectado")

	// ── Construcción del árbol de dependencias (DI manual) ───────
	// IAM bounded context
	userRepo := iampostgres.NewUserPostgresRepository(pool)
	familyRepo := iampostgres.NewFamilyPostgresRepository(pool)

	tokenSvc := service.NewTokenService(service.TokenConfig{
		SecretKey:       []byte(cfg.JWTSecret),
		Issuer:          "odontoagenda.iam",
		AccessTokenTTL:  15 * time.Minute,
		RefreshTokenTTL: 30 * 24 * time.Hour,
	})

	registerHandler := command.NewRegisterUserHandler(userRepo, familyRepo, eventBus)
	loginHandler := command.NewLoginHandler(userRepo, familyRepo, tokenSvc)
	refreshHandler := command.NewRefreshTokensHandler(userRepo, familyRepo, tokenSvc)
	logoutHandler := command.NewLogoutHandler(userRepo, eventBus)

	jwtCfg := middleware.JWTConfig{
		SecretKey: []byte(cfg.JWTSecret),
		Issuer:    "odontoagenda.iam",
	}

	// ── Router ───────────────────────────────────────────────────
	r := chi.NewRouter()

	// Middlewares globales (orden importa)
	r.Use(middleware.RequestID)
	r.Use(middleware.Logger(logger))
	r.Use(middleware.Recoverer(logger))

	// Health check (sin auth)
	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"status":"ok","service":"odontoagenda-gateway"}`)
	})

	// Bounded contexts
	r.Route("/api/v1", func(r chi.Router) {
		iamhttp.RegisterRoutes(r, jwtCfg,
			registerHandler, loginHandler, refreshHandler, logoutHandler,
		)
		// Aquí se montarán los demás bounded contexts a medida que se desarrollen:
		// patienthttp.RegisterRoutes(r, jwtCfg, ...)
		// schedulinghttp.RegisterRoutes(r, jwtCfg, ...)
	})

	// ── Servidor HTTP con graceful shutdown ───────────────────────
	srv := &http.Server{
		Addr:         ":" + cfg.Port,
		Handler:      r,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	// Canal para escuchar señales del OS (SIGINT, SIGTERM).
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		logger.Info("servidor HTTP iniciado", "port", cfg.Port)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("error en servidor HTTP", "error", err)
			os.Exit(1)
		}
	}()

	<-quit
	logger.Info("señal recibida, iniciando graceful shutdown...")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		logger.Error("error en shutdown", "error", err)
	}
	logger.Info("servidor detenido correctamente")
}

// ── Config ────────────────────────────────────────────────────────

type config struct {
	Port        string
	DatabaseURL string
	NATSUrl     string
	JWTSecret   string
}

func loadConfig() config {
	cfg := config{
		Port:        getEnv("PORT", "8080"),
		DatabaseURL: getEnv("DATABASE_URL", "postgres://odontoagenda:odontoagenda@localhost:5432/odontoagenda"),
		NATSUrl:     getEnv("NATS_URL", "nats://localhost:4222"),
		JWTSecret:   getEnv("JWT_SECRET", ""),
	}

	if cfg.JWTSecret == "" {
		slog.Error("JWT_SECRET no configurado: variable de entorno requerida")
		os.Exit(1)
	}

	return cfg
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
