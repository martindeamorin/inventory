package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jmoiron/sqlx"
	_ "github.com/lib/pq"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"

	"inventory-service/internal/api"
	"inventory-service/internal/config"
	"inventory-service/internal/kafka"
	redisCache "inventory-service/internal/redis"
	"inventory-service/internal/repository"
	"inventory-service/internal/service"
)

// setupLogging configures structured logging
func setupLogging() {
	zerolog.TimeFieldFormat = zerolog.TimeFormatUnix
	log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr})
}

// initializeDatabase sets up and tests the database connection
func initializeDatabase(cfg *config.Config) *sqlx.DB {
	db, err := sqlx.Connect("postgres", cfg.DatabaseURL)
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to connect to database")
	}

	// Test database connection
	if err := db.Ping(); err != nil {
		log.Fatal().Err(err).Msg("Failed to ping database")
	}

	log.Info().Msg("Database connection established")
	return db
}

// initializeCache sets up Redis cache with cluster support
func initializeCache(cfg *config.Config) *redisCache.CacheClient {
	cache := redisCache.NewCacheClient(
		cfg.RedisAddrs,
		cfg.RedisPassword,
		cfg.RedisClusterMode,
		cfg.RedisTTL,
		cfg.RedisKeyPrefix,
	)

	// Test Redis connection
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := cache.Ping(ctx); err != nil {
		log.Fatal().Err(err).Msg("Failed to connect to Redis")
	}
	log.Info().Msg("Redis connection established")

	return cache
}

// initializeKafka sets up Kafka publisher
func initializeKafka(cfg *config.Config) *kafka.Publisher {
	log.Info().Strs("kafka_brokers", cfg.KafkaBrokers).Msg("Initializing Kafka publisher with brokers")
	return kafka.NewPublisher(cfg.KafkaBrokers, cfg.KafkaEventsTopicName, cfg.KafkaStateTopicName)
}

// createInventoryService creates and configures the inventory service
func createInventoryService(repo *repository.InventoryRepository, outboxRepo *repository.OutboxRepository, publisher *kafka.Publisher, cache *redisCache.CacheClient, cfg *config.Config) *service.InventoryService {
	serviceConfig := service.ServiceConfig{
		ReservationTTL:           cfg.ReservationTTL,
		MaxReasonableReservation: cfg.MaxReasonableReservation,
		CacheTimeout:             cfg.CacheTimeout,
		StockSafetyBuffer:        cfg.StockSafetyBuffer,
		EnableStockPreCheck:      cfg.EnableStockPreCheck,
	}

	log.Info().
		Dur("reservation_ttl", serviceConfig.ReservationTTL).
		Float64("reservation_ttl_seconds", serviceConfig.ReservationTTL.Seconds()).
		Str("env_reservation_ttl_sec", os.Getenv("RESERVATION_TTL_SEC")).
		Int("max_reservation_qty", serviceConfig.MaxReasonableReservation).
		Dur("cache_timeout", serviceConfig.CacheTimeout).
		Float64("stock_safety_buffer", serviceConfig.StockSafetyBuffer).
		Bool("enable_stock_precheck", serviceConfig.EnableStockPreCheck).
		Msg("Service configuration loaded")

	inventoryService, err := service.NewInventoryService(repo, outboxRepo, publisher, cache, serviceConfig)
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to create inventory service")
	}

	return inventoryService
}

// startHTTPServer starts the HTTP server
func startHTTPServer(cfg *config.Config, inventoryService *service.InventoryService) *http.Server {
	queueHandler := api.NewQueueHandler(inventoryService)
	router := queueHandler.SetupQueueRoutes()

	serverAddr := fmt.Sprintf("%s:%s", cfg.ServerAddr, cfg.ServerPort)
	server := &http.Server{
		Addr:         serverAddr,
		Handler:      router,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	go func() {
		log.Info().Str("address", serverAddr).Msg("Queue Service HTTP server starting")
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal().Err(err).Msg("Failed to start HTTP server")
		}
	}()

	return server
}

// startOutboxWorker starts the outbox publisher with advisory locks
func startOutboxWorker(db *sqlx.DB, publisher *kafka.Publisher) {
	log.Info().Msg("Starting outbox publisher with advisory locks")

	// Create outbox repository for lock-based processing
	outboxRepo := repository.NewOutboxRepository(db)

	// Start outbox publisher with advisory locks for reliable, ordered event delivery
	outboxCtx, outboxCancel := context.WithCancel(context.Background())

	go func() {
		defer outboxCancel()
		log.Info().Msg("Outbox publisher with advisory locks started")
		publisher.RunOutboxPublisher(outboxCtx, outboxRepo)
		log.Warn().Msg("Outbox publisher stopped")
	}()
}

// gracefulShutdown handles graceful shutdown of the service
func gracefulShutdown(server *http.Server) {
	// Wait for interrupt signal to gracefully shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Info().Msg("Shutting down Queue Service...")

	// Graceful shutdown with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		log.Error().Err(err).Msg("Server forced to shutdown")
	}

	log.Info().Msg("Queue Service stopped")
}

func main() {
	setupLogging()
	log.Info().Msg("Starting Queue Service...")

	// Load configuration
	cfg := config.LoadConfig()

	// Initialize all components
	db := initializeDatabase(cfg)
	defer db.Close()

	cache := initializeCache(cfg)
	defer cache.Close()

	publisher := initializeKafka(cfg)
	defer publisher.Close()

	// Initialize repository and service
	repo := repository.NewInventoryRepository(db)
	outboxRepo := repository.NewOutboxRepository(db)
	inventoryService := createInventoryService(repo, outboxRepo, publisher, cache, cfg)

	// Start HTTP server
	server := startHTTPServer(cfg, inventoryService)

	// Start outbox worker
	startOutboxWorker(db, publisher)

	// Handle graceful shutdown
	gracefulShutdown(server)
}
