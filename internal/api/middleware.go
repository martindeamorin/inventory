package api

import (
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/go-playground/validator/v10"
	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"inventory-service/internal/models"
)

// RequestIDMiddleware adds a unique request ID to each request
func RequestIDMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		requestID := c.GetHeader("X-Request-ID")
		if requestID == "" {
			requestID = uuid.New().String()
		}

		// Set request ID in response headers (REST-native way)
		c.Header("X-Request-ID", requestID)
		c.Set("request_id", requestID)
		c.Next()
	}
}

func ErrorHandlerMiddleware() gin.HandlerFunc {
	return gin.HandlerFunc(func(c *gin.Context) {
		c.Next()

		// Handle any errors that occurred during request processing
		if len(c.Errors) > 0 {
			err := c.Errors.Last()

			requestID := getRequestID(c)

			// Set request ID in error response for tracing
			if requestID != "" {
				c.Header("X-Request-ID", requestID)
			}

			// Determine error type and respond appropriately
			switch err.Type {
			case gin.ErrorTypeBind:
				handleValidationError(c, err.Err)
			case gin.ErrorTypePublic:
				handleBusinessError(c, err.Err)
			default:
				handleInternalError(c, err.Err)
			}
		}
	})
}

// ResponseHelpers provides methods for REST-native responses
type ResponseHelpers struct{}

// Success sends the resource directly (no wrapper)
func (h *ResponseHelpers) Success(c *gin.Context, resource interface{}) {
	c.JSON(200, resource)
}

// Created sends a 201 created response with the created resource
func (h *ResponseHelpers) Created(c *gin.Context, resource interface{}) {
	c.JSON(201, resource)
}

// NoContent sends a 204 no content response
func (h *ResponseHelpers) NoContent(c *gin.Context) {
	c.Status(204)
}

func (h *ResponseHelpers) ValidationError(c *gin.Context, field, message string) {
	problem := models.NewValidationProblem(field, message, models.ErrorCodeInvalidField)
	h.setRequestIDHeader(c)
	c.JSON(400, problem)
}

func (h *ResponseHelpers) MultiValidationError(c *gin.Context, violations []models.ValidationError) {
	problem := models.NewMultiValidationProblem(violations)
	h.setRequestIDHeader(c)
	c.JSON(400, problem)
}

// BusinessError sends a business logic error (409 or 422)
func (h *ResponseHelpers) BusinessError(c *gin.Context, status int, title, detail string, code models.ErrorCode) {
	problem := models.NewBusinessLogicProblem(status, title, detail, code)
	h.setRequestIDHeader(c)
	c.JSON(status, problem)
}

// NotFound sends a 404 not found response
func (h *ResponseHelpers) NotFound(c *gin.Context, resource string) {
	problem := models.NewNotFoundProblem(resource)
	h.setRequestIDHeader(c)
	c.JSON(404, problem)
}

// InternalError sends a 500 internal server error response
func (h *ResponseHelpers) InternalError(c *gin.Context, detail string) {
	problem := models.NewProblemDetails(500, "Internal Server Error", "An unexpected error occurred")
	h.setRequestIDHeader(c)

	// Log the error for debugging but don't expose internals
	requestID := getRequestID(c)
	log.Error().
		Str("request_id", requestID).
		Str("detail", detail).
		Msg("Internal server error")

	c.JSON(500, problem)
}

// Conflict sends a 409 conflict response
func (h *ResponseHelpers) Conflict(c *gin.Context, title, detail string) {
	problem := models.NewProblemDetails(409, title, detail)
	h.setRequestIDHeader(c)
	c.JSON(409, problem)
}

// UnprocessableEntity sends a 422 unprocessable entity response
func (h *ResponseHelpers) UnprocessableEntity(c *gin.Context, title, detail string) {
	problem := models.NewProblemDetails(422, title, detail)
	h.setRequestIDHeader(c)
	c.JSON(422, problem)
}

// Helper functions

func (h *ResponseHelpers) setRequestIDHeader(c *gin.Context) {
	if requestID := getRequestID(c); requestID != "" {
		c.Header("X-Request-ID", requestID)
	}
}

func getRequestID(c *gin.Context) string {
	if requestID, exists := c.Get("request_id"); exists {
		return requestID.(string)
	}
	return ""
}

func handleValidationError(c *gin.Context, err error) {
	requestID := getRequestID(c)
	if requestID != "" {
		c.Header("X-Request-ID", requestID)
	}

	if validationErrors, ok := err.(validator.ValidationErrors); ok {
		// Handle multiple validation errors
		violations := make([]models.ValidationError, 0, len(validationErrors))
		for _, validationError := range validationErrors {
			violations = append(violations, models.ValidationError{
				Field:   strings.ToLower(validationError.Field()),
				Message: getValidationMessage(validationError),
				Code:    validationError.Tag(),
			})
		}

		problem := models.NewMultiValidationProblem(violations)
		c.JSON(400, problem)
		return
	}

	// Generic validation error
	problem := models.NewProblemDetails(400, "Bad Request", err.Error())
	c.JSON(400, problem)
}

func handleBusinessError(c *gin.Context, err error) {
	requestID := getRequestID(c)
	if requestID != "" {
		c.Header("X-Request-ID", requestID)
	}

	// Map common business errors to appropriate responses
	errorMsg := err.Error()

	switch {
	case strings.Contains(errorMsg, "insufficient"):
		problem := models.NewBusinessLogicProblem(409, "Insufficient Stock", errorMsg, models.ErrorCodeInsufficientStock)
		c.JSON(409, problem)
	case strings.Contains(errorMsg, "expired"):
		problem := models.NewBusinessLogicProblem(422, "Reservation Expired", errorMsg, models.ErrorCodeReservationExpired)
		c.JSON(422, problem)
	case strings.Contains(errorMsg, "not found"):
		problem := models.NewNotFoundProblem("Resource")
		c.JSON(404, problem)
	default:
		problem := models.NewProblemDetails(500, "Internal Server Error", "An unexpected error occurred")
		c.JSON(500, problem)
	}
}

func handleInternalError(c *gin.Context, err error) {
	requestID := getRequestID(c)
	if requestID != "" {
		c.Header("X-Request-ID", requestID)
	}

	problem := models.NewProblemDetails(500, "Internal Server Error", "An unexpected error occurred")

	log.Error().
		Str("request_id", requestID).
		Err(err).
		Msg("Internal server error")

	c.JSON(500, problem)
}

func getValidationMessage(err validator.FieldError) string {
	switch err.Tag() {
	case "required":
		return "This field is required"
	case "min":
		return "Value is too small"
	case "max":
		return "Value is too large"
	case "email":
		return "Invalid email format"
	default:
		return "Invalid value"
	}
}

// Validation middleware
func ValidateJSON(model interface{}) gin.HandlerFunc {
	return func(c *gin.Context) {
		if err := c.ShouldBindJSON(model); err != nil {
			c.Error(err).SetType(gin.ErrorTypeBind)
			c.Abort()
			return
		}
		c.Next()
	}
}

// Create a global instance for easy access
var Response = &ResponseHelpers{}
