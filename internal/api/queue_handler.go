package api

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"inventory-service/internal/models"
	"inventory-service/internal/service"
)

// QueueHandler handles HTTP requests for write operations (Queue Service)
type QueueHandler struct {
	inventoryService *service.InventoryService
}

// NewQueueHandler creates a new Queue API handler
func NewQueueHandler(inventoryService *service.InventoryService) *QueueHandler {
	return &QueueHandler{
		inventoryService: inventoryService,
	}
}

// SetupQueueRoutes sets up the HTTP routes for Queue Service
func (h *QueueHandler) SetupQueueRoutes() *gin.Engine {
	// Set Gin to release mode for production
	gin.SetMode(gin.ReleaseMode)

	r := gin.New()

	// Middleware
	r.Use(gin.Logger())
	r.Use(gin.Recovery())
	r.Use(RequestIDMiddleware())
	r.Use(ErrorHandlerMiddleware())
	r.Use(h.corsMiddleware())

	// Health check
	r.GET("/health", h.healthCheck)

	// Write operations for Queue Service
	api := r.Group("/api/v1")
	{
		// Inventory write operations
		api.POST("/inventory/:sku/reserve", h.reserveStock)
		api.POST("/reservations/:id/commit", h.commitReservation)
		api.POST("/reservations/:id/release", h.releaseReservation)
	}

	return r
}

// reserveStock handles stock reservation requests
func (h *QueueHandler) reserveStock(c *gin.Context) {
	sku := c.Param("sku")
	if sku == "" {
		Response.ValidationError(c, "sku", "SKU is required")
		return
	}

	var req models.CreateReservationRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		log.Error().Err(err).Msg("Failed to bind reserve request")
		Response.ValidationError(c, "request", "Invalid request format")
		return
	}

	// Validate request fields
	if req.Qty <= 0 {
		Response.ValidationError(c, "qty", "Quantity must be positive")
		return
	}

	if req.OwnerID == "" {
		Response.ValidationError(c, "owner_id", "Owner ID is required")
		return
	}

	if req.IdempotencyKey == "" {
		Response.ValidationError(c, "idempotency_key", "Idempotency key is required")
		return
	}

	// Convert to internal model
	internalReq := &models.ReserveRequest{
		Qty:            req.Qty,
		OwnerID:        req.OwnerID,
		IdempotencyKey: req.IdempotencyKey,
	}

	// Call service
	response, err := h.inventoryService.ReserveStock(c.Request.Context(), sku, internalReq)
	if err != nil {
		log.Error().Err(err).Str("sku", sku).Msg("Failed to reserve stock")

		errorMsg := err.Error()
		switch {
		case strings.Contains(errorMsg, "quantity must be positive"):
			Response.ValidationError(c, "quantity", "Quantity must be positive")
		case strings.Contains(errorMsg, "exceeds maximum allowed"):
			Response.ValidationError(c, "quantity", err.Error())
		case strings.Contains(errorMsg, "insufficient stock"):
			Response.BusinessError(c, 409, "Insufficient Stock", err.Error(), models.ErrorCodeInsufficientStock)
		case strings.Contains(errorMsg, "already exists"):
			Response.Conflict(c, "Reservation Exists", "A reservation with this idempotency key already exists")
		default:
			Response.InternalError(c, errorMsg)
		}
		return
	}
	Response.Created(c, response)
}

// commitReservation handles reservation commit requests
func (h *QueueHandler) commitReservation(c *gin.Context) {
	reservationIDStr := c.Param("id")
	if reservationIDStr == "" {
		Response.ValidationError(c, "id", "Reservation ID is required")
		return
	}

	// Parse UUID
	reservationID, err := uuid.Parse(reservationIDStr)
	if err != nil {
		Response.ValidationError(c, "id", "Invalid reservation ID format")
		return
	}

	var req models.CommitReservationRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		log.Error().Err(err).Msg("Failed to bind commit request")
		Response.ValidationError(c, "request", "Invalid request format")
		return
	}

	if req.IdempotencyKey == "" {
		Response.ValidationError(c, "idempotency_key", "Idempotency key is required")
		return
	}

	// Convert to internal model
	internalReq := &models.CommitRequest{
		IdempotencyKey: req.IdempotencyKey,
	}

	// Call service
	err = h.inventoryService.CommitReservation(c.Request.Context(), reservationID, internalReq)
	if err != nil {
		log.Error().Err(err).Str("reservation_id", reservationIDStr).Msg("Failed to commit reservation")

		errorMsg := err.Error()
		switch {
		case strings.Contains(errorMsg, "not found"):
			Response.NotFound(c, "Reservation")
		case strings.Contains(errorMsg, "expired"):
			Response.UnprocessableEntity(c, "Reservation Expired", "The reservation has expired and cannot be committed")
		case strings.Contains(errorMsg, "not in PENDING status"):
			Response.UnprocessableEntity(c, "Invalid Status", "Reservation is not in pending status")
		default:
			Response.InternalError(c, errorMsg)
		}
		return
	}

	Response.NoContent(c)
}

// releaseReservation handles reservation release requests
func (h *QueueHandler) releaseReservation(c *gin.Context) {
	reservationIDStr := c.Param("id")
	if reservationIDStr == "" {
		Response.ValidationError(c, "id", "Reservation ID is required")
		return
	}

	// Parse UUID
	reservationID, err := uuid.Parse(reservationIDStr)
	if err != nil {
		Response.ValidationError(c, "id", "Invalid reservation ID format")
		return
	}

	var req models.ReleaseReservationRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		log.Error().Err(err).Msg("Failed to bind release request")
		Response.ValidationError(c, "request", "Invalid request format")
		return
	}

	if req.IdempotencyKey == "" {
		Response.ValidationError(c, "idempotency_key", "Idempotency key is required")
		return
	}

	// Convert to internal model
	internalReq := &models.ReleaseRequest{
		IdempotencyKey: req.IdempotencyKey,
	}

	// Call service
	err = h.inventoryService.ReleaseReservation(c.Request.Context(), reservationID, internalReq)
	if err != nil {
		log.Error().Err(err).Str("reservation_id", reservationIDStr).Msg("Failed to release reservation")

		errorMsg := err.Error()
		switch {
		case strings.Contains(errorMsg, "not found"):
			Response.NotFound(c, "Reservation")
		case strings.Contains(errorMsg, "not in PENDING status"):
			Response.UnprocessableEntity(c, "Invalid Status", "Reservation is not in pending status")
		default:
			Response.InternalError(c, errorMsg)
		}
		return
	}

	Response.NoContent(c)
}

// healthCheck handles health check requests
func (h *QueueHandler) healthCheck(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"status":  "healthy",
		"service": "inventory-queue-service",
	})
}

// corsMiddleware handles CORS headers
func (h *QueueHandler) corsMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Access-Control-Allow-Origin", "*")
		c.Header("Access-Control-Allow-Methods", "POST, GET, OPTIONS, PUT, DELETE")
		c.Header("Access-Control-Allow-Headers", "Accept, Content-Type, Content-Length, Accept-Encoding, X-CSRF-Token, Authorization")

		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(204)
			return
		}

		c.Next()
	}
}
