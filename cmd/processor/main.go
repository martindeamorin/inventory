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

// ProcessorService handles inventory events and updates the database
type ProcessorService struct {
	repo       *repository.InventoryRepository
	outboxRepo *repository.OutboxRepository
	publisher  *kafka.Publisher
	cache      *redisCache.CacheClient
	config     *config.Config
}

// NewProcessorService creates a new processor service
func NewProcessorService(repo *repository.InventoryRepository, outboxRepo *repository.OutboxRepository, publisher *kafka.Publisher, cache *redisCache.CacheClient, cfg *config.Config) *ProcessorService {
	return &ProcessorService{
		repo:       repo,
		outboxRepo: outboxRepo,
		publisher:  publisher,
		cache:      cache,
		config:     cfg,
	}
}

// HandleEvent processes inventory events from Kafka
func (p *ProcessorService) HandleEvent(ctx context.Context, event *models.InventoryEvent) error {
	log.Info().
		Str("event_id", event.EventID).
		Str("event_type", event.EventType).
		Str("sku", event.SKU).
		Int("qty", event.Qty).
		Msg("Processing inventory event")

	// Check if event has already been processed (deduplication)
	// Use outbox pattern for deduplication instead of processed_events table
	processed, err := p.outboxRepo.IsEventProcessed(ctx, event.EventType, event.SKU)
	if err != nil {
		log.Error().Err(err).
			Str("event_id", event.EventID).
			Str("event_type", event.EventType).
			Str("sku", event.SKU).
			Msg("Failed to check if event is processed")
		return fmt.Errorf("failed to check if event is processed: %w", err)
	}

	if processed {
		log.Warn().
			Str("event_id", event.EventID).
			Str("event_type", event.EventType).
			Str("sku", event.SKU).
			Msg("Event already processed - skipping duplicate")
		return nil
	}

	// Mark event as consumed from Kafka
	if err := p.outboxRepo.MarkEventConsumed(ctx, nil, event.EventType, event.SKU, "processor"); err != nil {
		log.Error().Err(err).
			Str("event_id", event.EventID).
			Str("event_type", event.EventType).
			Str("sku", event.SKU).
			Msg("Failed to mark event as consumed")
		return fmt.Errorf("failed to mark event as consumed: %w", err)
	}

	switch event.EventType {
	case models.EventTypeReserveStock:
		return p.handleReserveStock(ctx, event)
	case models.EventTypeCommitReserve:
		return p.handleCommitReserve(ctx, event)
	case models.EventTypeReleaseStock:
		return p.handleReleaseStock(ctx, event)
	case models.EventTypeExpireReserve:
		return p.handleExpireReserve(ctx, event)
	default:
		log.Warn().Str("event_type", event.EventType).Msg("Unknown event type")
		return nil
	}
}

// handleReserveStock processes stock reservation events
func (p *ProcessorService) handleReserveStock(ctx context.Context, event *models.InventoryEvent) error {
	tx, err := p.repo.BeginTx(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Get inventory with lock
	inventory, err := p.repo.GetInventoryForUpdate(ctx, tx, event.SKU)
	if err != nil {
		return fmt.Errorf("failed to get inventory: %w", err)
	}

	if inventory == nil {
		// Create new inventory record if it doesn't exist
		inventory = &models.Inventory{
			SKU:          event.SKU,
			AvailableQty: 0,
			ReservedQty:  0,
			Version:      0,
		}
		if err := p.repo.CreateInventory(ctx, tx, inventory); err != nil {
			return fmt.Errorf("failed to create inventory: %w", err)
		}
	}

	// Set event version based on current inventory version + 1
	event.Version = inventory.Version + 1

	// Validate event version for ordering (additional check for race conditions)
	if event.Version <= inventory.Version {
		log.Warn().
			Str("sku", event.SKU).
			Int64("event_version", event.Version).
			Int64("inventory_version", inventory.Version).
			Msg("Stale event detected - skipping")
		return nil // Skip stale events
	}

	// Check if enough stock is available
	if inventory.AvailableQty < event.Qty {
		log.Warn().
			Str("sku", event.SKU).
			Int("requested", event.Qty).
			Int("available", inventory.AvailableQty).
			Int64("version", event.Version).
			Msg("Insufficient stock for reservation")

		// You might want to publish a failure event here
		return fmt.Errorf("insufficient stock: requested %d, available %d", event.Qty, inventory.AvailableQty)
	}

	// Update inventory: decrease available, increase reserved, and increment version
	inventory.AvailableQty -= event.Qty
	inventory.ReservedQty += event.Qty
	// Note: UpdateInventory will increment version automatically

	if err := p.repo.UpdateInventory(ctx, tx, inventory); err != nil {
		return fmt.Errorf("failed to update inventory: %w", err)
	}

	// Mark event as processed within the transaction to prevent duplicates
	if err := p.outboxRepo.MarkEventProcessed(ctx, tx, event.EventType, event.SKU); err != nil {
		return fmt.Errorf("failed to mark event as processed: %w", err)
	}

	// Commit transaction
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	// Publish updated state
	return p.publishInventoryState(ctx, inventory)
}

// handleCommitReserve processes reservation commit events
func (p *ProcessorService) handleCommitReserve(ctx context.Context, event *models.InventoryEvent) error {
	tx, err := p.repo.BeginTx(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Get inventory with lock
	inventory, err := p.repo.GetInventoryForUpdate(ctx, tx, event.SKU)
	if err != nil {
		return fmt.Errorf("failed to get inventory: %w", err)
	}

	if inventory == nil {
		return fmt.Errorf("inventory not found for SKU: %s", event.SKU)
	}

	// Set event version based on current inventory version + 1
	event.Version = inventory.Version + 1

	// Validate event version for ordering
	if event.Version <= inventory.Version {
		log.Warn().
			Str("sku", event.SKU).
			Int64("event_version", event.Version).
			Int64("inventory_version", inventory.Version).
			Msg("Stale event detected - skipping")
		return nil // Skip stale events
	}

	// Update inventory: decrease reserved (stock is now sold)
	if inventory.ReservedQty < event.Qty {
		log.Warn().
			Str("sku", event.SKU).
			Int("requested", event.Qty).
			Int("reserved", inventory.ReservedQty).
			Int64("version", event.Version).
			Msg("Not enough reserved stock to commit")
		return fmt.Errorf("not enough reserved stock: requested %d, reserved %d", event.Qty, inventory.ReservedQty)
	}

	inventory.ReservedQty -= event.Qty
	// Note: UpdateInventory will increment version automatically

	if err := p.repo.UpdateInventory(ctx, tx, inventory); err != nil {
		return fmt.Errorf("failed to update inventory: %w", err)
	}

	// Mark event as processed within the transaction to prevent duplicates
	if err := p.outboxRepo.MarkEventProcessed(ctx, tx, event.EventType, event.SKU); err != nil {
		return fmt.Errorf("failed to mark event as processed: %w", err)
	}

	// Commit transaction
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	// Publish updated state
	return p.publishInventoryState(ctx, inventory)
}

// handleReleaseStock processes stock release events
func (p *ProcessorService) handleReleaseStock(ctx context.Context, event *models.InventoryEvent) error {
	tx, err := p.repo.BeginTx(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Get inventory with lock
	inventory, err := p.repo.GetInventoryForUpdate(ctx, tx, event.SKU)
	if err != nil {
		return fmt.Errorf("failed to get inventory: %w", err)
	}

	if inventory == nil {
		return fmt.Errorf("inventory not found for SKU: %s", event.SKU)
	}

	// Set event version based on current inventory version + 1
	event.Version = inventory.Version + 1

	// Validate event version for ordering
	if event.Version <= inventory.Version {
		log.Warn().
			Str("sku", event.SKU).
			Int64("event_version", event.Version).
			Int64("inventory_version", inventory.Version).
			Msg("Stale event detected - skipping")
		return nil // Skip stale events
	}

	// Update inventory: decrease reserved, increase available
	if inventory.ReservedQty < event.Qty {
		log.Warn().
			Str("sku", event.SKU).
			Int("requested", event.Qty).
			Int("reserved", inventory.ReservedQty).
			Int64("version", event.Version).
			Msg("Not enough reserved stock to release")
		return fmt.Errorf("not enough reserved stock: requested %d, reserved %d", event.Qty, inventory.ReservedQty)
	}

	inventory.ReservedQty -= event.Qty
	inventory.AvailableQty += event.Qty
	// Note: UpdateInventory will increment version automatically

	if err := p.repo.UpdateInventory(ctx, tx, inventory); err != nil {
		return fmt.Errorf("failed to update inventory: %w", err)
	}

	// Mark event as processed within the transaction to prevent duplicates
	if err := p.outboxRepo.MarkEventProcessed(ctx, tx, event.EventType, event.SKU); err != nil {
		return fmt.Errorf("failed to mark event as processed: %w", err)
	}

	// Commit transaction
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	// Publish updated state
	return p.publishInventoryState(ctx, inventory)
}

// handleExpireReserve processes reservation expiration events
func (p *ProcessorService) handleExpireReserve(ctx context.Context, event *models.InventoryEvent) error {
	tx, err := p.repo.BeginTx(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Get inventory with lock
	inventory, err := p.repo.GetInventoryForUpdate(ctx, tx, event.SKU)
	if err != nil {
		return fmt.Errorf("failed to get inventory: %w", err)
	}

	if inventory == nil {
		return fmt.Errorf("inventory not found for SKU: %s", event.SKU)
	}

	// Set event version based on current inventory version + 1
	event.Version = inventory.Version + 1

	// Validate event version for ordering
	if event.Version <= inventory.Version {
		log.Warn().
			Str("sku", event.SKU).
			Int64("event_version", event.Version).
			Int64("inventory_version", inventory.Version).
			Msg("Stale event detected - skipping")
		return nil // Skip stale events
	}

	// Update inventory: decrease reserved, increase available
	if inventory.ReservedQty < event.Qty {
		log.Warn().
			Str("sku", event.SKU).
			Int("requested", event.Qty).
			Int("reserved", inventory.ReservedQty).
			Int64("version", event.Version).
			Msg("Not enough reserved stock to release")
		return fmt.Errorf("not enough reserved stock: requested %d, reserved %d", event.Qty, inventory.ReservedQty)
	}

	inventory.ReservedQty -= event.Qty
	inventory.AvailableQty += event.Qty

	if err := p.repo.UpdateInventory(ctx, tx, inventory); err != nil {
		return fmt.Errorf("failed to update inventory: %w", err)
	}

	// Update reservation status to EXPIRED
	if event.ReservationID != nil {
		if err := p.repo.UpdateReservationStatus(ctx, tx, *event.ReservationID, models.ReservationStatusExpired); err != nil {
			return fmt.Errorf("failed to update reservation status: %w", err)
		}
	}

	// Mark event as processed within the transaction to prevent duplicates
	if err := p.outboxRepo.MarkEventProcessed(ctx, tx, event.EventType, event.SKU); err != nil {
		return fmt.Errorf("failed to mark event as processed: %w", err)
	}

	// Commit transaction
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	// Publish updated state
	return p.publishInventoryState(ctx, inventory)
}

// publishInventoryState publishes the current inventory state to Kafka
func (p *ProcessorService) publishInventoryState(ctx context.Context, inventory *models.Inventory) error {
	state := &models.InventoryState{
		SKU:          inventory.SKU,
		AvailableQty: inventory.AvailableQty,
		ReservedQty:  inventory.ReservedQty,
		Version:      inventory.Version,
		UpdatedAt:    inventory.UpdatedAt,
	}

	if err := p.publisher.PublishState(ctx, state); err != nil {
		log.Error().Err(err).Str("sku", inventory.SKU).Msg("Failed to publish inventory state")
		return fmt.Errorf("failed to publish inventory state: %w", err)
	}

	// Invalidate cache asynchronously
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		if err := p.cache.DeleteInventory(ctx, inventory.SKU); err != nil {
			log.Error().Err(err).Str("sku", inventory.SKU).Msg("Failed to invalidate cache")
		}
	}()

	return nil
}

// runExpirationSweeper periodically checks for expired reservations
func (p *ProcessorService) runExpirationSweeper(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second) // Check every 30 seconds
	defer ticker.Stop()

	log.Info().Msg("Starting reservation expiration sweeper")

	for {
		select {
		case <-ctx.Done():
			log.Info().Msg("Stopping expiration sweeper")
			return
		case <-ticker.C:
			if err := p.processExpiredReservations(ctx); err != nil {
				log.Error().Err(err).Msg("Failed to process expired reservations")
			}
		}
	}
}

// processExpiredReservations finds and expires old reservations
func (p *ProcessorService) processExpiredReservations(ctx context.Context) error {
	expiredReservations, err := p.repo.GetExpiredReservations(ctx)
	if err != nil {
		return fmt.Errorf("failed to get expired reservations: %w", err)
	}

	for _, reservation := range expiredReservations {
		// Create expire event
		event := &models.InventoryEvent{
			EventID:        fmt.Sprintf("expire-%s", reservation.ReservationID.String()),
			EventType:      models.EventTypeExpireReserve,
			SKU:            reservation.SKU,
			Qty:            reservation.Qty,
			ReservationID:  &reservation.ReservationID,
			Status:         models.ReservationStatusExpired,
			OwnerID:        reservation.OwnerID,
			IdempotencyKey: reservation.IdempotencyKey,
			Timestamp:      time.Now(),
			Version:        0, // Will be set by the processor
		}

		// Process the expiration
		if err := p.HandleEvent(ctx, event); err != nil {
			log.Error().Err(err).
				Str("reservation_id", reservation.ReservationID.String()).
				Msg("Failed to process expired reservation")
			continue
		}

		log.Info().
			Str("reservation_id", reservation.ReservationID.String()).
			Str("sku", reservation.SKU).
			Msg("Processed expired reservation")
	}

	if len(expiredReservations) > 0 {
		log.Info().Int("count", len(expiredReservations)).Msg("Processed expired reservations")
	}

	return nil
}

// runProcessedEventsCleanup periodically cleans up old processed events
func (p *ProcessorService) runProcessedEventsCleanup(ctx context.Context) {
	// Clean up processed events older than 24 hours every hour
	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()

	log.Info().Msg("Starting processed events cleanup routine")

	for {
		select {
		case <-ctx.Done():
			log.Info().Msg("Stopping processed events cleanup routine")
			return
		case <-ticker.C:
			if err := p.outboxRepo.CleanupOldProcessedEvents(ctx, 24*time.Hour); err != nil {
				log.Error().Err(err).Msg("Failed to cleanup old processed outbox events")
			}
		}
	}
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

// initializeKafka sets up Kafka publisher and consumer
func initializeKafka(cfg *config.Config) (*kafka.Publisher, *kafka.Consumer) {
	publisher := kafka.NewPublisher(cfg.KafkaBrokers, cfg.KafkaEventsTopicName, cfg.KafkaStateTopicName)
	consumer := kafka.NewConsumer(cfg.KafkaBrokers, cfg.KafkaConsumerGroup, cfg.KafkaEventsTopicName, cfg.KafkaStateTopicName)

	return publisher, consumer
}

// initializeCache sets up Redis cache with cluster support
func initializeCache(cfg *config.Config) *redisCache.CacheClient {
	return redisCache.NewCacheClient(
		cfg.RedisAddrs,
		cfg.RedisPassword,
		cfg.RedisClusterMode,
		cfg.RedisTTL,
		cfg.RedisKeyPrefix,
	)
}

// startHTTPServer starts the HTTP server for monitoring
func startHTTPServer(cfg *config.Config) *http.Server {
	handler := api.NewProcessorHandler()
	serverAddr := fmt.Sprintf("%s:%s", cfg.ServerAddr, cfg.ServerPort)

	srv := &http.Server{
		Addr:    serverAddr,
		Handler: handler.SetupProcessorRoutes(),
	}

	go func() {
		log.Info().Str("address", serverAddr).Msg("Processor Service HTTP server starting")
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Error().Err(err).Msg("HTTP server failed")
		}
	}()

	return srv
}

// startBackgroundServices starts all background goroutines
func startBackgroundServices(ctx context.Context, processor *ProcessorService, consumer *kafka.Consumer) {
	// Start expiration sweeper
	go processor.runExpirationSweeper(ctx)

	// Start processed events cleanup routine
	go processor.runProcessedEventsCleanup(ctx)

	// Start consuming events
	go func() {
		if err := consumer.ConsumeEvents(ctx, processor); err != nil {
			log.Error().Err(err).Msg("Event consumption stopped")
		}
	}()
}

// gracefulShutdown handles graceful shutdown of the service
func gracefulShutdown(cancel context.CancelFunc, srv *http.Server) {
	// Wait for interrupt signal to gracefully shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Info().Msg("Shutting down Processor Service...")

	// Cancel context to stop all goroutines
	cancel()

	// Shutdown HTTP server
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		log.Error().Err(err).Msg("HTTP server forced shutdown")
	}

	// Give some time for graceful shutdown
	time.Sleep(5 * time.Second)

	log.Info().Msg("Processor Service stopped")
}

func main() {
	setupLogging()
	log.Info().Msg("Starting Processor Service...")

	// Load configuration
	cfg := config.LoadConfig()

	// Initialize all components
	db := initializeDatabase(cfg)
	defer db.Close()

	publisher, consumer := initializeKafka(cfg)
	defer publisher.Close()
	defer consumer.Close()

	cache := initializeCache(cfg)
	repo := repository.NewInventoryRepository(db)
	outboxRepo := repository.NewOutboxRepository(db)
	processor := NewProcessorService(repo, outboxRepo, publisher, cache, cfg)

	// Create context for graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start all services
	srv := startHTTPServer(cfg)
	startBackgroundServices(ctx, processor, consumer)

	log.Info().Msg("Processor Service started, consuming events...")

	// Handle graceful shutdown
	gracefulShutdown(cancel, srv)
}
