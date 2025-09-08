package test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"

	"inventory-service/internal/models"
)

// Tests simples para validar la estructura de las respuestas de API
func TestAPIResponseModels_ReserveResponse(t *testing.T) {
	reservationID := uuid.New()
	now := time.Now()
	expiresAt := now.Add(24 * time.Hour)

	response := models.ReserveResponse{
		ReservationID: reservationID,
		SKU:           "TEST-SKU",
		Qty:           5,
		Status:        models.ReservationStatusPending,
		ExpiresAt:     expiresAt,
		Message:       "Reservation created successfully",
	}

	// Validar estructura
	assert.Equal(t, reservationID, response.ReservationID)
	assert.Equal(t, "TEST-SKU", response.SKU)
	assert.Equal(t, 5, response.Qty)
	assert.Equal(t, models.ReservationStatusPending, response.Status)
	assert.Equal(t, expiresAt, response.ExpiresAt)
	assert.Equal(t, "Reservation created successfully", response.Message)

	// Validar lógica de negocio
	assert.True(t, response.Qty > 0)
	assert.NotEmpty(t, response.SKU)
	assert.True(t, response.ExpiresAt.After(now))
}

func TestAPIResponseModels_AvailabilityResponse(t *testing.T) {
	now := time.Now()

	response := models.AvailabilityResponse{
		SKU:          "TEST-SKU",
		AvailableQty: 100,
		ReservedQty:  10,
		CacheHit:     true,
		LastUpdated:  now,
	}

	// Validar estructura
	assert.Equal(t, "TEST-SKU", response.SKU)
	assert.Equal(t, 100, response.AvailableQty)
	assert.Equal(t, 10, response.ReservedQty)
	assert.True(t, response.CacheHit)
	assert.Equal(t, now, response.LastUpdated)

	// Validar lógica de negocio
	assert.True(t, response.AvailableQty >= 0)
	assert.True(t, response.ReservedQty >= 0)
	assert.NotEmpty(t, response.SKU)
	assert.False(t, response.LastUpdated.IsZero())
}

func TestAPIRequestModels_ReserveRequest(t *testing.T) {
	request := models.ReserveRequest{
		Qty:            5,
		OwnerID:        "test-owner",
		IdempotencyKey: "test-key-123",
	}

	// Validar estructura
	assert.Equal(t, 5, request.Qty)
	assert.Equal(t, "test-owner", request.OwnerID)
	assert.Equal(t, "test-key-123", request.IdempotencyKey)

	// Validar lógica de negocio
	assert.True(t, request.Qty > 0)
	assert.NotEmpty(t, request.OwnerID)
	assert.NotEmpty(t, request.IdempotencyKey)
}

func TestAPIRequestModels_CommitRequest(t *testing.T) {
	request := models.CommitRequest{
		IdempotencyKey: "commit-key-456",
	}

	// Validar estructura
	assert.Equal(t, "commit-key-456", request.IdempotencyKey)
	assert.NotEmpty(t, request.IdempotencyKey)
}

func TestAPIRequestModels_ReleaseRequest(t *testing.T) {
	request := models.ReleaseRequest{
		IdempotencyKey: "release-key-789",
	}

	// Validar estructura
	assert.Equal(t, "release-key-789", request.IdempotencyKey)
	assert.NotEmpty(t, request.IdempotencyKey)
}

func TestAPIErrorModels_ErrorResponse(t *testing.T) {
	details := map[string]string{
		"field": "qty",
		"issue": "must be positive",
	}

	errorResponse := models.ErrorResponse{
		Error:   "Validation failed",
		Code:    string(models.ErrorCodeValidationError),
		Details: details,
	}

	// Validar estructura
	assert.Equal(t, "Validation failed", errorResponse.Error)
	assert.Equal(t, string(models.ErrorCodeValidationError), errorResponse.Code)
	assert.NotNil(t, errorResponse.Details)
	assert.Equal(t, "qty", errorResponse.Details["field"])
	assert.Equal(t, "must be positive", errorResponse.Details["issue"])
}

func TestAPIModels_InventoryEvent(t *testing.T) {
	eventID := uuid.New().String()
	reservationID := uuid.New()
	now := time.Now()

	event := models.InventoryEvent{
		EventID:        eventID,
		EventType:      models.EventTypeReserveStock,
		SKU:            "TEST-SKU",
		Qty:            5,
		Version:        1,
		ReservationID:  &reservationID,
		Status:         models.ReservationStatusPending,
		OwnerID:        "test-owner",
		IdempotencyKey: "test-key",
		Timestamp:      now,
	}

	// Validar estructura
	assert.Equal(t, eventID, event.EventID)
	assert.Equal(t, models.EventTypeReserveStock, event.EventType)
	assert.Equal(t, "TEST-SKU", event.SKU)
	assert.Equal(t, 5, event.Qty)
	assert.Equal(t, int64(1), event.Version)
	assert.NotNil(t, event.ReservationID)
	assert.Equal(t, reservationID, *event.ReservationID)
	assert.Equal(t, models.ReservationStatusPending, event.Status)
	assert.Equal(t, "test-owner", event.OwnerID)
	assert.Equal(t, "test-key", event.IdempotencyKey)
	assert.Equal(t, now, event.Timestamp)
}

func TestAPIModels_InventoryState(t *testing.T) {
	now := time.Now()

	state := models.InventoryState{
		SKU:          "TEST-SKU",
		AvailableQty: 95,
		ReservedQty:  5,
		Version:      2,
		UpdatedAt:    now,
	}

	// Validar estructura
	assert.Equal(t, "TEST-SKU", state.SKU)
	assert.Equal(t, 95, state.AvailableQty)
	assert.Equal(t, 5, state.ReservedQty)
	assert.Equal(t, int64(2), state.Version)
	assert.Equal(t, now, state.UpdatedAt)

	// Validar lógica de negocio
	assert.True(t, state.AvailableQty >= 0)
	assert.True(t, state.ReservedQty >= 0)
	assert.True(t, state.Version > 0)
	assert.NotEmpty(t, state.SKU)
}

func TestAPIModels_ReservationResponse(t *testing.T) {
	now := time.Now()
	expiresAt := now.Add(24 * time.Hour)

	response := models.ReservationResponse{
		ID:             uuid.New().String(),
		SKU:            "TEST-SKU",
		Quantity:       5,
		Status:         string(models.ReservationStatusPending),
		OwnerID:        "test-owner",
		ExpiresAt:      expiresAt,
		CreatedAt:      now,
		UpdatedAt:      now,
		IdempotencyKey: "test-key",
	}

	// Validar estructura
	assert.NotEmpty(t, response.ID)
	assert.Equal(t, "TEST-SKU", response.SKU)
	assert.Equal(t, 5, response.Quantity)
	assert.Equal(t, string(models.ReservationStatusPending), response.Status)
	assert.Equal(t, "test-owner", response.OwnerID)
	assert.Equal(t, expiresAt, response.ExpiresAt)
	assert.Equal(t, now, response.CreatedAt)
	assert.Equal(t, now, response.UpdatedAt)
	assert.Equal(t, "test-key", response.IdempotencyKey)

	// Validar lógica de negocio
	assert.True(t, response.Quantity > 0)
	assert.NotEmpty(t, response.SKU)
	assert.NotEmpty(t, response.OwnerID)
	assert.True(t, response.ExpiresAt.After(response.CreatedAt))
}
