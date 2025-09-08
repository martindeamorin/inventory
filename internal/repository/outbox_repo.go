package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/lib/pq"
	"github.com/rs/zerolog/log"
)

// OutboxEvent represents an event in the outbox table
type OutboxEvent struct {
	ID              int       `db:"id" json:"id"`
	EventType       string    `db:"event_type" json:"event_type"`
	Key             string    `db:"key" json:"key"`
	Payload         string    `db:"payload" json:"payload"`
	CreatedAt       time.Time `db:"created_at" json:"created_at"`
	Published       bool      `db:"published" json:"published"`
	PublishAttempts int       `db:"publish_attempts" json:"publish_attempts"`
	LastError       *string   `db:"last_error" json:"last_error,omitempty"`
}

// OutboxRepository handles outbox operations with advisory locking
type OutboxRepository struct {
	db *sqlx.DB
}

// NewOutboxRepository creates a new outbox repository
func NewOutboxRepository(db *sqlx.DB) *OutboxRepository {
	return &OutboxRepository{db: db}
}

// TryAcquireOutboxLock attempts to acquire a PostgreSQL advisory lock
// Returns true if lock was acquired, false if another worker has it
func (r *OutboxRepository) TryAcquireOutboxLock(ctx context.Context, lockKey int64) (bool, error) {
	var acquired bool
	query := "SELECT pg_try_advisory_lock($1)"

	err := r.db.QueryRowContext(ctx, query, lockKey).Scan(&acquired)
	if err != nil && err != sql.ErrNoRows {
		log.Error().Err(err).Int64("lock_key", lockKey).Msg("Failed to acquire advisory lock")
		return false, fmt.Errorf("failed to acquire advisory lock: %w", err)
	}

	if acquired {
		log.Debug().Int64("lock_key", lockKey).Msg("Successfully acquired outbox advisory lock")
	} else {
		log.Debug().Int64("lock_key", lockKey).Msg("Advisory lock already held by another worker")
	}

	return acquired, nil
}

// ReleaseOutboxLock releases the PostgreSQL advisory lock
func (r *OutboxRepository) ReleaseOutboxLock(ctx context.Context, lockKey int64) error {
	query := "SELECT pg_advisory_unlock($1)"

	var released bool
	err := r.db.QueryRowContext(ctx, query, lockKey).Scan(&released)
	if err != nil {
		log.Error().Err(err).Int64("lock_key", lockKey).Msg("Failed to release advisory lock")
		return fmt.Errorf("failed to release advisory lock: %w", err)
	}

	if released {
		log.Debug().Int64("lock_key", lockKey).Msg("Successfully released outbox advisory lock")
	} else {
		log.Warn().Int64("lock_key", lockKey).Msg("Advisory lock was not held when trying to release")
	}

	return nil
}

// FetchOutboxBatchOrdered fetches unpublished events in order (ORDER BY id ASC)
// Uses FOR UPDATE SKIP LOCKED to prevent conflicts between workers
func (r *OutboxRepository) FetchOutboxBatchOrdered(ctx context.Context, limit int) ([]OutboxEvent, error) {
	query := `
		SELECT id, event_type, key, payload, created_at, published, publish_attempts, last_error
		FROM outbox
		WHERE published = false
		ORDER BY id ASC
		FOR UPDATE SKIP LOCKED
		LIMIT $1
	`

	tx, err := r.db.BeginTxx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() {
		if err := tx.Rollback(); err != nil && err != sql.ErrTxDone {
			log.Error().Err(err).Msg("Failed to rollback transaction")
		}
	}()

	rows, err := tx.QueryxContext(ctx, query, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to query outbox events: %w", err)
	}
	defer rows.Close()

	var events []OutboxEvent
	for rows.Next() {
		var event OutboxEvent
		if err := rows.StructScan(&event); err != nil {
			return nil, fmt.Errorf("failed to scan outbox event: %w", err)
		}
		events = append(events, event)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating outbox rows: %w", err)
	}

	// Commit transaction to hold the locks until processing
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("failed to commit transaction: %w", err)
	}

	log.Debug().Int("count", len(events)).Msg("Fetched outbox events for processing")
	return events, nil
}

// MarkOutboxPublished marks events as successfully published
func (r *OutboxRepository) MarkOutboxPublished(ctx context.Context, ids []int64) error {
	if len(ids) == 0 {
		return nil
	}

	query := `
		UPDATE outbox 
		SET published = true, 
		    published_at = NOW() 
		WHERE id = ANY($1)
	`

	result, err := r.db.ExecContext(ctx, query, pq.Array(ids))
	if err != nil {
		log.Error().Err(err).Interface("ids", ids).Msg("Failed to mark outbox events as published")
		return fmt.Errorf("failed to mark outbox events as published: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get affected rows: %w", err)
	}

	log.Info().
		Interface("ids", ids).
		Int64("rows_affected", rowsAffected).
		Msg("Marked outbox events as published")

	return nil
}

// IncrementPublishAttempts increments the publish attempts counter and records error
func (r *OutboxRepository) IncrementPublishAttempts(ctx context.Context, id int64, lastError string) error {
	query := `
		UPDATE outbox 
		SET publish_attempts = publish_attempts + 1,
		    last_error = $2,
		    updated_at = NOW()
		WHERE id = $1
	`

	result, err := r.db.ExecContext(ctx, query, id, lastError)
	if err != nil {
		log.Error().Err(err).Int64("id", id).Msg("Failed to increment publish attempts")
		return fmt.Errorf("failed to increment publish attempts: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get affected rows: %w", err)
	}

	if rowsAffected == 0 {
		log.Warn().Int64("id", id).Msg("No outbox event found to increment attempts")
	} else {
		log.Debug().Int64("id", id).Str("error", lastError).Msg("Incremented publish attempts")
	}

	return nil
}

// InsertOutboxEvent inserts a new event into the outbox
func (r *OutboxRepository) InsertOutboxEvent(ctx context.Context, tx *sqlx.Tx, eventType, key string, payload interface{}) error {
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal payload: %w", err)
	}

	query := `
		INSERT INTO outbox (event_type, key, payload, created_at)
		VALUES ($1, $2, $3, NOW())
	`

	var executor interface {
		ExecContext(context.Context, string, ...interface{}) (sql.Result, error)
	}

	if tx != nil {
		executor = tx
	} else {
		executor = r.db
	}

	_, err = executor.ExecContext(ctx, query, eventType, key, string(payloadJSON))
	if err != nil {
		log.Error().Err(err).
			Str("event_type", eventType).
			Str("key", key).
			Msg("Failed to insert outbox event")
		return fmt.Errorf("failed to insert outbox event: %w", err)
	}

	log.Debug().
		Str("event_type", eventType).
		Str("key", key).
		Msg("Inserted outbox event")

	return nil
}
