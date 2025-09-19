package repository

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/rs/zerolog/log"

	"inventory-service/internal/models"
)

// InventoryRepository handles database operations for inventory
type InventoryRepository struct {
	db         *sqlx.DB
	outboxRepo *OutboxRepository
}

// NewInventoryRepository creates a new inventory repository
func NewInventoryRepository(db *sqlx.DB) *InventoryRepository {
	return &InventoryRepository{
		db:         db,
		outboxRepo: NewOutboxRepository(db),
	}
}

// GetInventory retrieves inventory by SKU
func (r *InventoryRepository) GetInventory(ctx context.Context, sku string) (*models.Inventory, error) {
	var inventory models.Inventory
	query := `SELECT sku, available_qty, reserved_qty, version, updated_at 
			  FROM inventory WHERE sku = $1`

	err := r.db.GetContext(ctx, &inventory, query, sku)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		log.Error().Err(err).Str("sku", sku).Msg("Failed to get inventory")
		return nil, fmt.Errorf("failed to get inventory: %w", err)
	}

	return &inventory, nil
}

// GetInventoryForUpdate retrieves inventory by SKU with row lock for update
func (r *InventoryRepository) GetInventoryForUpdate(ctx context.Context, tx *sqlx.Tx, sku string) (*models.Inventory, error) {
	var inventory models.Inventory
	query := `SELECT sku, available_qty, reserved_qty, version, updated_at 
			  FROM inventory WHERE sku = $1 FOR UPDATE`

	err := tx.GetContext(ctx, &inventory, query, sku)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		log.Error().Err(err).Str("sku", sku).Msg("Failed to get inventory for update")
		return nil, fmt.Errorf("failed to get inventory for update: %w", err)
	}

	return &inventory, nil
}

// UpdateInventory updates inventory quantities and version
func (r *InventoryRepository) UpdateInventory(ctx context.Context, tx *sqlx.Tx, inventory *models.Inventory) error {
	query := `UPDATE inventory 
			  SET available_qty = $2, reserved_qty = $3, version = version + 1, updated_at = NOW() 
			  WHERE sku = $1 AND version = $4`

	result, err := tx.ExecContext(ctx, query, inventory.SKU, inventory.AvailableQty, inventory.ReservedQty, inventory.Version)
	if err != nil {
		log.Error().Err(err).Str("sku", inventory.SKU).Msg("Failed to update inventory")
		return fmt.Errorf("failed to update inventory: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get affected rows: %w", err)
	}

	if rowsAffected == 0 {
		return fmt.Errorf("optimistic lock failed: inventory version mismatch")
	}

	// Update the version in the struct for caller reference
	inventory.Version++
	inventory.UpdatedAt = time.Now()

	return nil
}

// CreateInventory creates a new inventory record
func (r *InventoryRepository) CreateInventory(ctx context.Context, tx *sqlx.Tx, inventory *models.Inventory) error {
	query := `INSERT INTO inventory (sku, available_qty, reserved_qty, version, updated_at) 
			  VALUES ($1, $2, $3, $4, NOW())`

	_, err := tx.ExecContext(ctx, query, inventory.SKU, inventory.AvailableQty, inventory.ReservedQty, inventory.Version)
	if err != nil {
		log.Error().Err(err).Str("sku", inventory.SKU).Msg("Failed to create inventory")
		return fmt.Errorf("failed to create inventory: %w", err)
	}

	inventory.Version = 1
	inventory.UpdatedAt = time.Now()

	return nil
}

// GetReservation retrieves a reservation by ID
func (r *InventoryRepository) GetReservation(ctx context.Context, reservationID uuid.UUID) (*models.Reservation, error) {
	var reservation models.Reservation
	query := `SELECT reservation_id, sku, qty, status, expires_at, owner_id, idempotency_key, created_at, updated_at 
			  FROM reservation WHERE reservation_id = $1`

	err := r.db.GetContext(ctx, &reservation, query, reservationID)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		log.Error().Err(err).Str("reservation_id", reservationID.String()).Msg("Failed to get reservation")
		return nil, fmt.Errorf("failed to get reservation: %w", err)
	}

	return &reservation, nil
}

// GetReservationByIdempotencyKey retrieves a reservation by idempotency key
func (r *InventoryRepository) GetReservationByIdempotencyKey(ctx context.Context, idempotencyKey string) (*models.Reservation, error) {
	var reservation models.Reservation
	query := `SELECT reservation_id, sku, qty, status, expires_at, owner_id, idempotency_key, created_at, updated_at 
			  FROM reservation WHERE idempotency_key = $1`

	err := r.db.GetContext(ctx, &reservation, query, idempotencyKey)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		log.Error().Err(err).Str("idempotency_key", idempotencyKey).Msg("Failed to get reservation by idempotency key")
		return nil, fmt.Errorf("failed to get reservation by idempotency key: %w", err)
	}

	return &reservation, nil
}

// CreateReservation creates a new reservation
func (r *InventoryRepository) CreateReservation(ctx context.Context, tx *sqlx.Tx, reservation *models.Reservation) error {
	query := `INSERT INTO reservation (reservation_id, sku, qty, status, expires_at, owner_id, idempotency_key, created_at, updated_at) 
			  VALUES ($1, $2, $3, $4, $5, $6, $7, NOW(), NOW())`

	_, err := tx.ExecContext(ctx, query, reservation.ReservationID, reservation.SKU, reservation.Qty,
		reservation.Status, reservation.ExpiresAt, reservation.OwnerID, reservation.IdempotencyKey)
	if err != nil {
		log.Error().Err(err).Str("reservation_id", reservation.ReservationID.String()).Msg("Failed to create reservation")
		return fmt.Errorf("failed to create reservation: %w", err)
	}

	now := time.Now()
	reservation.CreatedAt = now
	reservation.UpdatedAt = now

	return nil
}

// UpdateReservationStatus updates the status of a reservation
func (r *InventoryRepository) UpdateReservationStatus(ctx context.Context, tx *sqlx.Tx, reservationID uuid.UUID, status models.ReservationStatus) error {
	query := `UPDATE reservation SET status = $2, updated_at = NOW() WHERE reservation_id = $1`

	result, err := tx.ExecContext(ctx, query, reservationID, status)
	if err != nil {
		log.Error().Err(err).Str("reservation_id", reservationID.String()).Msg("Failed to update reservation status")
		return fmt.Errorf("failed to update reservation status: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get affected rows: %w", err)
	}

	if rowsAffected == 0 {
		return fmt.Errorf("reservation not found")
	}

	return nil
}

// GetExpiredReservations retrieves all expired PENDING reservations
func (r *InventoryRepository) GetExpiredReservations(ctx context.Context) ([]models.Reservation, error) {
	var reservations []models.Reservation
	query := `SELECT reservation_id, sku, qty, status, expires_at, owner_id, idempotency_key, created_at, updated_at 
			  FROM reservation 
			  WHERE status = $1 AND expires_at < NOW()
			  ORDER BY expires_at ASC
			  LIMIT 100`

	err := r.db.SelectContext(ctx, &reservations, query, models.ReservationStatusPending)
	if err != nil {
		log.Error().Err(err).Msg("Failed to get expired reservations")
		return nil, fmt.Errorf("failed to get expired reservations: %w", err)
	}

	return reservations, nil
}

// CreateOutboxEvent creates a new outbox event for reliable message publishing
func (r *InventoryRepository) CreateOutboxEvent(ctx context.Context, tx *sqlx.Tx, eventType, key string, payload interface{}) error {
	return r.outboxRepo.InsertOutboxEvent(ctx, tx, eventType, key, payload)
}

// GetUnpublishedOutboxEvents retrieves outbox events that haven't been published yet
// Uses ORDER BY id ASC to ensure events are processed in insertion order
func (r *InventoryRepository) GetUnpublishedOutboxEvents(ctx context.Context) ([]models.OutboxEvent, error) {
	var events []models.OutboxEvent
	query := `SELECT id, event_type, key, payload, created_at, published, publish_attempts, last_error 
			  FROM outbox 
			  WHERE published = FALSE 
			  ORDER BY id ASC 
			  LIMIT 100`

	err := r.db.SelectContext(ctx, &events, query)
	if err != nil {
		log.Error().Err(err).Msg("Failed to get unpublished outbox events")
		return nil, fmt.Errorf("failed to get unpublished outbox events: %w", err)
	}

	return events, nil
}

// BeginTx starts a new database transaction
func (r *InventoryRepository) BeginTx(ctx context.Context) (*sqlx.Tx, error) {
	tx, err := r.db.BeginTxx(ctx, nil)
	if err != nil {
		log.Error().Err(err).Msg("Failed to begin transaction")
		return nil, fmt.Errorf("failed to begin transaction: %w", err)
	}
	return tx, nil
}
