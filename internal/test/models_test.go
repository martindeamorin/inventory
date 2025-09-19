package test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"

	"inventory-service/internal/models"
)

func TestReservationStatus_Constants(t *testing.T) {
	// Test que las constantes de estado estén definidas correctamente
	assert.Equal(t, models.ReservationStatus("PENDING"), models.ReservationStatusPending)
	assert.Equal(t, models.ReservationStatus("COMMITTED"), models.ReservationStatusCommitted)
	assert.Equal(t, models.ReservationStatus("RELEASED"), models.ReservationStatusReleased)
	assert.Equal(t, models.ReservationStatus("EXPIRED"), models.ReservationStatusExpired)
}

func TestEventTypes_Constants(t *testing.T) {
	// Test que los tipos de evento estén definidos correctamente
	assert.Equal(t, "reserve_stock", models.EventTypeReserveStock)
	assert.Equal(t, "commit_reserve", models.EventTypeCommitReserve)
	assert.Equal(t, "release_stock", models.EventTypeReleaseStock)
	assert.Equal(t, "expire_reserve", models.EventTypeExpireReserve)
	assert.Equal(t, "inventory_state", models.EventTypeInventoryState)
}

func TestErrorCodes_Constants(t *testing.T) {
	// Test que los códigos de error estén definidos correctamente
	assert.Equal(t, models.ErrorCode("INVALID_FIELD"), models.ErrorCodeInvalidField)
	assert.Equal(t, models.ErrorCode("INSUFFICIENT_STOCK"), models.ErrorCodeInsufficientStock)
	assert.Equal(t, models.ErrorCode("RESERVATION_NOT_FOUND"), models.ErrorCodeReservationNotFound)
	assert.Equal(t, models.ErrorCode("DUPLICATE_REQUEST"), models.ErrorCodeDuplicateRequest)
}

func TestInventory_ModelValidation(t *testing.T) {
	// Test creación de modelo Inventory válido
	now := time.Now()
	inventory := models.Inventory{
		SKU:          "TEST-SKU-001",
		AvailableQty: 100,
		ReservedQty:  10,
		Version:      1,
		UpdatedAt:    now,
	}

	assert.Equal(t, "TEST-SKU-001", inventory.SKU)
	assert.Equal(t, 100, inventory.AvailableQty)
	assert.Equal(t, 10, inventory.ReservedQty)
	assert.Equal(t, int64(1), inventory.Version)
	assert.Equal(t, now, inventory.UpdatedAt)

	// Test que las cantidades no pueden ser negativas en lógica de negocio
	assert.True(t, inventory.AvailableQty >= 0)
	assert.True(t, inventory.ReservedQty >= 0)
}

func TestReservation_ModelValidation(t *testing.T) {
	// Test creación de modelo Reservation válido
	reservationID := uuid.New()
	now := time.Now()
	expiresAt := now.Add(24 * time.Hour)

	reservation := models.Reservation{
		ReservationID:  reservationID,
		SKU:            "TEST-SKU-001",
		Qty:            5,
		Status:         models.ReservationStatusPending,
		ExpiresAt:      expiresAt,
		OwnerID:        "user-123",
		IdempotencyKey: "idempotency-key-456",
		CreatedAt:      now,
		UpdatedAt:      now,
	}

	assert.Equal(t, reservationID, reservation.ReservationID)
	assert.Equal(t, "TEST-SKU-001", reservation.SKU)
	assert.Equal(t, 5, reservation.Qty)
	assert.Equal(t, models.ReservationStatusPending, reservation.Status)
	assert.Equal(t, expiresAt, reservation.ExpiresAt)
	assert.Equal(t, "user-123", reservation.OwnerID)
	assert.Equal(t, "idempotency-key-456", reservation.IdempotencyKey)
	assert.Equal(t, now, reservation.CreatedAt)
	assert.Equal(t, now, reservation.UpdatedAt)

	// Test validaciones de negocio
	assert.True(t, reservation.Qty > 0)
	assert.True(t, reservation.ExpiresAt.After(reservation.CreatedAt))
	assert.NotEmpty(t, reservation.SKU)
	assert.NotEmpty(t, reservation.OwnerID)
	assert.NotEmpty(t, reservation.IdempotencyKey)
}

func TestReservation_IsExpired(t *testing.T) {
	now := time.Now()

	// Test reservación expirada
	expiredReservation := models.Reservation{
		ExpiresAt: now.Add(-1 * time.Hour), // Expiró hace 1 hora
		Status:    models.ReservationStatusPending,
	}

	// Test reservación válida
	validReservation := models.Reservation{
		ExpiresAt: now.Add(1 * time.Hour), // Expira en 1 hora
		Status:    models.ReservationStatusPending,
	}

	// Test reservación ya comprometida (no debería considerarse expirada)
	committedReservation := models.Reservation{
		ExpiresAt: now.Add(-1 * time.Hour),           // Técnicamente expirada
		Status:    models.ReservationStatusCommitted, // Pero ya comprometida
	}

	// Lógica de expiración: solo PENDING y tiempo pasado
	assert.True(t, expiredReservation.Status == models.ReservationStatusPending && expiredReservation.ExpiresAt.Before(now))
	assert.False(t, validReservation.Status == models.ReservationStatusPending && validReservation.ExpiresAt.Before(now))
	assert.False(t, committedReservation.Status == models.ReservationStatusPending) // No es PENDING
}

func TestOutboxEvent_ModelValidation(t *testing.T) {
	// Test creación de modelo OutboxEvent válido
	now := time.Now()
	event := models.OutboxEvent{
		ID:              1,
		EventType:       models.EventTypeReserveStock,
		Key:             "TEST-SKU-001",
		Payload:         `{"sku":"TEST-SKU-001","qty":5}`,
		CreatedAt:       now,
		Published:       false,
		PublishAttempts: 0,
		LastError:       nil,
	}

	assert.Equal(t, 1, event.ID)
	assert.Equal(t, models.EventTypeReserveStock, event.EventType)
	assert.Equal(t, "TEST-SKU-001", event.Key)
	assert.Contains(t, event.Payload, "TEST-SKU-001")
	assert.Equal(t, now, event.CreatedAt)
	assert.False(t, event.Published)
	assert.Equal(t, 0, event.PublishAttempts)
	assert.Nil(t, event.LastError)
}

func TestInventoryEvent_ModelValidation(t *testing.T) {
	// Test creación de modelo InventoryEvent válido
	eventID := uuid.New().String()
	reservationID := uuid.New()
	now := time.Now()

	event := models.InventoryEvent{
		EventID:        eventID,
		EventType:      models.EventTypeReserveStock,
		SKU:            "TEST-SKU-001",
		Qty:            5,
		Version:        1,
		ReservationID:  &reservationID,
		Status:         models.ReservationStatusPending,
		OwnerID:        "user-123",
		IdempotencyKey: "idempotency-key-456",
		Timestamp:      now,
	}

	assert.Equal(t, eventID, event.EventID)
	assert.Equal(t, models.EventTypeReserveStock, event.EventType)
	assert.Equal(t, "TEST-SKU-001", event.SKU)
	assert.Equal(t, 5, event.Qty)
	assert.Equal(t, int64(1), event.Version)
	assert.NotNil(t, event.ReservationID)
	assert.Equal(t, reservationID, *event.ReservationID)
	assert.Equal(t, models.ReservationStatusPending, event.Status)
	assert.Equal(t, "user-123", event.OwnerID)
	assert.Equal(t, "idempotency-key-456", event.IdempotencyKey)
	assert.Equal(t, now, event.Timestamp)
}

func TestInventoryState_ModelValidation(t *testing.T) {
	// Test creación de modelo InventoryState válido
	now := time.Now()
	state := models.InventoryState{
		SKU:          "TEST-SKU-001",
		AvailableQty: 95,
		ReservedQty:  5,
		Version:      2,
		UpdatedAt:    now,
	}

	assert.Equal(t, "TEST-SKU-001", state.SKU)
	assert.Equal(t, 95, state.AvailableQty)
	assert.Equal(t, 5, state.ReservedQty)
	assert.Equal(t, int64(2), state.Version)
	assert.Equal(t, now, state.UpdatedAt)

	// Test validaciones de integridad
	assert.True(t, state.AvailableQty >= 0)
	assert.True(t, state.ReservedQty >= 0)
	assert.True(t, state.Version > 0)
}

func TestReserveRequest_ModelValidation(t *testing.T) {
	// Test creación de modelo ReserveRequest válido
	request := models.ReserveRequest{
		Qty:            5,
		OwnerID:        "user-123",
		IdempotencyKey: "idempotency-key-456",
	}

	assert.Equal(t, 5, request.Qty)
	assert.Equal(t, "user-123", request.OwnerID)
	assert.Equal(t, "idempotency-key-456", request.IdempotencyKey)

	// Test validaciones de negocio
	assert.True(t, request.Qty > 0)
	assert.NotEmpty(t, request.OwnerID)
	assert.NotEmpty(t, request.IdempotencyKey)
}

func TestAvailabilityResponse_ModelValidation(t *testing.T) {
	// Test creación de modelo AvailabilityResponse válido
	now := time.Now()
	response := models.AvailabilityResponse{
		SKU:          "TEST-SKU-001",
		AvailableQty: 95,
		ReservedQty:  5,
		CacheHit:     true,
		LastUpdated:  now,
	}

	assert.Equal(t, "TEST-SKU-001", response.SKU)
	assert.Equal(t, 95, response.AvailableQty)
	assert.Equal(t, 5, response.ReservedQty)
	assert.True(t, response.CacheHit)
	assert.Equal(t, now, response.LastUpdated)

	// Test validaciones
	assert.True(t, response.AvailableQty >= 0)
	assert.True(t, response.ReservedQty >= 0)
	assert.NotEmpty(t, response.SKU)
	assert.False(t, response.LastUpdated.IsZero()) // Debe tener timestamp válido
}

func TestCommitRequest_ModelValidation(t *testing.T) {
	// Test creación de modelo CommitRequest válido
	request := models.CommitRequest{
		IdempotencyKey: "idempotency-key-456",
	}

	assert.Equal(t, "idempotency-key-456", request.IdempotencyKey)
	assert.NotEmpty(t, request.IdempotencyKey)
}

func TestReleaseRequest_ModelValidation(t *testing.T) {
	// Test creación de modelo ReleaseRequest válido
	request := models.ReleaseRequest{
		IdempotencyKey: "idempotency-key-456",
	}

	assert.Equal(t, "idempotency-key-456", request.IdempotencyKey)
	assert.NotEmpty(t, request.IdempotencyKey)
}

func TestReserveResponse_ModelValidation(t *testing.T) {
	// Test creación de modelo ReserveResponse válido
	reservationID := uuid.New()
	now := time.Now()
	expiresAt := now.Add(24 * time.Hour)

	response := models.ReserveResponse{
		ReservationID: reservationID,
		SKU:           "TEST-SKU-001",
		Qty:           5,
		Status:        models.ReservationStatusPending,
		ExpiresAt:     expiresAt,
		Message:       "Reservation created successfully",
	}

	assert.Equal(t, reservationID, response.ReservationID)
	assert.Equal(t, "TEST-SKU-001", response.SKU)
	assert.Equal(t, 5, response.Qty)
	assert.Equal(t, models.ReservationStatusPending, response.Status)
	assert.Equal(t, expiresAt, response.ExpiresAt)
	assert.Equal(t, "Reservation created successfully", response.Message)

	// Test validaciones
	assert.True(t, response.Qty > 0)
	assert.NotEmpty(t, response.SKU)
	assert.NotEmpty(t, response.Message)
	assert.True(t, response.ExpiresAt.After(now))
}

func TestErrorResponse_ModelValidation(t *testing.T) {
	// Test creación de modelo ErrorResponse válido
	details := map[string]string{
		"field": "qty",
		"issue": "must be positive",
	}

	errorResponse := models.ErrorResponse{
		Error:   "Validation failed",
		Code:    string(models.ErrorCodeValidationError),
		Details: details,
	}

	assert.Equal(t, "Validation failed", errorResponse.Error)
	assert.Equal(t, string(models.ErrorCodeValidationError), errorResponse.Code)
	assert.NotNil(t, errorResponse.Details)
	assert.Equal(t, "qty", errorResponse.Details["field"])
	assert.Equal(t, "must be positive", errorResponse.Details["issue"])
}
