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
	"inventory-service/internal/models"
	redisCache "inventory-service/internal/redis"
	"inventory-service/internal/repository"
)

// ReaderService handles read operations and cache consistency
type ReaderService struct {
	repo   *repository.InventoryRepository
	cache  *redisCache.CacheClient
	config *config.Config
}

// NewReaderService creates a new reader service
func NewReaderService(repo *repository.InventoryRepository, cache *redisCache.CacheClient, cfg *config.Config) *ReaderService {
	return &ReaderService{
		repo:   repo,
		cache:  cache,
		config: cfg,
	}
}

// HandleState processes inventory state updates from Kafka to keep cache consistent
func (r *ReaderService) HandleState(ctx context.Context, state *models.InventoryState) error {
	log.Debug().
		Str("sku", state.SKU).
		Int("available_qty", state.AvailableQty).
		Int("reserved_qty", state.ReservedQty).
		Msg("Updating cache from state event")

	// Update cache with new state
	if err := r.cache.UpdateInventoryFromState(ctx, state); err != nil {
		log.Error().Err(err).Str("sku", state.SKU).Msg("Failed to update cache from state")
		return fmt.Errorf("failed to update cache: %w", err)
	}

	return nil
}

// GetAvailability returns inventory availability, checking cache first
func (r *ReaderService) GetAvailability(ctx context.Context, sku string) (*models.AvailabilityResponse, error) {
	// Try cache first
	inventory, err := r.cache.GetInventory(ctx, sku)
	if err != nil {
		log.Error().Err(err).Str("sku", sku).Msg("Cache error, falling back to database")
	}

	if inventory != nil {
		return &models.AvailabilityResponse{
			SKU:          inventory.SKU,
			AvailableQty: inventory.AvailableQty,
			ReservedQty:  inventory.ReservedQty,
			CacheHit:     true,
			LastUpdated:  inventory.UpdatedAt,
		}, nil
	}

	// Cache miss, read from database
	inventory, err = r.repo.GetInventory(ctx, sku)
	if err != nil {
		return nil, fmt.Errorf("failed to get inventory from database: %w", err)
	}

	if inventory == nil {
		return &models.AvailabilityResponse{
			SKU:          sku,
			AvailableQty: 0,
			ReservedQty:  0,
			CacheHit:     false,
			LastUpdated:  time.Now(),
		}, nil
	}

	// Update cache asynchronously
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		if err := r.cache.SetInventory(ctx, inventory); err != nil {
			log.Error().Err(err).Str("sku", sku).Msg("Failed to update cache")
		}
	}()

	return &models.AvailabilityResponse{
		SKU:          inventory.SKU,
		AvailableQty: inventory.AvailableQty,
		ReservedQty:  inventory.ReservedQty,
		CacheHit:     false,
		LastUpdated:  inventory.UpdatedAt,
	}, nil
}

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

	// Test cache connection
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := cache.Ping(ctx); err != nil {
		log.Fatal().Err(err).Msg("Failed to connect to Redis")
	}

	log.Info().Msg("Redis connection established")
	return cache
}

// initializeKafka sets up Kafka consumer for state updates
func initializeKafka(cfg *config.Config) *kafka.Consumer {
	return kafka.NewConsumer(cfg.KafkaBrokers, cfg.KafkaConsumerGroup+"-reader", cfg.KafkaEventsTopicName, cfg.KafkaStateTopicName)
}

// startHTTPServer starts the HTTP server
func startHTTPServer(cfg *config.Config, reader *ReaderService) *http.Server {
	readerHandler := api.NewReaderHandler(reader)
	router := readerHandler.SetupReaderRoutes()
	serverAddr := fmt.Sprintf("%s:%s", cfg.ServerAddr, cfg.ServerPort)

	server := &http.Server{
		Addr:         serverAddr,
		Handler:      router,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	go func() {
		log.Info().Str("address", serverAddr).Msg("Reader Service HTTP server starting")
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal().Err(err).Msg("Failed to start HTTP server")
		}
	}()

	return server
}

// startStateConsumer starts consuming state updates for cache consistency
func startStateConsumer(ctx context.Context, consumer *kafka.Consumer, reader *ReaderService) {
	go func() {
		log.Info().Msg("Starting to consume inventory state updates for cache consistency")
		if err := consumer.ConsumeState(ctx, reader); err != nil {
			log.Error().Err(err).Msg("State consumption stopped")
		}
	}()
}

// gracefulShutdown handles graceful shutdown of the service
func gracefulShutdown(cancel context.CancelFunc, server *http.Server) {
	// Wait for interrupt signal to gracefully shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Info().Msg("Shutting down Reader Service...")

	// Cancel context to stop Kafka consumption
	cancel()

	// Graceful shutdown of HTTP server
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer shutdownCancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Error().Err(err).Msg("Server forced to shutdown")
	}

	log.Info().Msg("Reader Service stopped")
}

func main() {
	setupLogging()
	log.Info().Msg("Starting Reader Service...")

	// Load configuration
	cfg := config.LoadConfig()

	// Initialize all components
	db := initializeDatabase(cfg)
	defer db.Close()

	cache := initializeCache(cfg)
	consumer := initializeKafka(cfg)
	defer consumer.Close()

	// Initialize service
	repo := repository.NewInventoryRepository(db)
	reader := NewReaderService(repo, cache, cfg)

	// Create context for graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start HTTP server and state consumer
	server := startHTTPServer(cfg, reader)
	startStateConsumer(ctx, consumer, reader)

	log.Info().Msg("Reader Service started")

	// Handle graceful shutdown
	gracefulShutdown(cancel, server)
}
