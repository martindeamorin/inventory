package kafka

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/rs/zerolog/log"
	"github.com/segmentio/kafka-go"

	"inventory-service/internal/models"
	"inventory-service/internal/repository"
)

// Publisher handles publishing messages to Kafka
type Publisher struct {
	eventsWriter *kafka.Writer
	stateWriter  *kafka.Writer
}

// NewPublisher creates a new Kafka publisher
func NewPublisher(brokers []string, eventsTopic, stateTopic string) *Publisher {
	// Use hash balancer for events so messages with the same Key (SKU)
	// are routed to the same partition and ordering is preserved per SKU.
	eventsWriter := &kafka.Writer{
		Addr:                   kafka.TCP(brokers...),
		Topic:                  eventsTopic,
		Balancer:               &kafka.Hash{},
		RequiredAcks:           kafka.RequireAll, // Wait for all replicas
		Async:                  false,            // Synchronous writes for reliability
		AllowAutoTopicCreation: true,

		// Producer reliability settings
		BatchTimeout: 10 * time.Millisecond, // Small batch timeout for low latency
		BatchSize:    1,                     // Process one message at a time for consistency
		MaxAttempts:  3,                     // Retry failed sends
		WriteTimeout: 10 * time.Second,      // Timeout for write operations
	}

	stateWriter := &kafka.Writer{
		Addr:                   kafka.TCP(brokers...),
		Topic:                  stateTopic,
		Balancer:               &kafka.Hash{}, // Use hash balancer for state updates
		RequiredAcks:           kafka.RequireAll,
		Async:                  false,
		AllowAutoTopicCreation: true,

		// Producer reliability settings
		BatchTimeout: 10 * time.Millisecond,
		BatchSize:    1,
		MaxAttempts:  3,
		WriteTimeout: 10 * time.Second,
	}

	return &Publisher{
		eventsWriter: eventsWriter,
		stateWriter:  stateWriter,
	}
}

// PublishEvent publishes an inventory event to the events topic
func (p *Publisher) PublishEvent(ctx context.Context, event *models.InventoryEvent) error {
	data, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("failed to marshal event: %w", err)
	}

	message := kafka.Message{
		Key:   []byte(event.SKU), // Partition by SKU for ordering
		Value: data,
		Headers: []kafka.Header{
			{Key: "event-type", Value: []byte(event.EventType)},
			{Key: "event-id", Value: []byte(event.EventID)},
		},
	}

	err = p.eventsWriter.WriteMessages(ctx, message)
	if err != nil {
		log.Error().Err(err).
			Str("event_type", event.EventType).
			Str("sku", event.SKU).
			Str("event_id", event.EventID).
			Msg("Failed to publish event")
		return fmt.Errorf("failed to publish event: %w", err)
	}

	log.Info().
		Str("event_type", event.EventType).
		Str("sku", event.SKU).
		Str("event_id", event.EventID).
		Msg("Published event")

	return nil
}

// PublishState publishes inventory state to the state topic
func (p *Publisher) PublishState(ctx context.Context, state *models.InventoryState) error {
	data, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("failed to marshal state: %w", err)
	}

	message := kafka.Message{
		Key:   []byte(state.SKU), // Partition by SKU for ordering
		Value: data,
		Headers: []kafka.Header{
			{Key: "event-type", Value: []byte(models.EventTypeInventoryState)},
			{Key: "sku", Value: []byte(state.SKU)},
		},
	}

	err = p.stateWriter.WriteMessages(ctx, message)
	if err != nil {
		log.Error().Err(err).
			Str("sku", state.SKU).
			Msg("Failed to publish state")
		return fmt.Errorf("failed to publish state: %w", err)
	}

	log.Debug().
		Str("sku", state.SKU).
		Int("available_qty", state.AvailableQty).
		Int("reserved_qty", state.ReservedQty).
		Msg("Published state")

	return nil
}

// Close closes the Kafka writers
func (p *Publisher) Close() error {
	var errs []error

	if err := p.eventsWriter.Close(); err != nil {
		errs = append(errs, fmt.Errorf("failed to close events writer: %w", err))
	}

	if err := p.stateWriter.Close(); err != nil {
		errs = append(errs, fmt.Errorf("failed to close state writer: %w", err))
	}

	if len(errs) > 0 {
		return fmt.Errorf("errors closing publishers: %v", errs)
	}

	return nil
}

// PublishOutboxEvent publishes an event from outbox table
func (p *Publisher) PublishOutboxEvent(ctx context.Context, outboxEvent *models.OutboxEvent) error {
	// Parse the payload back to InventoryEvent
	var event models.InventoryEvent
	if err := json.Unmarshal([]byte(outboxEvent.Payload), &event); err != nil {
		return fmt.Errorf("failed to unmarshal outbox payload: %w", err)
	}

	// Publish using the same method
	return p.PublishEvent(ctx, &event)
}

// RunOutboxPublisher runs the outbox publisher loop with advisory locking
func (p *Publisher) RunOutboxPublisher(ctx context.Context, outboxRepo *repository.OutboxRepository) {
	// Get configuration from environment
	lockKeyStr := os.Getenv("OUTBOX_LOCK_KEY")
	if lockKeyStr == "" {
		lockKeyStr = "1234567890" // Default lock key
	}
	lockKey, err := strconv.ParseInt(lockKeyStr, 10, 64)
	if err != nil {
		log.Fatal().Err(err).Str("lock_key", lockKeyStr).Msg("Invalid OUTBOX_LOCK_KEY")
	}

	batchSizeStr := os.Getenv("OUTBOX_BATCH_SIZE")
	if batchSizeStr == "" {
		batchSizeStr = "100" // Default batch size
	}
	batchSize, err := strconv.Atoi(batchSizeStr)
	if err != nil {
		log.Fatal().Err(err).Str("batch_size", batchSizeStr).Msg("Invalid OUTBOX_BATCH_SIZE")
	}

	intervalStr := os.Getenv("OUTBOX_POLL_INTERVAL")
	if intervalStr == "" {
		intervalStr = "500ms" // Default poll interval
	}
	interval, err := time.ParseDuration(intervalStr)
	if err != nil {
		log.Fatal().Err(err).Str("interval", intervalStr).Msg("Invalid OUTBOX_POLL_INTERVAL")
	}

	log.Info().
		Int64("lock_key", lockKey).
		Int("batch_size", batchSize).
		Dur("poll_interval", interval).
		Msg("Starting outbox publisher")

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Info().Msg("Stopping outbox publisher")
			return
		case <-ticker.C:
			if err := p.processOutboxBatch(ctx, outboxRepo, lockKey, batchSize); err != nil {
				log.Error().Err(err).Msg("Failed to process outbox batch")
			}
		}
	}
}

// processOutboxBatch processes a single batch of outbox events
func (p *Publisher) processOutboxBatch(ctx context.Context, outboxRepo *repository.OutboxRepository, lockKey int64, batchSize int) error {
	// Try to acquire advisory lock
	acquired, err := outboxRepo.TryAcquireOutboxLock(ctx, lockKey)
	if err != nil {
		return fmt.Errorf("failed to acquire lock: %w", err)
	}
	if !acquired {
		// Another worker holds the lock, skip this cycle
		log.Debug().Msg("Lock held by another worker, skipping batch")
		return nil
	}

	// Ensure lock is released
	defer func() {
		if err := outboxRepo.ReleaseOutboxLock(ctx, lockKey); err != nil {
			log.Error().Err(err).Msg("Failed to release outbox lock")
		}
	}()

	// Fetch batch of events to process
	events, err := outboxRepo.FetchOutboxBatchOrdered(ctx, batchSize)
	if err != nil {
		return fmt.Errorf("failed to fetch outbox batch: %w", err)
	}

	if len(events) == 0 {
		log.Debug().Msg("No outbox events to process")
		return nil
	}

	log.Debug().Int("count", len(events)).Msg("Processing outbox batch")

	// Process each event
	var successfulIDs []int64
	for _, event := range events {
		if err := p.publishOutboxEvent(ctx, &event); err != nil {
			log.Error().
				Err(err).
				Int64("outbox_id", int64(event.ID)).
				Str("event_type", event.EventType).
				Str("key", event.Key).
				Msg("Failed to publish outbox event")

			// Increment attempt counter
			if incrementErr := outboxRepo.IncrementPublishAttempts(ctx, int64(event.ID), err.Error()); incrementErr != nil {
				log.Error().Err(incrementErr).Int64("outbox_id", int64(event.ID)).Msg("Failed to increment publish attempts")
			}
			continue
		}

		successfulIDs = append(successfulIDs, int64(event.ID))
		log.Debug().
			Int64("outbox_id", int64(event.ID)).
			Str("event_type", event.EventType).
			Str("key", event.Key).
			Msg("Successfully published outbox event")
	}

	// Mark successful events as published
	if len(successfulIDs) > 0 {
		if err := outboxRepo.MarkOutboxPublished(ctx, successfulIDs); err != nil {
			return fmt.Errorf("failed to mark events as published: %w", err)
		}
		log.Info().
			Int("published_count", len(successfulIDs)).
			Int("total_count", len(events)).
			Msg("Outbox batch processed")
	}

	return nil
}

// publishOutboxEvent publishes a single outbox event to Kafka
func (p *Publisher) publishOutboxEvent(ctx context.Context, outboxEvent *repository.OutboxEvent) error {
	// Create Kafka message with SKU as key for partitioning
	message := kafka.Message{
		Key:   []byte(outboxEvent.Key), // Key is SKU for partitioning
		Value: []byte(outboxEvent.Payload),
		Time:  time.Now(),
	}

	// Determine which writer to use based on event type
	var writer *kafka.Writer
	if outboxEvent.EventType == "inventory.state" {
		writer = p.stateWriter
	} else {
		writer = p.eventsWriter
	}

	// Publish message
	if err := writer.WriteMessages(ctx, message); err != nil {
		return fmt.Errorf("failed to write message to Kafka: %w", err)
	}

	return nil
}
