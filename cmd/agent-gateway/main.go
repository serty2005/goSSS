// Package main — точка входа agent-gateway.
//
// agent-gateway — облегчённый горизонтально-масштабируемый сервис,
// выделенный из монолита XenionDesk. Отвечает за:
//   - приём heartbeat и данных от агентов (sssruner / getad);
//   - регистрацию агентов и выпуск JWT access-токенов;
//   - публикацию событий в NATS JetStream (publisher-only, без подписок).
//
// Конфигурация читается из .env, общая с монолитом.
// Для локальной разработки можно использовать EVENT_BUS_BACKEND=memory.
package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"

	"etalon-server/internal/infra/config"
	"etalon-server/internal/infra/db"
	"etalon-server/internal/infra/logger"
	infraRepos "etalon-server/internal/infra/repositories"
	"etalon-server/internal/services"
	"etalon-server/internal/services/agentauth"
	"etalon-server/internal/transport/http/handlers"
	"etalon-server/pkg/eventbus"

	// Регистрация payload-типов событий через init().
	_ "etalon-server/internal/core/events"
)

func main() {
	bootstrapLog := logger.ConfigureStdLogger("agent-gateway", os.Getenv("LOG_LEVEL"))

	// 1. Конфигурация
	cfg := config.New()
	log := logger.New("", "agent-gateway", cfg.LogLevel, true)

	log.Info("Запуск agent-gateway",
		"event_bus_backend", cfg.EventBusBackend,
		"port", cfg.AgentGatewayPort,
	)

	// 2. База данных
	database, err := db.NewConnection(cfg)
	if err != nil {
		bootstrapLog.Fatal("Ошибка подключения к БД", "error", err)
	}

	// Лёгкая миграция — только таблицы agent-gateway (Agent, AgentSessionToken,
	// AgentCommand, AgentRegistrationAttempt) + partial unique index для saga_id.
	if err := db.MigrateAgentGateway(database); err != nil {
		bootstrapLog.Fatal("Ошибка миграции agent-gateway", "error", err)
	}
	log.Info("Миграция agent-gateway завершена")

	// 3. Шина событий (publisher-only)
	bus, err := eventbus.New(cfg)
	if err != nil {
		bootstrapLog.Fatal("Ошибка инициализации шины событий", "error", err)
	}

	// 4. Репозитории
	agentRepo := infraRepos.NewAgentRepo(database)
	companyRepo := infraRepos.NewCompanyRepo(database)

	// 5. Сервисы
	agentOperatorFlow := services.NewAgentOperatorFlowService(
		database,
		cfg.AgentAdapterCatalog.DefaultChannel,
	)

	agentService := services.NewAgentService(
		log.With("component", "agent_service"),
		agentRepo,
		companyRepo,
		bus,
		agentOperatorFlow,
	)

	// JWT access-токены (EdDSA). Если JWT выключен — используется opaque fallback.
	tokenService, err := agentauth.NewAccessTokenService(cfg.AgentAuth)
	if err != nil {
		bootstrapLog.Fatal("Ошибка инициализации JWT-сервиса", "error", err)
	}
	if tokenService != nil {
		log.Info("JWT access-токены включены (EdDSA)")
	} else {
		log.Info("JWT access-токены выключены, используется opaque-режим")
	}

	agentAuthService := agentauth.NewServiceWithJWT(
		database,
		log.With("component", "agent_auth_service"),
		agentRepo,
		agentService,
		tokenService,
	)

	agentHandler := handlers.NewAgentHandler(agentService, agentAuthService, cfg.AgentAPIKey)

	// 6. HTTP-роутер
	port := cfg.AgentGatewayPort
	if port == "" {
		port = "8090"
	}

	router := chi.NewRouter()
	router.Use(middleware.RequestID)
	router.Use(middleware.RealIP)
	router.Use(middleware.Recoverer)
	router.Use(cors.Handler(cors.Options{
		AllowedOrigins:   []string{"*"},
		AllowedMethods:   []string{"GET", "POST", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", "X-API-Key"},
		AllowCredentials: false,
		MaxAge:           300,
	}))

	// Health / readiness
	router.Get("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "ok")
	})
	router.Get("/readyz", func(w http.ResponseWriter, r *http.Request) {
		sqlDB, err := database.DB()
		if err != nil || sqlDB.Ping() != nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			fmt.Fprint(w, "db unavailable")
			return
		}
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "ok")
	})

	// Агентские маршруты
	router.Post("/api/submit_json", agentHandler.HandleSubmitJSON)
	router.Route("/api/agents", func(r chi.Router) {
		agentHandler.RegisterRoutes(r)
	})

	srv := &http.Server{
		Addr:              ":" + port,
		Handler:           router,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	// 7. Запуск
	go func() {
		log.Info("HTTP-сервер запущен", "port", port)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			bootstrapLog.Fatal("Ошибка HTTP-сервера", "error", err)
		}
	}()

	// 8. Graceful shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	sig := <-quit
	log.Info("Получен сигнал остановки, завершаем работу...", "signal", sig)

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Error("Ошибка при остановке HTTP-сервера", "error", err)
	}

	// Шина событий — publisher-only, просто закрываем.
	if b, ok := bus.(interface{ Shutdown() }); ok {
		b.Shutdown()
	}

	log.Info("agent-gateway остановлен")
}
