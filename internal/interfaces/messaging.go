package interfaces

import (
	"context"

	"inventory-service/internal/models"
)

// MessagePublisher defines the contract for publishing events
type MessagePublisher interface {
	PublishEvent(ctx context.Context, event *models.InventoryEvent) error
	PublishOutboxEvent(ctx context.Context, event *models.OutboxEvent) error
	PublishState(ctx context.Context, state *models.InventoryState) error
	Close() error
}

// MessageConsumer defines the contract for consuming events
type MessageConsumer interface {
	ConsumeEvents(ctx context.Context, handler EventHandler) error
	ConsumeState(ctx context.Context, handler StateHandler) error
	Close() error
}

// Event and State handlers
type EventHandler interface {
	HandleEvent(ctx context.Context, event *models.InventoryEvent) error
}

type StateHandler interface {
	HandleState(ctx context.Context, state *models.InventoryState) error
}
