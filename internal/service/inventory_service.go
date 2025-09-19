package service

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"inventory-service/internal/interfaces"
	"inventory-service/internal/models"
	"inventory-service/internal/repository"
)

// InventoryService handles business logic for inventory operations
type InventoryService struct {
	repo       interfaces.InventoryRepository
	publisher  interfaces.MessagePublisher
	cache      interfaces.CacheRepository
	config     ServiceConfig
	outboxRepo *repository.OutboxRepository // Direct access to outbox operations
}

// ServiceConfig holds service configuration
type ServiceConfig struct {
	ReservationTTL           time.Duration
	MaxReasonableReservation int           // Maximum allowed reservation quantity
	CacheTimeout             time.Duration // Timeout for cache operations during pre-check
	StockSafetyBuffer        float64       // Safety buffer percentage (0.1 = 10%)
	EnableStockPreCheck      bool          // Enable/disable stock pre-check feature
}

// Validate validates the service configuration
func (c ServiceConfig) Validate() error {
	if c.ReservationTTL < time.Minute {
		return fmt.Errorf("reservation TTL must be at least 1 minute, got %v", c.ReservationTTL)
	}
	if c.MaxReasonableReservation < 1 {
		return fmt.Errorf("max reasonable reservation must be positive, got %d", c.MaxReasonableReservation)
	}
	if c.CacheTimeout < time.Millisecond {
		return fmt.Errorf("cache timeout must be at least 1ms, got %v", c.CacheTimeout)
	}
	if c.StockSafetyBuffer < 0 || c.StockSafetyBuffer > 1 {
		return fmt.Errorf("stock safety buffer must be between 0 and 1, got %f", c.StockSafetyBuffer)
	}
	return nil
}

// NewInventoryService creates a new inventory service with dependency injection and validation
func NewInventoryService(
	repo interfaces.InventoryRepository,
	outboxRepo *repository.OutboxRepository,
	publisher interfaces.MessagePublisher,
	cache interfaces.CacheRepository,
	config ServiceConfig,
) (*InventoryService, error) {
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid service configuration: %w", err)
	}

	return &InventoryService{
		repo:       repo,
		outboxRepo: outboxRepo,
		publisher:  publisher,
		cache:      cache,
		config:     config,
	}, nil
}

// ReserveStock creates a PENDING reservation and publishes an event with smart stock pre-check
func (s *InventoryService) ReserveStock(ctx context.Context, sku string, req *models.ReserveRequest) (*models.ReserveResponse, error) {
	// 1. Validate inputs
	if err := s.validateReserveRequest(sku, req); err != nil {
		return nil, err
	}

	// 2. Check available stock
	if err := s.checkAvailableStock(ctx, sku, req); err != nil {
		return nil, err
	}

	// 3. Check idempotency
	existingReservation, err := s.reservationAlreadyExists(ctx, req.IdempotencyKey)
	if err != nil {
		return nil, fmt.Errorf("idempotency check failed: %w", err)
	}
	if existingReservation != nil {
		return s.buildReserveResponse(existingReservation, "Reservation already exists"), nil
	}

	// 4. Create reservation in transaction
	reservation, err := s.createReservationTransaction(ctx, sku, req)
	if err != nil {
		return nil, err
	}

	// 5. Invalidate Cache
	s.invalidateCacheBySku(sku)

	return s.buildReserveResponse(reservation, "Reservation created successfully"), nil
}

// validateReserveRequest validates the reservation request
func (s *InventoryService) validateReserveRequest(sku string, req *models.ReserveRequest) error {
	if sku == "" {
		return fmt.Errorf("SKU is required")
	}
	if req.Qty <= 0 {
		return fmt.Errorf("quantity must be positive, got %d", req.Qty)
	}
	if req.OwnerID == "" {
		return fmt.Errorf("owner ID is required")
	}
	if req.IdempotencyKey == "" {
		return fmt.Errorf("idempotency key is required")
	}
	if s.config.EnableStockPreCheck && req.Qty > s.config.MaxReasonableReservation {
		return fmt.Errorf("reservation quantity %d exceeds maximum allowed %d", req.Qty, s.config.MaxReasonableReservation)
	}
	return nil
}

func (s *InventoryService) checkAvailableStock(ctx context.Context, sku string, req *models.ReserveRequest) error {
	// Stock pre-check
	if s.config.EnableStockPreCheck {
		return s.checkStockAvailability(ctx, sku, req.Qty)
	}
	return nil
}

// checkStockAvailability checks if there's sufficient stock available
func (s *InventoryService) checkStockAvailability(ctx context.Context, sku string, qty int) error {
	available, err := s.cache.GetAvailableStock(ctx, sku)
	if err != nil {
		// Cache miss or timeout - fallback to database check for accurate validation
		log.Debug().
			Err(err).
			Str("sku", sku).
			Msg("Cache unavailable for stock pre-check, falling back to database")

		// Fallback: get available stock from database
		inventory, dbErr := s.repo.GetInventory(ctx, sku)
		if dbErr != nil {
			return fmt.Errorf("failed to get inventory from database: %w", dbErr)
		}
		if inventory == nil {
			return fmt.Errorf("no inventory found for SKU %s", sku)
		}
		available = inventory.AvailableQty
	}

	// Apply safety buffer for concurrent reservations
	safeBuffer := int(float64(available) * s.config.StockSafetyBuffer)
	effectiveAvailable := available - safeBuffer

	if qty > effectiveAvailable {
		return fmt.Errorf("insufficient stock: requested %d, effectively available %d (total: %d, safety buffer: %d)", qty, effectiveAvailable, available, safeBuffer)
	}

	log.Debug().
		Str("sku", sku).
		Int("requested", qty).
		Int("available", available).
		Int("buffer", safeBuffer).
		Msg("Stock pre-check passed")

	return nil
}

// checkIdempotency checks for existing reservations with the same idempotency key
func (s *InventoryService) reservationAlreadyExists(ctx context.Context, idempotencyKey string) (*models.Reservation, error) {
	existingReservation, err := s.repo.GetReservationByIdempotencyKey(ctx, idempotencyKey)
	if err != nil {
		return nil, fmt.Errorf("failed to check existing reservation: %w", err)
	}
	return existingReservation, nil
}

// createReservationTransaction creates a reservation within a database transaction
func (s *InventoryService) createReservationTransaction(ctx context.Context, sku string, req *models.ReserveRequest) (*models.Reservation, error) {
	// Start database transaction
	tx, err := s.repo.BeginTx(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Create reservation
	reservationID := uuid.New()
	expiresAt := time.Now().Add(s.config.ReservationTTL)

	reservation := &models.Reservation{
		ReservationID:  reservationID,
		SKU:            sku,
		Qty:            req.Qty,
		Status:         models.ReservationStatusPending,
		ExpiresAt:      expiresAt,
		OwnerID:        req.OwnerID,
		IdempotencyKey: req.IdempotencyKey,
	}

	if err := s.repo.CreateReservation(ctx, tx, reservation); err != nil {
		return nil, fmt.Errorf("failed to create reservation: %w", err)
	}

	// Create inventory event
	event := &models.InventoryEvent{
		EventID:        uuid.New().String(),
		EventType:      models.EventTypeReserveStock,
		SKU:            sku,
		Qty:            req.Qty,
		Version:        0, // Will be set by processor based on current inventory version
		ReservationID:  &reservationID,
		Status:         models.ReservationStatusPending,
		OwnerID:        req.OwnerID,
		IdempotencyKey: req.IdempotencyKey,
		Timestamp:      time.Now(),
	}

	// Store event in outbox for reliable delivery
	if err := s.repo.CreateOutboxEvent(ctx, tx, models.EventTypeReserveStock, sku, event); err != nil {
		return nil, fmt.Errorf("failed to create outbox event: %w", err)
	}

	// Commit transaction
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("failed to commit transaction: %w", err)
	}

	return reservation, nil
}

// performAsyncOperations performs cache invalidation and event publishing asynchronously
func (s *InventoryService) invalidateCacheBySku(sku string) {
	// Cache invalidation
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		if err := s.cache.DeleteInventory(ctx, sku); err != nil {
			log.Error().Err(err).Str("sku", sku).Msg("Failed to invalidate cache after reservation")
		} else {
			log.Debug().Str("sku", sku).Msg("Cache invalidated after reservation")
		}
	}()
}

// buildReserveResponse builds a response for reservation operations
func (s *InventoryService) buildReserveResponse(reservation *models.Reservation, message string) *models.ReserveResponse {
	return &models.ReserveResponse{
		ReservationID: reservation.ReservationID,
		SKU:           reservation.SKU,
		Qty:           reservation.Qty,
		Status:        reservation.Status,
		ExpiresAt:     reservation.ExpiresAt,
		Message:       message,
	}
}

// CommitReservation commits a PENDING reservation
func (s *InventoryService) CommitReservation(ctx context.Context, reservationID uuid.UUID, req *models.CommitRequest) error {
	// Get reservation
	reservation, err := s.repo.GetReservation(ctx, reservationID)
	if err != nil {
		return fmt.Errorf("failed to get reservation: %w", err)
	}

	if reservation == nil {
		return fmt.Errorf("reservation not found")
	}

	if reservation.Status != models.ReservationStatusPending {
		return fmt.Errorf("reservation is not in PENDING status")
	}

	if time.Now().After(reservation.ExpiresAt) {
		return fmt.Errorf("reservation has expired")
	}

	// Start transaction
	tx, err := s.repo.BeginTx(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Update reservation status
	if err := s.repo.UpdateReservationStatus(ctx, tx, reservationID, models.ReservationStatusCommitted); err != nil {
		return fmt.Errorf("failed to update reservation status: %w", err)
	}

	// Create commit event
	event := &models.InventoryEvent{
		EventID:        uuid.New().String(),
		EventType:      models.EventTypeCommitReserve,
		SKU:            reservation.SKU,
		Qty:            reservation.Qty,
		Version:        0, // Will be set by processor based on current inventory version
		ReservationID:  &reservationID,
		Status:         models.ReservationStatusCommitted,
		OwnerID:        reservation.OwnerID,
		IdempotencyKey: req.IdempotencyKey,
		Timestamp:      time.Now(),
	}

	// Store event in outbox
	if err := s.repo.CreateOutboxEvent(ctx, tx, models.EventTypeCommitReserve, reservation.SKU, event); err != nil {
		return fmt.Errorf("failed to create outbox event: %w", err)
	}

	// Commit transaction
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	// Invalidate cache immediately to ensure fresh reads
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		if err := s.cache.DeleteInventory(ctx, reservation.SKU); err != nil {
			log.Error().Err(err).Str("sku", reservation.SKU).Msg("Failed to invalidate cache after commit")
		} else {
			log.Debug().Str("sku", reservation.SKU).Msg("Cache invalidated after commit")
		}
	}()

	return nil
}

// ReleaseReservation releases a PENDING reservation
func (s *InventoryService) ReleaseReservation(ctx context.Context, reservationID uuid.UUID, req *models.ReleaseRequest) error {
	// Get reservation
	reservation, err := s.repo.GetReservation(ctx, reservationID)
	if err != nil {
		return fmt.Errorf("failed to get reservation: %w", err)
	}

	if reservation == nil {
		return fmt.Errorf("reservation not found")
	}

	if reservation.Status != models.ReservationStatusPending {
		return fmt.Errorf("reservation is not in PENDING status")
	}

	// Start transaction
	tx, err := s.repo.BeginTx(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Update reservation status
	if err := s.repo.UpdateReservationStatus(ctx, tx, reservationID, models.ReservationStatusReleased); err != nil {
		return fmt.Errorf("failed to update reservation status: %w", err)
	}

	// Create release event
	event := &models.InventoryEvent{
		EventID:        uuid.New().String(),
		EventType:      models.EventTypeReleaseStock,
		SKU:            reservation.SKU,
		Qty:            reservation.Qty,
		Version:        0, // Will be set by processor based on current inventory version
		ReservationID:  &reservationID,
		Status:         models.ReservationStatusReleased,
		OwnerID:        reservation.OwnerID,
		IdempotencyKey: req.IdempotencyKey,
		Timestamp:      time.Now(),
	}

	// Store event in outbox
	if err := s.repo.CreateOutboxEvent(ctx, tx, models.EventTypeReleaseStock, reservation.SKU, event); err != nil {
		return fmt.Errorf("failed to create outbox event: %w", err)
	}

	// Commit transaction
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	// Invalidate cache immediately to ensure fresh reads
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		if err := s.cache.DeleteInventory(ctx, reservation.SKU); err != nil {
			log.Error().Err(err).Str("sku", reservation.SKU).Msg("Failed to invalidate cache after release")
		} else {
			log.Debug().Str("sku", reservation.SKU).Msg("Cache invalidated after release")
		}
	}()

	return nil
}

// GetAvailability returns inventory availability, checking cache first
func (s *InventoryService) GetAvailability(ctx context.Context, sku string) (*models.AvailabilityResponse, error) {
	// Try cache first
	inventory, err := s.cache.GetInventory(ctx, sku)
	if err != nil {
		log.Error().Err(err).Str("sku", sku).Msg("Cache error, falling back to database")
	}

	if inventory != nil {
		return &models.AvailabilityResponse{
			SKU:          inventory.SKU,
			AvailableQty: inventory.AvailableQty,
			ReservedQty:  inventory.ReservedQty,
			CacheHit:     true,
			LastUpdated:  inventory.UpdatedAt,
		}, nil
	}

	// Cache miss, read from database
	inventory, err = s.repo.GetInventory(ctx, sku)
	if err != nil {
		return nil, fmt.Errorf("failed to get inventory from database: %w", err)
	}

	if inventory == nil {
		return &models.AvailabilityResponse{
			SKU:          sku,
			AvailableQty: 0,
			ReservedQty:  0,
			CacheHit:     false,
			LastUpdated:  time.Now(),
		}, nil
	}

	// Update cache asynchronously
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		if err := s.cache.SetInventory(ctx, inventory); err != nil {
			log.Error().Err(err).Str("sku", sku).Msg("Failed to update cache")
		}
	}()

	return &models.AvailabilityResponse{
		SKU:          inventory.SKU,
		AvailableQty: inventory.AvailableQty,
		ReservedQty:  inventory.ReservedQty,
		CacheHit:     false,
		LastUpdated:  inventory.UpdatedAt,
	}, nil
}

// ProcessExpiredReservations finds and processes expired reservations
func (s *InventoryService) ProcessExpiredReservations(ctx context.Context) error {
	expiredReservations, err := s.repo.GetExpiredReservations(ctx)
	if err != nil {
		return fmt.Errorf("failed to get expired reservations: %w", err)
	}

	for i := range expiredReservations {
		if err := s.expireReservation(ctx, &expiredReservations[i]); err != nil {
			log.Error().Err(err).
				Str("reservation_id", expiredReservations[i].ReservationID.String()).
				Msg("Failed to expire reservation")
		}
	}

	return nil
}

// expireReservation expires a single reservation
func (s *InventoryService) expireReservation(ctx context.Context, reservation *models.Reservation) error {
	tx, err := s.repo.BeginTx(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Update reservation status
	if err := s.repo.UpdateReservationStatus(ctx, tx, reservation.ReservationID, models.ReservationStatusExpired); err != nil {
		return fmt.Errorf("failed to update reservation status: %w", err)
	}

	// Create expire event
	event := &models.InventoryEvent{
		EventID:        uuid.New().String(),
		EventType:      models.EventTypeExpireReserve,
		SKU:            reservation.SKU,
		Qty:            reservation.Qty,
		Version:        0, // Will be set by processor based on current inventory version
		ReservationID:  &reservation.ReservationID,
		Status:         models.ReservationStatusExpired,
		OwnerID:        reservation.OwnerID,
		IdempotencyKey: reservation.IdempotencyKey,
		Timestamp:      time.Now(),
	}

	// Store event in outbox
	if err := s.repo.CreateOutboxEvent(ctx, tx, models.EventTypeExpireReserve, reservation.SKU, event); err != nil {
		return fmt.Errorf("failed to create outbox event: %w", err)
	}

	// Commit transaction
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	log.Info().
		Str("reservation_id", reservation.ReservationID.String()).
		Str("sku", reservation.SKU).
		Msg("Expired reservation")

	return nil
}
