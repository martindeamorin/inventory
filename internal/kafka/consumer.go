package kafka

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/rs/zerolog/log"
	"github.com/segmentio/kafka-go"

	"inventory-service/internal/models"
)

// EventHandler defines the interface for handling inventory events
type EventHandler interface {
	HandleEvent(ctx context.Context, event *models.InventoryEvent) error
}

// StateHandler defines the interface for handling inventory state updates
type StateHandler interface {
	HandleState(ctx context.Context, state *models.InventoryState) error
}

// Consumer handles consuming messages from Kafka
type Consumer struct {
	eventsReader *kafka.Reader
	stateReader  *kafka.Reader
}

// NewConsumer creates a new Kafka consumer
func NewConsumer(brokers []string, consumerGroup, eventsTopic, stateTopic string) *Consumer {
	eventsReader := kafka.NewReader(kafka.ReaderConfig{
		Brokers: brokers,
		Topic:   eventsTopic,
		GroupID: consumerGroup,

		// Consumer configuration for reliability and exactly-once processing
		MinBytes:       1,               // Minimum bytes to wait for
		MaxBytes:       10e6,            // 10MB max message size
		CommitInterval: 5 * time.Second, // Commit less frequently to reduce rebalancing issues
		StartOffset:    kafka.LastOffset,

		// Partition and offset management
		MaxWait: 1 * time.Second, // Maximum time to wait for new messages

		// Error handling
		ErrorLogger: kafka.LoggerFunc(func(msg string, args ...interface{}) {
			log.Error().Msgf("Kafka events reader error: "+msg, args...)
		}),
	})

	stateReader := kafka.NewReader(kafka.ReaderConfig{
		Brokers: brokers,
		Topic:   stateTopic,
		GroupID: consumerGroup + "-state",

		// Consumer configuration
		MinBytes:       1,
		MaxBytes:       10e6,
		CommitInterval: time.Second,
		StartOffset:    kafka.LastOffset,

		// Error handling
		ErrorLogger: kafka.LoggerFunc(func(msg string, args ...interface{}) {
			log.Error().Msgf("Kafka state reader error: "+msg, args...)
		}),
	})

	return &Consumer{
		eventsReader: eventsReader,
		stateReader:  stateReader,
	}
}

// ConsumeEvents starts consuming events and processes them with the provided handler
func (c *Consumer) ConsumeEvents(ctx context.Context, handler EventHandler) error {
	log.Info().Msg("Starting to consume inventory events")

	for {
		select {
		case <-ctx.Done():
			log.Info().Msg("Stopping event consumption")
			return ctx.Err()
		default:
			// Read message with timeout
			message, err := c.eventsReader.FetchMessage(ctx)
			if err != nil {
				if err == context.Canceled {
					return nil
				}
				log.Error().Err(err).Msg("Failed to fetch event message")
				time.Sleep(time.Second) // Backoff on error
				continue
			}

			// Parse the event
			var event models.InventoryEvent
			if err := json.Unmarshal(message.Value, &event); err != nil {
				log.Error().Err(err).
					Str("topic", message.Topic).
					Int("partition", message.Partition).
					Int64("offset", message.Offset).
					Msg("Failed to unmarshal event")

				// Commit the message to skip it
				if commitErr := c.eventsReader.CommitMessages(ctx, message); commitErr != nil {
					log.Error().Err(commitErr).Msg("Failed to commit invalid message")
				}
				continue
			}

			// Process the event with retry logic for transient failures
			processErr := c.processEventWithRetry(ctx, handler, &event, 3)
			if processErr != nil {
				log.Error().Err(processErr).
					Str("event_type", event.EventType).
					Str("sku", event.SKU).
					Str("event_id", event.EventID).
					Msg("Failed to handle event after retries")

				// DO NOT commit the message if processing failed after retries
				// This will cause Kafka to redeliver the message
				continue // Skip to next message, don't commit this one
			}

			// Only commit the message if processing was successful
			if err := c.eventsReader.CommitMessages(ctx, message); err != nil {
				log.Error().Err(err).
					Str("event_id", event.EventID).
					Msg("Failed to commit event message")
				// Don't continue here - we processed successfully but couldn't commit
				// The message might be redelivered, but that's better than losing it
			} else {
				log.Debug().
					Str("event_type", event.EventType).
					Str("sku", event.SKU).
					Str("event_id", event.EventID).
					Msg("Successfully processed and committed event")
			}
		}
	}
}

// ConsumeState starts consuming state updates and processes them with the provided handler
func (c *Consumer) ConsumeState(ctx context.Context, handler StateHandler) error {
	log.Info().Msg("Starting to consume inventory state updates")

	for {
		select {
		case <-ctx.Done():
			log.Info().Msg("Stopping state consumption")
			return ctx.Err()
		default:
			// Read message with timeout
			message, err := c.stateReader.FetchMessage(ctx)
			if err != nil {
				if err == context.Canceled {
					return nil
				}
				log.Error().Err(err).Msg("Failed to fetch state message")
				time.Sleep(time.Second) // Backoff on error
				continue
			}

			// Parse the state
			var state models.InventoryState
			if err := json.Unmarshal(message.Value, &state); err != nil {
				log.Error().Err(err).
					Str("topic", message.Topic).
					Int("partition", message.Partition).
					Int64("offset", message.Offset).
					Msg("Failed to unmarshal state")

				// Commit the message to skip it
				if commitErr := c.stateReader.CommitMessages(ctx, message); commitErr != nil {
					log.Error().Err(commitErr).Msg("Failed to commit invalid state message")
				}
				continue
			}

			// Process the state update
			if err := handler.HandleState(ctx, &state); err != nil {
				log.Error().Err(err).
					Str("sku", state.SKU).
					Msg("Failed to handle state update")
			}

			// Commit the message
			if err := c.stateReader.CommitMessages(ctx, message); err != nil {
				log.Error().Err(err).Msg("Failed to commit state message")
			} else {
				log.Debug().
					Str("sku", state.SKU).
					Msg("Successfully processed state update")
			}
		}
	}
}

// processEventWithRetry processes an event with exponential backoff retry logic
func (c *Consumer) processEventWithRetry(ctx context.Context, handler EventHandler, event *models.InventoryEvent, maxRetries int) error {
	for attempt := 0; attempt <= maxRetries; attempt++ {
		err := handler.HandleEvent(ctx, event)
		if err == nil {
			return nil // Success
		}

		// Check if this is a non-retryable error (e.g., validation errors)
		if isNonRetryableError(err) {
			log.Warn().Err(err).
				Str("event_id", event.EventID).
				Msg("Non-retryable error, skipping event")
			return err
		}

		if attempt < maxRetries {
			// Exponential backoff: 100ms, 200ms, 400ms
			backoff := time.Duration(100*(1<<attempt)) * time.Millisecond
			log.Warn().Err(err).
				Str("event_id", event.EventID).
				Int("attempt", attempt+1).
				Int("max_retries", maxRetries+1).
				Dur("backoff", backoff).
				Msg("Event processing failed, retrying after backoff")

			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(backoff):
				// Continue to next retry
			}
		}
	}

	return fmt.Errorf("event processing failed after %d attempts", maxRetries+1)
}

// isNonRetryableError determines if an error should not be retried
func isNonRetryableError(err error) bool {
	if err == nil {
		return false
	}

	errMsg := err.Error()
	// Add patterns for non-retryable errors
	nonRetryablePatterns := []string{
		"insufficient stock",
		"validation failed",
		"invalid event",
		"Event already processed", // Our deduplication check
	}

	for _, pattern := range nonRetryablePatterns {
		if strings.Contains(errMsg, pattern) {
			return true
		}
	}

	return false
}

// Close closes the Kafka readers
func (c *Consumer) Close() error {
	var errs []error

	if err := c.eventsReader.Close(); err != nil {
		errs = append(errs, fmt.Errorf("failed to close events reader: %w", err))
	}

	if err := c.stateReader.Close(); err != nil {
		errs = append(errs, fmt.Errorf("failed to close state reader: %w", err))
	}

	if len(errs) > 0 {
		return fmt.Errorf("errors closing consumers: %v", errs)
	}

	return nil
}
