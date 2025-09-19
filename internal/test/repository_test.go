package test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"inventory-service/internal/models"
)

// MockDB simula una base de datos en memoria para testing
type MockDB struct {
	inventories  map[string]*models.Inventory
	reservations map[uuid.UUID]*models.Reservation
	outboxEvents []models.OutboxEvent
	nextEventID  int
}

func NewMockDB() *MockDB {
	return &MockDB{
		inventories:  make(map[string]*models.Inventory),
		reservations: make(map[uuid.UUID]*models.Reservation),
		outboxEvents: make([]models.OutboxEvent, 0),
		nextEventID:  1,
	}
}

func (db *MockDB) GetInventory(sku string) *models.Inventory {
	return db.inventories[sku]
}

func (db *MockDB) SetInventory(inventory *models.Inventory) {
	db.inventories[inventory.SKU] = inventory
}

func (db *MockDB) UpdateInventory(sku string, availableQty, reservedQty int, version int64) error {
	if inv, exists := db.inventories[sku]; exists {
		if inv.Version != version {
			return assert.AnError // Simulate version conflict
		}
		inv.AvailableQty = availableQty
		inv.ReservedQty = reservedQty
		inv.Version++
		inv.UpdatedAt = time.Now()
		return nil
	}
	return assert.AnError // Inventory not found
}

func (db *MockDB) CreateReservation(reservation *models.Reservation) {
	db.reservations[reservation.ReservationID] = reservation
}

func (db *MockDB) GetReservation(id uuid.UUID) *models.Reservation {
	return db.reservations[id]
}

func (db *MockDB) GetReservationByIdempotencyKey(key string) *models.Reservation {
	for _, res := range db.reservations {
		if res.IdempotencyKey == key {
			return res
		}
	}
	return nil
}

func (db *MockDB) UpdateReservationStatus(id uuid.UUID, status models.ReservationStatus) error {
	if res, exists := db.reservations[id]; exists {
		res.Status = status
		res.UpdatedAt = time.Now()
		return nil
	}
	return assert.AnError
}

func (db *MockDB) GetExpiredReservations() []models.Reservation {
	var expired []models.Reservation
	now := time.Now()
	for _, res := range db.reservations {
		if res.Status == models.ReservationStatusPending && res.ExpiresAt.Before(now) {
			expired = append(expired, *res)
		}
	}
	return expired
}

func (db *MockDB) CreateOutboxEvent(eventType, key string, payload interface{}) {
	event := models.OutboxEvent{
		ID:               db.nextEventID,
		EventType:        eventType,
		Key:              key,
		Payload:          payload.(string),
		CreatedAt:        time.Now(),
		Published:        false,
		PublishAttempts:  0,
		ConsumedAt:       nil,
		ProcessedBy:      nil,
		ProcessingStatus: models.ProcessingStatusPending,
	}
	db.outboxEvents = append(db.outboxEvents, event)
	db.nextEventID++
}

func (db *MockDB) GetUnpublishedOutboxEvents() []models.OutboxEvent {
	var unpublished []models.OutboxEvent
	for _, event := range db.outboxEvents {
		if !event.Published {
			unpublished = append(unpublished, event)
		}
	}
	return unpublished
}

func TestMockDB_InventoryOperations(t *testing.T) {
	// Arrange
	db := NewMockDB()

	inventory := &models.Inventory{
		SKU:          "TEST-SKU",
		AvailableQty: 100,
		ReservedQty:  0,
		Version:      1,
		UpdatedAt:    time.Now(),
	}

	// Test SetInventory and GetInventory
	db.SetInventory(inventory)

	// Act & Assert
	retrieved := db.GetInventory("TEST-SKU")
	assert.NotNil(t, retrieved)
	assert.Equal(t, "TEST-SKU", retrieved.SKU)
	assert.Equal(t, 100, retrieved.AvailableQty)
	assert.Equal(t, 0, retrieved.ReservedQty)
	assert.Equal(t, int64(1), retrieved.Version)

	// Test UpdateInventory
	err := db.UpdateInventory("TEST-SKU", 95, 5, 1)
	assert.NoError(t, err)

	updated := db.GetInventory("TEST-SKU")
	assert.Equal(t, 95, updated.AvailableQty)
	assert.Equal(t, 5, updated.ReservedQty)
	assert.Equal(t, int64(2), updated.Version)

	// Test version conflict
	err = db.UpdateInventory("TEST-SKU", 90, 10, 1) // Wrong version
	assert.Error(t, err)
}

func TestMockDB_ReservationOperations(t *testing.T) {
	// Arrange
	db := NewMockDB()
	reservationID := uuid.New()

	reservation := &models.Reservation{
		ReservationID:  reservationID,
		SKU:            "TEST-SKU",
		Qty:            5,
		Status:         models.ReservationStatusPending,
		ExpiresAt:      time.Now().Add(24 * time.Hour),
		OwnerID:        "test-owner",
		IdempotencyKey: "test-key",
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
	}

	// Test CreateReservation and GetReservation
	db.CreateReservation(reservation)

	// Act & Assert
	retrieved := db.GetReservation(reservationID)
	assert.NotNil(t, retrieved)
	assert.Equal(t, reservationID, retrieved.ReservationID)
	assert.Equal(t, "TEST-SKU", retrieved.SKU)
	assert.Equal(t, 5, retrieved.Qty)
	assert.Equal(t, models.ReservationStatusPending, retrieved.Status)

	// Test GetReservationByIdempotencyKey
	byKey := db.GetReservationByIdempotencyKey("test-key")
	assert.NotNil(t, byKey)
	assert.Equal(t, reservationID, byKey.ReservationID)

	// Test UpdateReservationStatus
	err := db.UpdateReservationStatus(reservationID, models.ReservationStatusCommitted)
	assert.NoError(t, err)

	updated := db.GetReservation(reservationID)
	assert.Equal(t, models.ReservationStatusCommitted, updated.Status)
}

func TestMockDB_ExpiredReservations(t *testing.T) {
	// Arrange
	db := NewMockDB()

	// Create an expired reservation
	expiredReservation := &models.Reservation{
		ReservationID:  uuid.New(),
		SKU:            "TEST-SKU",
		Qty:            5,
		Status:         models.ReservationStatusPending,
		ExpiresAt:      time.Now().Add(-1 * time.Hour), // Expired 1 hour ago
		OwnerID:        "test-owner",
		IdempotencyKey: "expired-key",
		CreatedAt:      time.Now().Add(-2 * time.Hour),
		UpdatedAt:      time.Now().Add(-2 * time.Hour),
	}

	// Create a valid reservation
	validReservation := &models.Reservation{
		ReservationID:  uuid.New(),
		SKU:            "TEST-SKU-2",
		Qty:            3,
		Status:         models.ReservationStatusPending,
		ExpiresAt:      time.Now().Add(1 * time.Hour), // Expires in 1 hour
		OwnerID:        "test-owner-2",
		IdempotencyKey: "valid-key",
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
	}

	db.CreateReservation(expiredReservation)
	db.CreateReservation(validReservation)

	// Act
	expired := db.GetExpiredReservations()

	// Assert
	assert.Len(t, expired, 1)
	assert.Equal(t, expiredReservation.ReservationID, expired[0].ReservationID)
}

func TestMockDB_OutboxOperations(t *testing.T) {
	// Arrange
	db := NewMockDB()

	// Test CreateOutboxEvent
	db.CreateOutboxEvent("test_event", "test-key", `{"data": "test"}`)
	db.CreateOutboxEvent("another_event", "another-key", `{"data": "another"}`)

	// Act
	unpublished := db.GetUnpublishedOutboxEvents()

	// Assert
	assert.Len(t, unpublished, 2)
	assert.Equal(t, "test_event", unpublished[0].EventType)
	assert.Equal(t, "test-key", unpublished[0].Key)
	assert.False(t, unpublished[0].Published)
	assert.Equal(t, 0, unpublished[0].PublishAttempts)

	// Mark first event as published
	db.outboxEvents[0].Published = true

	unpublishedAfter := db.GetUnpublishedOutboxEvents()
	assert.Len(t, unpublishedAfter, 1)
	assert.Equal(t, "another_event", unpublishedAfter[0].EventType)
}

func TestInventoryBusinessLogic_ReservationWorkflow(t *testing.T) {
	// Arrange
	db := NewMockDB()

	// Setup initial inventory
	inventory := &models.Inventory{
		SKU:          "WORKFLOW-SKU",
		AvailableQty: 10,
		ReservedQty:  0,
		Version:      1,
		UpdatedAt:    time.Now(),
	}
	db.SetInventory(inventory)

	// Test complete reservation workflow
	reservationID := uuid.New()

	// Step 1: Create reservation
	reservation := &models.Reservation{
		ReservationID:  reservationID,
		SKU:            "WORKFLOW-SKU",
		Qty:            3,
		Status:         models.ReservationStatusPending,
		ExpiresAt:      time.Now().Add(24 * time.Hour),
		OwnerID:        "workflow-owner",
		IdempotencyKey: "workflow-key",
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
	}
	db.CreateReservation(reservation)

	// Step 2: Update inventory to reflect reservation
	err := db.UpdateInventory("WORKFLOW-SKU", 7, 3, 1)
	require.NoError(t, err)

	// Step 3: Verify state after reservation
	updatedInventory := db.GetInventory("WORKFLOW-SKU")
	assert.Equal(t, 7, updatedInventory.AvailableQty)
	assert.Equal(t, 3, updatedInventory.ReservedQty)
	assert.Equal(t, int64(2), updatedInventory.Version)

	createdReservation := db.GetReservation(reservationID)
	assert.Equal(t, models.ReservationStatusPending, createdReservation.Status)

	// Step 4: Commit reservation
	err = db.UpdateReservationStatus(reservationID, models.ReservationStatusCommitted)
	require.NoError(t, err)

	// Update inventory to reflect commitment (remove from reserved)
	err = db.UpdateInventory("WORKFLOW-SKU", 7, 0, 2)
	require.NoError(t, err)

	// Step 5: Verify final state
	finalInventory := db.GetInventory("WORKFLOW-SKU")
	assert.Equal(t, 7, finalInventory.AvailableQty)
	assert.Equal(t, 0, finalInventory.ReservedQty)
	assert.Equal(t, int64(3), finalInventory.Version)

	committedReservation := db.GetReservation(reservationID)
	assert.Equal(t, models.ReservationStatusCommitted, committedReservation.Status)
}
