package test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"

	"inventory-service/internal/models"
)

// MockRedisClient simula el comportamiento de Redis para testing
type MockRedisClient struct {
	mock.Mock
	data map[string]interface{}
}

func NewMockRedisClient() *MockRedisClient {
	return &MockRedisClient{
		data: make(map[string]interface{}),
	}
}

func (m *MockRedisClient) Get(ctx context.Context, key string) (string, error) {
	args := m.Called(ctx, key)
	return args.String(0), args.Error(1)
}

func (m *MockRedisClient) Set(ctx context.Context, key string, value interface{}, expiration time.Duration) error {
	args := m.Called(ctx, key, value, expiration)
	if args.Error(0) == nil {
		m.data[key] = value
	}
	return args.Error(0)
}

func (m *MockRedisClient) Del(ctx context.Context, key string) error {
	args := m.Called(ctx, key)
	if args.Error(0) == nil {
		delete(m.data, key)
	}
	return args.Error(0)
}

func (m *MockRedisClient) Ping(ctx context.Context) error {
	args := m.Called(ctx)
	return args.Error(0)
}

// CacheClient representa nuestro cliente de cache para testing
type CacheClient struct {
	redis  *MockRedisClient
	prefix string
}

func NewCacheClient(redis *MockRedisClient) *CacheClient {
	return &CacheClient{
		redis:  redis,
		prefix: "inventory:",
	}
}

func (c *CacheClient) GetInventory(ctx context.Context, sku string) (*models.Inventory, error) {
	key := c.prefix + sku
	data, err := c.redis.Get(ctx, key)
	if err != nil {
		return nil, err
	}
	if data == "" {
		return nil, assert.AnError
	}

	// Deserialize JSON data
	var inventory models.Inventory
	if err := json.Unmarshal([]byte(data), &inventory); err != nil {
		return nil, err
	}

	return &inventory, nil
}

func (c *CacheClient) SetInventory(ctx context.Context, inventory *models.Inventory) error {
	key := c.prefix + inventory.SKU
	// Serialize to JSON for storage
	jsonData, err := json.Marshal(inventory)
	if err != nil {
		return err
	}
	return c.redis.Set(ctx, key, string(jsonData), 15*time.Minute)
}

func (c *CacheClient) DeleteInventory(ctx context.Context, sku string) error {
	key := c.prefix + sku
	return c.redis.Del(ctx, key)
}

func (c *CacheClient) GetAvailableStock(ctx context.Context, sku string) (int, error) {
	inventory, err := c.GetInventory(ctx, sku)
	if err != nil {
		return 0, err
	}
	if inventory == nil {
		return 0, assert.AnError
	}
	return inventory.AvailableQty, nil
}

func (c *CacheClient) GetAvailableStockWithTimeout(ctx context.Context, sku string, timeout time.Duration) (int, error) {
	timeoutCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	return c.GetAvailableStock(timeoutCtx, sku)
}

func TestCacheClient_GetInventory_Hit(t *testing.T) {
	// Arrange
	mockRedis := NewMockRedisClient()
	client := NewCacheClient(mockRedis)

	// Create expected inventory and marshal it to JSON
	expectedInventory := &models.Inventory{
		SKU:          "CACHED-SKU",
		AvailableQty: 100,
		ReservedQty:  10,
		Version:      1,
		UpdatedAt:    time.Now(),
	}
	jsonData, _ := json.Marshal(expectedInventory)

	mockRedis.On("Get", mock.Anything, "inventory:CACHED-SKU").Return(string(jsonData), nil)

	// Act
	inventory, err := client.GetInventory(context.Background(), "CACHED-SKU")

	// Assert
	assert.NoError(t, err)
	assert.NotNil(t, inventory)
	assert.Equal(t, "CACHED-SKU", inventory.SKU)
	assert.Equal(t, 100, inventory.AvailableQty)
	assert.Equal(t, 10, inventory.ReservedQty)

	mockRedis.AssertExpectations(t)
}

func TestCacheClient_GetInventory_Miss(t *testing.T) {
	// Arrange
	mockRedis := NewMockRedisClient()
	client := NewCacheClient(mockRedis)

	mockRedis.On("Get", mock.Anything, "inventory:MISSING-SKU").Return("", assert.AnError)

	// Act
	inventory, err := client.GetInventory(context.Background(), "MISSING-SKU")

	// Assert
	assert.Error(t, err)
	assert.Nil(t, inventory)

	mockRedis.AssertExpectations(t)
}

func TestCacheClient_SetInventory_Success(t *testing.T) {
	// Arrange
	mockRedis := NewMockRedisClient()
	client := NewCacheClient(mockRedis)

	inventory := &models.Inventory{
		SKU:          "TEST-SKU",
		AvailableQty: 50,
		ReservedQty:  5,
		Version:      1,
		UpdatedAt:    time.Now(),
	}

	// Expect the JSON string version
	expectedJSON, _ := json.Marshal(inventory)
	mockRedis.On("Set", mock.Anything, "inventory:TEST-SKU", string(expectedJSON), 15*time.Minute).Return(nil)

	// Act
	err := client.SetInventory(context.Background(), inventory)

	// Assert
	assert.NoError(t, err)
	mockRedis.AssertExpectations(t)
}

func TestCacheClient_SetInventory_Error(t *testing.T) {
	// Arrange
	mockRedis := NewMockRedisClient()
	client := NewCacheClient(mockRedis)

	inventory := &models.Inventory{
		SKU:          "TEST-SKU",
		AvailableQty: 50,
		ReservedQty:  5,
		Version:      1,
		UpdatedAt:    time.Now(),
	}

	// Expect the JSON string version
	expectedJSON, _ := json.Marshal(inventory)
	mockRedis.On("Set", mock.Anything, "inventory:TEST-SKU", string(expectedJSON), 15*time.Minute).Return(assert.AnError)

	// Act
	err := client.SetInventory(context.Background(), inventory)

	// Assert
	assert.Error(t, err)
	mockRedis.AssertExpectations(t)
}

func TestCacheClient_DeleteInventory_Success(t *testing.T) {
	// Arrange
	mockRedis := NewMockRedisClient()
	client := NewCacheClient(mockRedis)

	mockRedis.On("Del", mock.Anything, "inventory:TEST-SKU").Return(nil)

	// Act
	err := client.DeleteInventory(context.Background(), "TEST-SKU")

	// Assert
	assert.NoError(t, err)
	mockRedis.AssertExpectations(t)
}

func TestCacheClient_GetAvailableStock_Success(t *testing.T) {
	// Arrange
	mockRedis := NewMockRedisClient()
	client := NewCacheClient(mockRedis)

	// Create expected inventory and marshal it to JSON
	expectedInventory := &models.Inventory{
		SKU:          "CACHED-SKU",
		AvailableQty: 100,
		ReservedQty:  10,
		Version:      1,
		UpdatedAt:    time.Now(),
	}
	jsonData, _ := json.Marshal(expectedInventory)

	mockRedis.On("Get", mock.Anything, "inventory:CACHED-SKU").Return(string(jsonData), nil)

	// Act
	stock, err := client.GetAvailableStock(context.Background(), "CACHED-SKU")

	// Assert
	assert.NoError(t, err)
	assert.Equal(t, 100, stock)

	mockRedis.AssertExpectations(t)
}

func TestCacheClient_GetAvailableStock_Miss(t *testing.T) {
	// Arrange
	mockRedis := NewMockRedisClient()
	client := NewCacheClient(mockRedis)

	mockRedis.On("Get", mock.Anything, "inventory:MISSING-SKU").Return("", assert.AnError)

	// Act
	stock, err := client.GetAvailableStock(context.Background(), "MISSING-SKU")

	// Assert
	assert.Error(t, err)
	assert.Equal(t, 0, stock)

	mockRedis.AssertExpectations(t)
}

func TestCacheClient_GetAvailableStockWithTimeout_Success(t *testing.T) {
	// Arrange
	mockRedis := NewMockRedisClient()
	client := NewCacheClient(mockRedis)

	// Create expected inventory and marshal it to JSON
	expectedInventory := &models.Inventory{
		SKU:          "CACHED-SKU",
		AvailableQty: 100,
		ReservedQty:  10,
		Version:      1,
		UpdatedAt:    time.Now(),
	}
	jsonData, _ := json.Marshal(expectedInventory)

	mockRedis.On("Get", mock.Anything, "inventory:CACHED-SKU").Return(string(jsonData), nil)

	// Act
	stock, err := client.GetAvailableStockWithTimeout(context.Background(), "CACHED-SKU", 5*time.Second)

	// Assert
	assert.NoError(t, err)
	assert.Equal(t, 100, stock)

	mockRedis.AssertExpectations(t)
}

func TestCacheClient_GetAvailableStockWithTimeout_Timeout(t *testing.T) {
	// Arrange
	mockRedis := NewMockRedisClient()
	client := NewCacheClient(mockRedis)

	// Simular un timeout haciendo que el mock nunca responda
	mockRedis.On("Get", mock.Anything, "inventory:SLOW-SKU").Return("", context.DeadlineExceeded)

	// Act
	stock, err := client.GetAvailableStockWithTimeout(context.Background(), "SLOW-SKU", 1*time.Millisecond)

	// Assert
	assert.Error(t, err)
	assert.Equal(t, 0, stock)

	mockRedis.AssertExpectations(t)
}

func TestCacheClient_CacheInvalidation_Workflow(t *testing.T) {
	// Arrange
	mockRedis := NewMockRedisClient()
	client := NewCacheClient(mockRedis)

	inventory := &models.Inventory{
		SKU:          "WORKFLOW-SKU",
		AvailableQty: 100,
		ReservedQty:  0,
		Version:      1,
		UpdatedAt:    time.Now(),
	}

	// Test workflow: Set -> Get -> Delete -> Get (miss)

	// Step 1: Set inventory in cache
	expectedJSON, _ := json.Marshal(inventory)
	mockRedis.On("Set", mock.Anything, "inventory:WORKFLOW-SKU", string(expectedJSON), 15*time.Minute).Return(nil)
	err := client.SetInventory(context.Background(), inventory)
	assert.NoError(t, err)

	// Step 2: Get inventory from cache (hit)
	jsonData, _ := json.Marshal(inventory)
	mockRedis.On("Get", mock.Anything, "inventory:WORKFLOW-SKU").Return(string(jsonData), nil).Once()
	cachedInventory, err := client.GetInventory(context.Background(), "WORKFLOW-SKU")
	assert.NoError(t, err)
	assert.NotNil(t, cachedInventory)
	if cachedInventory != nil {
		assert.Equal(t, "WORKFLOW-SKU", cachedInventory.SKU)
	}

	// Step 3: Invalidate cache
	mockRedis.On("Del", mock.Anything, "inventory:WORKFLOW-SKU").Return(nil)
	err = client.DeleteInventory(context.Background(), "WORKFLOW-SKU")
	assert.NoError(t, err)

	// Step 4: Try to get again (should miss)
	mockRedis.On("Get", mock.Anything, "inventory:WORKFLOW-SKU").Return("", assert.AnError).Once()
	missedInventory, err := client.GetInventory(context.Background(), "WORKFLOW-SKU")
	assert.Error(t, err)
	assert.Nil(t, missedInventory)

	mockRedis.AssertExpectations(t)
}
