package api

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// ProcessorHandler handles HTTP requests for processor service operations
// Mainly used for health checks and internal monitoring
type ProcessorHandler struct {
}

// NewProcessorHandler creates a new Processor API handler
func NewProcessorHandler() *ProcessorHandler {
	return &ProcessorHandler{}
}

// SetupProcessorRoutes sets up the HTTP routes for Processor Service
func (h *ProcessorHandler) SetupProcessorRoutes() *gin.Engine {
	// Set Gin to release mode for production
	gin.SetMode(gin.ReleaseMode)

	r := gin.New()

	// Middleware
	r.Use(gin.Logger())
	r.Use(gin.Recovery())
	r.Use(h.corsMiddleware())

	// Health check and monitoring endpoints only
	r.GET("/health", h.healthCheck)

	return r
}

// healthCheck handles health check requests
func (h *ProcessorHandler) healthCheck(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"status":  "healthy",
		"service": "inventory-processor-service",
	})
}

// corsMiddleware handles CORS headers
func (h *ProcessorHandler) corsMiddleware() gin.HandlerFunc {
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
