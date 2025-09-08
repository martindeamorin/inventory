package test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"

	"inventory-service/internal/models"
	"inventory-service/internal/service"
)

// MockInventoryRepository implementa el interface repository para testing
type MockInventoryRepository struct {
	mock.Mock
}

func (m *MockInventoryRepository) BeginTx(ctx context.Context) (*sqlx.Tx, error) {
	args := m.Called(ctx)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*sqlx.Tx), args.Error(1)
}

func (m *MockInventoryRepository) GetInventory(ctx context.Context, sku string) (*models.Inventory, error) {
	args := m.Called(ctx, sku)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*models.Inventory), args.Error(1)
}

func (m *MockInventoryRepository) GetInventoryForUpdate(ctx context.Context, tx *sqlx.Tx, sku string) (*models.Inventory, error) {
	args := m.Called(ctx, tx, sku)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*models.Inventory), args.Error(1)
}

func (m *MockInventoryRepository) UpdateInventory(ctx context.Context, tx *sqlx.Tx, inventory *models.Inventory) error {
	args := m.Called(ctx, tx, inventory)
	return args.Error(0)
}

func (m *MockInventoryRepository) CreateInventory(ctx context.Context, tx *sqlx.Tx, inventory *models.Inventory) error {
	args := m.Called(ctx, tx, inventory)
	return args.Error(0)
}

func (m *MockInventoryRepository) GetReservation(ctx context.Context, reservationID uuid.UUID) (*models.Reservation, error) {
	args := m.Called(ctx, reservationID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*models.Reservation), args.Error(1)
}

func (m *MockInventoryRepository) GetReservationByIdempotencyKey(ctx context.Context, idempotencyKey string) (*models.Reservation, error) {
	args := m.Called(ctx, idempotencyKey)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*models.Reservation), args.Error(1)
}

func (m *MockInventoryRepository) CreateReservation(ctx context.Context, tx *sqlx.Tx, reservation *models.Reservation) error {
	args := m.Called(ctx, tx, reservation)
	return args.Error(0)
}

func (m *MockInventoryRepository) UpdateReservationStatus(ctx context.Context, tx *sqlx.Tx, reservationID uuid.UUID, status models.ReservationStatus) error {
	args := m.Called(ctx, tx, reservationID, status)
	return args.Error(0)
}

func (m *MockInventoryRepository) GetExpiredReservations(ctx context.Context) ([]models.Reservation, error) {
	args := m.Called(ctx)
	return args.Get(0).([]models.Reservation), args.Error(1)
}

func (m *MockInventoryRepository) CreateOutboxEvent(ctx context.Context, tx *sqlx.Tx, eventType, key string, payload interface{}) error {
	args := m.Called(ctx, tx, eventType, key, payload)
	return args.Error(0)
}

func (m *MockInventoryRepository) GetUnpublishedOutboxEvents(ctx context.Context) ([]models.OutboxEvent, error) {
	args := m.Called(ctx)
	return args.Get(0).([]models.OutboxEvent), args.Error(1)
}

// MockCacheRepository implementa el interface cache para testing
type MockCacheRepository struct {
	mock.Mock
}

func (m *MockCacheRepository) GetInventory(ctx context.Context, sku string) (*models.Inventory, error) {
	args := m.Called(ctx, sku)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*models.Inventory), args.Error(1)
}

func (m *MockCacheRepository) GetAvailableStock(ctx context.Context, sku string) (int, error) {
	args := m.Called(ctx, sku)
	return args.Int(0), args.Error(1)
}

func (m *MockCacheRepository) GetAvailableStockWithTimeout(ctx context.Context, sku string, timeout time.Duration) (int, error) {
	args := m.Called(ctx, sku, timeout)
	return args.Int(0), args.Error(1)
}

func (m *MockCacheRepository) SetInventory(ctx context.Context, inventory *models.Inventory) error {
	args := m.Called(ctx, inventory)
	return args.Error(0)
}

func (m *MockCacheRepository) DeleteInventory(ctx context.Context, sku string) error {
	args := m.Called(ctx, sku)
	return args.Error(0)
}

func (m *MockCacheRepository) UpdateInventoryFromState(ctx context.Context, state *models.InventoryState) error {
	args := m.Called(ctx, state)
	return args.Error(0)
}

func (m *MockCacheRepository) Close() error {
	args := m.Called()
	return args.Error(0)
}

// MockMessagePublisher implementa el interface publisher para testing
type MockMessagePublisher struct {
	mock.Mock
}

func (m *MockMessagePublisher) PublishEvent(ctx context.Context, event *models.InventoryEvent) error {
	args := m.Called(ctx, event)
	return args.Error(0)
}

func (m *MockMessagePublisher) PublishOutboxEvent(ctx context.Context, event *models.OutboxEvent) error {
	args := m.Called(ctx, event)
	return args.Error(0)
}

func (m *MockMessagePublisher) PublishState(ctx context.Context, state *models.InventoryState) error {
	args := m.Called(ctx, state)
	return args.Error(0)
}

func (m *MockMessagePublisher) Close() error {
	args := m.Called()
	return args.Error(0)
}

func TestInventoryService_GetAvailability_CacheHit(t *testing.T) {
	// Arrange
	mockRepo := new(MockInventoryRepository)
	mockCache := new(MockCacheRepository)
	mockPublisher := new(MockMessagePublisher)

	cfg := service.ServiceConfig{
		ReservationTTL:           24 * time.Hour,
		MaxReasonableReservation: 1000,
		CacheTimeout:             5 * time.Second,
		StockSafetyBuffer:        0.1,
		EnableStockPreCheck:      true,
	}

	inventoryService, err := service.NewInventoryService(mockRepo, nil, mockPublisher, mockCache, cfg)
	assert.NoError(t, err)

	expectedInventory := &models.Inventory{
		SKU:          "TEST-SKU",
		AvailableQty: 100,
		ReservedQty:  10,
		Version:      1,
		UpdatedAt:    time.Now(),
	}

	mockCache.On("GetInventory", mock.Anything, "TEST-SKU").Return(expectedInventory, nil)

	// Act
	result, err := inventoryService.GetAvailability(context.Background(), "TEST-SKU")

	// Assert
	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, "TEST-SKU", result.SKU)
	assert.Equal(t, 100, result.AvailableQty)
	assert.Equal(t, 10, result.ReservedQty)
	assert.True(t, result.CacheHit)
	assert.Equal(t, expectedInventory.UpdatedAt, result.LastUpdated)

	mockCache.AssertExpectations(t)
}

func TestInventoryService_GetAvailability_CacheMiss(t *testing.T) {
	// Arrange
	mockRepo := new(MockInventoryRepository)
	mockCache := new(MockCacheRepository)
	mockPublisher := new(MockMessagePublisher)

	cfg := service.ServiceConfig{
		ReservationTTL:           24 * time.Hour,
		MaxReasonableReservation: 1000,
		CacheTimeout:             5 * time.Second,
		StockSafetyBuffer:        0.1,
		EnableStockPreCheck:      true,
	}

	inventoryService, err := service.NewInventoryService(mockRepo, nil, mockPublisher, mockCache, cfg)
	assert.NoError(t, err)

	expectedInventory := &models.Inventory{
		SKU:          "TEST-SKU",
		AvailableQty: 100,
		ReservedQty:  10,
		Version:      1,
		UpdatedAt:    time.Now(),
	}

	mockCache.On("GetInventory", mock.Anything, "TEST-SKU").Return(nil, assert.AnError)
	mockRepo.On("GetInventory", mock.Anything, "TEST-SKU").Return(expectedInventory, nil)
	// The SetInventory call happens asynchronously in a goroutine, so we make it optional
	mockCache.On("SetInventory", mock.Anything, mock.Anything).Return(nil).Maybe()

	// Act
	result, err := inventoryService.GetAvailability(context.Background(), "TEST-SKU")

	// Assert
	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, "TEST-SKU", result.SKU)
	assert.Equal(t, 100, result.AvailableQty)
	assert.Equal(t, 10, result.ReservedQty)
	assert.False(t, result.CacheHit)
	assert.Equal(t, expectedInventory.UpdatedAt, result.LastUpdated)

	mockCache.AssertExpectations(t)
	mockRepo.AssertExpectations(t)
}

func TestInventoryService_GetAvailability_NotFound(t *testing.T) {
	// Arrange
	mockRepo := new(MockInventoryRepository)
	mockCache := new(MockCacheRepository)
	mockPublisher := new(MockMessagePublisher)

	cfg := service.ServiceConfig{
		ReservationTTL:           24 * time.Hour,
		MaxReasonableReservation: 1000,
		CacheTimeout:             5 * time.Second,
		StockSafetyBuffer:        0.1,
		EnableStockPreCheck:      true,
	}

	inventoryService, err := service.NewInventoryService(mockRepo, nil, mockPublisher, mockCache, cfg)
	assert.NoError(t, err)

	mockCache.On("GetInventory", mock.Anything, "NONEXISTENT").Return(nil, assert.AnError)
	mockRepo.On("GetInventory", mock.Anything, "NONEXISTENT").Return(nil, nil)

	// Act
	result, err := inventoryService.GetAvailability(context.Background(), "NONEXISTENT")

	// Assert
	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, "NONEXISTENT", result.SKU)
	assert.Equal(t, 0, result.AvailableQty)
	assert.Equal(t, 0, result.ReservedQty)
	assert.False(t, result.CacheHit)
	assert.WithinDuration(t, time.Now(), result.LastUpdated, 1*time.Second)

	mockCache.AssertExpectations(t)
	mockRepo.AssertExpectations(t)
}

func TestServiceConfig_Validate(t *testing.T) {
	// Test configuración válida
	validConfig := service.ServiceConfig{
		ReservationTTL:           24 * time.Hour,
		MaxReasonableReservation: 1000,
		CacheTimeout:             5 * time.Second,
		StockSafetyBuffer:        0.1,
		EnableStockPreCheck:      true,
	}

	err := validConfig.Validate()
	assert.NoError(t, err)

	// Test ReservationTTL inválido
	invalidConfig1 := validConfig
	invalidConfig1.ReservationTTL = 30 * time.Second
	err = invalidConfig1.Validate()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "reservation TTL must be at least 1 minute")

	// Test MaxReasonableReservation inválido
	invalidConfig2 := validConfig
	invalidConfig2.MaxReasonableReservation = 0
	err = invalidConfig2.Validate()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "max reasonable reservation must be positive")

	// Test CacheTimeout inválido
	invalidConfig3 := validConfig
	invalidConfig3.CacheTimeout = 0
	err = invalidConfig3.Validate()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "cache timeout must be at least 1ms")

	// Test StockSafetyBuffer inválido
	invalidConfig4 := validConfig
	invalidConfig4.StockSafetyBuffer = 1.5
	err = invalidConfig4.Validate()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "stock safety buffer must be between 0 and 1")
}
