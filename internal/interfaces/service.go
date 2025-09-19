package interfaces

import (
	"context"

	"github.com/google/uuid"

	"inventory-service/internal/models"
)

// InventoryService defines the contract for inventory business operations
type InventoryService interface {
	// Core inventory operations
	ReserveStock(ctx context.Context, sku string, req *models.ReserveRequest) (*models.ReserveResponse, error)
	CommitReservation(ctx context.Context, reservationID uuid.UUID, req *models.CommitRequest) error
	ReleaseReservation(ctx context.Context, reservationID uuid.UUID, req *models.ReleaseRequest) error

	// Query operations
	GetAvailability(ctx context.Context, sku string) (*models.AvailabilityResponse, error)
}

// ProcessorService defines the contract for event processing
type ProcessorService interface {
	HandleEvent(ctx context.Context, event *models.InventoryEvent) error
	StartExpirationSweeper(ctx context.Context) error
	StartProcessedEventsCleanup(ctx context.Context) error
}

// ReaderService defines the contract for read operations
type ReaderService interface {
	GetAvailability(ctx context.Context, sku string) (*models.AvailabilityResponse, error)
	vailability(ctx context.Context, skus []string) (map[string]*models.AvailabilityResponse, error)
}
