package models

import (
	"fmt"
	"time"

	"github.com/google/uuid"
)

// ReservationStatus represents the state of a reservation
type ReservationStatus string

const (
	ReservationStatusPending   ReservationStatus = "PENDING"
	ReservationStatusCommitted ReservationStatus = "COMMITTED"
	ReservationStatusReleased  ReservationStatus = "RELEASED"
	ReservationStatusExpired   ReservationStatus = "EXPIRED"
)

// Event types for Kafka messages
const (
	EventTypeReserveStock   = "reserve_stock"
	EventTypeCommitReserve  = "commit_reserve"
	EventTypeReleaseStock   = "release_stock"
	EventTypeReleaseReserve = "release_reserve"
	EventTypeExpireReserve  = "expire_reserve"
	EventTypeInventoryState = "inventory_state"
)

// ErrorCode represents standardized error codes
type ErrorCode string

const (
	// Error codes for API responses
	ErrorCodeInvalidField        ErrorCode = "INVALID_FIELD"
	ErrorCodeMissingField        ErrorCode = "MISSING_FIELD"
	ErrorCodeInvalidFormat       ErrorCode = "INVALID_FORMAT"
	ErrorCodeInsufficientStock   ErrorCode = "INSUFFICIENT_STOCK"
	ErrorCodeReservationExpired  ErrorCode = "RESERVATION_EXPIRED"
	ErrorCodeReservationNotFound ErrorCode = "RESERVATION_NOT_FOUND"
	ErrorCodeInventoryNotFound   ErrorCode = "INVENTORY_NOT_FOUND"
	ErrorCodeDuplicateRequest    ErrorCode = "DUPLICATE_REQUEST"
	ErrorCodeRateLimitExceeded   ErrorCode = "RATE_LIMIT_EXCEEDED"
	ErrorCodeInternalError       ErrorCode = "INTERNAL_ERROR"
	ErrorCodeValidationError     ErrorCode = "VALIDATION_ERROR"
	ErrorCodeBusinessLogicError  ErrorCode = "BUSINESS_LOGIC_ERROR"
	ErrorCodeNotFound            ErrorCode = "NOT_FOUND"
	ErrorCodeAlreadyCommitted    ErrorCode = "ALREADY_COMMITTED"
	ErrorCodeAlreadyReleased     ErrorCode = "ALREADY_RELEASED"
	ErrorCodeExpired             ErrorCode = "EXPIRED"
	ErrorCodeDatabaseError       ErrorCode = "DATABASE_ERROR"
	ErrorCodeCacheError          ErrorCode = "CACHE_ERROR"
	ErrorCodeEventingError       ErrorCode = "EVENTING_ERROR"
)

const (
	ProblemTypeValidationError = "validation-error"
	ProblemTypeBusinessError   = "business-logic-error"
	ProblemTypeNotFound        = "not-found"
	ProblemTypeInternalError   = "internal-error"
	ProblemTypeRateLimit       = "rate-limit"
)

// Domain Models

// Inventory represents the inventory table structure
type Inventory struct {
	SKU          string    `db:"sku" json:"sku"`
	AvailableQty int       `db:"available_qty" json:"available_qty"`
	ReservedQty  int       `db:"reserved_qty" json:"reserved_qty"`
	Version      int64     `db:"version" json:"version"`
	UpdatedAt    time.Time `db:"updated_at" json:"updated_at"`
}

// Reservation represents the reservation table structure
type Reservation struct {
	ReservationID  uuid.UUID         `db:"reservation_id" json:"reservation_id"`
	SKU            string            `db:"sku" json:"sku"`
	Qty            int               `db:"qty" json:"qty"`
	Status         ReservationStatus `db:"status" json:"status"`
	ExpiresAt      time.Time         `db:"expires_at" json:"expires_at"`
	OwnerID        string            `db:"owner_id" json:"owner_id"`
	IdempotencyKey string            `db:"idempotency_key" json:"idempotency_key"`
	CreatedAt      time.Time         `db:"created_at" json:"created_at"`
	UpdatedAt      time.Time         `db:"updated_at" json:"updated_at"`
}

// OutboxEvent represents the outbox pattern table for reliable event publishing
type OutboxEvent struct {
	ID              int       `db:"id" json:"id"`
	EventType       string    `db:"event_type" json:"event_type"`
	Key             string    `db:"key" json:"key"`
	Payload         string    `db:"payload" json:"payload"`
	CreatedAt       time.Time `db:"created_at" json:"created_at"`
	Published       bool      `db:"published" json:"published"`
	PublishAttempts int       `db:"publish_attempts" json:"publish_attempts"`
	LastError       *string   `db:"last_error" json:"last_error,omitempty"`
}

// ProcessedEvent represents events that have been processed to prevent duplicates
type ProcessedEvent struct {
	EventID     string    `db:"event_id" json:"event_id"`
	EventType   string    `db:"event_type" json:"event_type"`
	SKU         string    `db:"sku" json:"sku"`
	ProcessedAt time.Time `db:"processed_at" json:"processed_at"`
}

// InventoryEvent represents events published to Kafka
type InventoryEvent struct {
	EventID        string            `json:"event_id"`
	EventType      string            `json:"event_type"`
	SKU            string            `json:"sku"`
	Qty            int               `json:"qty"`
	Version        int64             `json:"version"`
	ReservationID  *uuid.UUID        `json:"reservation_id,omitempty"`
	Status         ReservationStatus `json:"status,omitempty"`
	OwnerID        string            `json:"owner_id"`
	IdempotencyKey string            `json:"idempotency_key"`
	Timestamp      time.Time         `json:"timestamp"`
}

// InventoryState represents the current state published to state topic
type InventoryState struct {
	SKU          string    `json:"sku"`
	AvailableQty int       `json:"available_qty"`
	ReservedQty  int       `json:"reserved_qty"`
	Version      int64     `json:"version"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// API Request Models

// ReserveRequest represents a request to reserve inventory
type ReserveRequest struct {
	Qty            int    `json:"qty" binding:"required,min=1" validate:"required,min=1"`
	OwnerID        string `json:"owner_id" binding:"required" validate:"required"`
	IdempotencyKey string `json:"idempotency_key" binding:"required" validate:"required"`
}

// CreateReservationRequest represents a request to create a reservation
type CreateReservationRequest struct {
	Qty            int    `json:"qty" validate:"required,min=1"`
	OwnerID        string `json:"owner_id" validate:"required"`
	IdempotencyKey string `json:"idempotency_key" validate:"required"`
}

// CommitRequest represents a request to commit a reservation
type CommitRequest struct {
	IdempotencyKey string `json:"idempotency_key" binding:"required" validate:"required"`
}

// ReleaseRequest represents a request to release a reservation
type ReleaseRequest struct {
	IdempotencyKey string `json:"idempotency_key" binding:"required" validate:"required"`
}

// API Response Models

// ReserveResponse represents the response after creating a reservation
type ReserveResponse struct {
	ReservationID uuid.UUID         `json:"reservation_id"`
	SKU           string            `json:"sku"`
	Qty           int               `json:"qty"`
	Status        ReservationStatus `json:"status"`
	ExpiresAt     time.Time         `json:"expires_at"`
	Message       string            `json:"message"`
}

// ReservationResponse represents a detailed reservation response
type ReservationResponse struct {
	ID             string    `json:"id"`
	SKU            string    `json:"sku"`
	Quantity       int       `json:"quantity"`
	Status         string    `json:"status"`
	OwnerID        string    `json:"owner_id"`
	ExpiresAt      time.Time `json:"expires_at"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
	IdempotencyKey string    `json:"idempotency_key"`
}

// AvailabilityResponse represents the response for inventory availability
type AvailabilityResponse struct {
	SKU          string    `json:"sku"`
	AvailableQty int       `json:"available_qty"`
	ReservedQty  int       `json:"reserved_qty"`
	CacheHit     bool      `json:"cache_hit"`
	LastUpdated  time.Time `json:"last_updated"`
}

// ErrorResponse represents a structured error response
type ErrorResponse struct {
	Error   string            `json:"error"`
	Code    string            `json:"code"`
	Details map[string]string `json:"details,omitempty"`
}

// Enhanced Error Handling Models

// ValidationError represents validation errors with detailed field information
type ValidationError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
	Code    string `json:"code,omitempty"`
	Value   any    `json:"value,omitempty"`
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("validation error for field '%s': %s", e.Field, e.Message)
}

// BusinessError represents business logic errors
type BusinessError struct {
	Code    ErrorCode `json:"code"`
	Message string    `json:"message"`
	Details any       `json:"details,omitempty"`
}

func (e *BusinessError) Error() string {
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

// SystemError represents system-level errors (database, cache, external services)
type SystemError struct {
	Code      ErrorCode `json:"code"`
	Message   string    `json:"message"`
	Cause     error     `json:"-"` // Don't expose internal error details in JSON
	Component string    `json:"component"`
}

func (e *SystemError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("%s in %s: %s (caused by: %v)", e.Code, e.Component, e.Message, e.Cause)
	}
	return fmt.Sprintf("%s in %s: %s", e.Code, e.Component, e.Message)
}

func (e *SystemError) Unwrap() error {
	return e.Cause
}

// NotFoundError represents resource not found errors
type NotFoundError struct {
	Resource string `json:"resource"`
	ID       string `json:"id"`
}

func (e *NotFoundError) Error() string {
	return fmt.Sprintf("%s with ID '%s' not found", e.Resource, e.ID)
}

// ConflictError represents resource conflict errors
type ConflictError struct {
	Resource string `json:"resource"`
	Reason   string `json:"reason"`
}

func (e *ConflictError) Error() string {
	return fmt.Sprintf("conflict with %s: %s", e.Resource, e.Reason)
}

type ProblemDetails struct {
	Type     string      `json:"type"`
	Title    string      `json:"title"`
	Status   int         `json:"status"`
	Detail   string      `json:"detail,omitempty"`
	Instance string      `json:"instance,omitempty"`
	Field    string      `json:"field,omitempty"`
	Code     string      `json:"code,omitempty"`
	Errors   interface{} `json:"errors,omitempty"`
}

func NewProblemDetails(status int, title, detail string) *ProblemDetails {
	return &ProblemDetails{
		Type:   getProblemType(status),
		Title:  title,
		Status: status,
		Detail: detail,
	}
}

// NewValidationProblem creates a validation error problem
func NewValidationProblem(field, message string, code ErrorCode) *ProblemDetails {
	return &ProblemDetails{
		Type:   ProblemTypeValidationError,
		Title:  "Validation Failed",
		Status: 400,
		Detail: message,
		Field:  field,
		Code:   string(code),
	}
}

// NewMultiValidationProblem creates a multi-field validation error problem
func NewMultiValidationProblem(violations []ValidationError) *ProblemDetails {
	return &ProblemDetails{
		Type:   ProblemTypeValidationError,
		Title:  "Validation Failed",
		Status: 400,
		Detail: "Multiple validation errors occurred",
		Errors: violations,
	}
}

// NewBusinessLogicProblem creates a business logic error problem
func NewBusinessLogicProblem(status int, title, detail string, code ErrorCode) *ProblemDetails {
	return &ProblemDetails{
		Type:   ProblemTypeBusinessError,
		Title:  title,
		Status: status,
		Detail: detail,
		Code:   string(code),
	}
}

// NewNotFoundProblem creates a not found error problem
func NewNotFoundProblem(resource string) *ProblemDetails {
	return &ProblemDetails{
		Type:   ProblemTypeNotFound,
		Title:  "Resource Not Found",
		Status: 404,
		Detail: resource + " not found",
	}
}

// NewInternalErrorProblem creates an internal server error problem
func NewInternalErrorProblem() *ProblemDetails {
	return &ProblemDetails{
		Type:   ProblemTypeInternalError,
		Title:  "Internal Server Error",
		Status: 500,
		Detail: "An unexpected error occurred",
	}
}

// Helper function to get problem type URI based on status code
func getProblemType(status int) string {
	switch status {
	case 400:
		return ProblemTypeValidationError
	case 404:
		return ProblemTypeNotFound
	case 409, 422:
		return ProblemTypeBusinessError
	case 500:
		return ProblemTypeInternalError
	default:
		return ProblemTypeInternalError
	}
}

// Error factory functions for common scenarios

func NewValidationError(field, message string, value any) *ValidationError {
	return &ValidationError{
		Field:   field,
		Message: message,
		Value:   value,
	}
}

func NewBusinessError(code ErrorCode, message string, details any) *BusinessError {
	return &BusinessError{
		Code:    code,
		Message: message,
		Details: details,
	}
}

func NewSystemError(code ErrorCode, component, message string, cause error) *SystemError {
	return &SystemError{
		Code:      code,
		Message:   message,
		Cause:     cause,
		Component: component,
	}
}

func NewNotFoundError(resource, id string) *NotFoundError {
	return &NotFoundError{
		Resource: resource,
		ID:       id,
	}
}

func NewConflictError(resource, reason string) *ConflictError {
	return &ConflictError{
		Resource: resource,
		Reason:   reason,
	}
}

// Error type guards for better error handling

func IsValidationError(err error) bool {
	_, ok := err.(*ValidationError)
	return ok
}

func IsBusinessError(err error) bool {
	_, ok := err.(*BusinessError)
	return ok
}

func IsSystemError(err error) bool {
	_, ok := err.(*SystemError)
	return ok
}

func IsNotFoundError(err error) bool {
	_, ok := err.(*NotFoundError)
	return ok
}

func IsConflictError(err error) bool {
	_, ok := err.(*ConflictError)
	return ok
}

// GetErrorCode extracts error code from various error types
func GetErrorCode(err error) ErrorCode {
	switch e := err.(type) {
	case *ValidationError:
		return ErrorCodeValidationError
	case *BusinessError:
		return e.Code
	case *SystemError:
		return e.Code
	case *NotFoundError:
		return ErrorCodeReservationNotFound
	case *ConflictError:
		return ErrorCodeInsufficientStock
	default:
		return ErrorCodeInternalError
	}
}

type ReleaseReservationRequest struct {
	IdempotencyKey string `json:"idempotency_key"`
}

type CommitReservationRequest struct {
	IdempotencyKey string `json:"idempotency_key"`
}
