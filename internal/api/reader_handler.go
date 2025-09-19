package api

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog/log"

	"inventory-service/internal/models"
)

// ReaderHandler handles HTTP requests for read operations (Reader Service)
type ReaderHandler struct {
	readerService ReaderServiceInterface
}

// ReaderServiceInterface defines the interface for reader service operations
type ReaderServiceInterface interface {
	GetAvailability(ctx context.Context, sku string) (*models.AvailabilityResponse, error)
}

// NewReaderHandler creates a new Reader API handler
func NewReaderHandler(readerService ReaderServiceInterface) *ReaderHandler {
	return &ReaderHandler{
		readerService: readerService,
	}
}

// SetupReaderRoutes sets up the HTTP routes for Reader Service
func (h *ReaderHandler) SetupReaderRoutes() *gin.Engine {
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

	// Read operations for Reader Service
	api := r.Group("/api/v1")
	{
		// Inventory read operations
		api.GET("/inventory/:sku/availability", h.getAvailability)
	}

	return r
}

// getAvailability handles inventory availability requests
func (h *ReaderHandler) getAvailability(c *gin.Context) {
	sku := c.Param("sku")
	if sku == "" {
		Response.ValidationError(c, "sku", "SKU is required")
		return
	}

	// Call reader service (returns original models.AvailabilityResponse from models.go)
	availability, err := h.readerService.GetAvailability(c.Request.Context(), sku)
	if err != nil {
		log.Error().Err(err).Str("sku", sku).Msg("Failed to get availability")

		// Handle specific error types
		errorMsg := err.Error()
		if strings.Contains(errorMsg, "not found") {
			Response.NotFound(c, "Inventory for SKU "+sku)
			return
		}

		Response.InternalError(c, errorMsg)
		return
	}

	// Create enhanced availability response - return resource directly (REST-native)
	apiResponse := map[string]interface{}{
		"sku":                availability.SKU,
		"available_quantity": availability.AvailableQty,
		"reserved_quantity":  availability.ReservedQty,
		"total_quantity":     availability.AvailableQty + availability.ReservedQty,
		"last_updated":       time.Now(),
		"cache_hit":          availability.CacheHit,
	}

	// Return 200 OK with the availability resource directly
	Response.Success(c, apiResponse)
}

// healthCheck handles health check requests
func (h *ReaderHandler) healthCheck(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"status":  "healthy",
		"service": "inventory-reader-service",
	})
}

// corsMiddleware handles CORS headers
func (h *ReaderHandler) corsMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Access-Control-Allow-Origin", "*")
		c.Header("Access-Control-Allow-Methods", "GET, OPTIONS")
		c.Header("Access-Control-Allow-Headers", "Accept, Content-Type, Content-Length, Accept-Encoding, Authorization")

		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(204)
			return
		}

		c.Next()
	}
}
