package interfaces

import (
	"context"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"

	"inventory-service/internal/models"
)

// InventoryRepository defines the contract for inventory data operations
type InventoryRepository interface {
	// Transaction management
	BeginTx(ctx context.Context) (*sqlx.Tx, error)

	// Inventory operations
	GetInventory(ctx context.Context, sku string) (*models.Inventory, error)
	GetInventoryForUpdate(ctx context.Context, tx *sqlx.Tx, sku string) (*models.Inventory, error)
	UpdateInventory(ctx context.Context, tx *sqlx.Tx, inventory *models.Inventory) error
	CreateInventory(ctx context.Context, tx *sqlx.Tx, inventory *models.Inventory) error

	// Reservation operations
	GetReservation(ctx context.Context, reservationID uuid.UUID) (*models.Reservation, error)
	GetReservationByIdempotencyKey(ctx context.Context, idempotencyKey string) (*models.Reservation, error)
	CreateReservation(ctx context.Context, tx *sqlx.Tx, reservation *models.Reservation) error
	UpdateReservationStatus(ctx context.Context, tx *sqlx.Tx, reservationID uuid.UUID, status models.ReservationStatus) error
	GetExpiredReservations(ctx context.Context) ([]models.Reservation, error)

	// Outbox operations
	CreateOutboxEvent(ctx context.Context, tx *sqlx.Tx, eventType, key string, payload interface{}) error
	GetUnpublishedOutboxEvents(ctx context.Context) ([]models.OutboxEvent, error)
}

// CacheRepository defines the contract for caching operations
type CacheRepository interface {
	GetInventory(ctx context.Context, sku string) (*models.Inventory, error)
	GetAvailableStock(ctx context.Context, sku string) (int, error)
	SetInventory(ctx context.Context, inventory *models.Inventory) error
	DeleteInventory(ctx context.Context, sku string) error
	UpdateInventoryFromState(ctx context.Context, state *models.InventoryState) error
	Close() error
}
