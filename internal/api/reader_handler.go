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

		// Batch read operations
		api.GET("/inventory/batch/availability", h.getBatchAvailability)
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

// getBatchAvailability handles batch availability requests
func (h *ReaderHandler) getBatchAvailability(c *gin.Context) {
	// Get SKUs from query parameter (comma-separated)
	skusParam := c.Query("skus")
	if skusParam == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "skus query parameter is required"})
		return
	}

	// Split the comma-separated SKUs
	skus := strings.Split(skusParam, ",")

	// Trim whitespace from each SKU
	for i, sku := range skus {
		skus[i] = strings.TrimSpace(sku)
	}

	if len(skus) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "At least one SKU is required"})
		return
	}

	if len(skus) > 100 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Maximum 100 SKUs allowed per batch request"})
		return
	}

	// Process each SKU
	results := make([]interface{}, 0, len(skus))

	for _, sku := range skus {
		if sku == "" {
			continue // Skip empty SKUs
		}

		availability, err := h.readerService.GetAvailability(c.Request.Context(), sku)
		if err != nil {
			// Add error result for this SKU
			results = append(results, gin.H{
				"sku":   sku,
				"error": "Failed to retrieve availability",
			})
			continue
		}

		results = append(results, availability)
	}

	response := gin.H{
		"total_requested": len(skus),
		"results":         results,
	}

	c.JSON(http.StatusOK, response)
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
