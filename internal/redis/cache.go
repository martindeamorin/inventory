package redis

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog/log"

	"inventory-service/internal/models"
)

// CacheClient wraps Redis client for inventory caching with cluster support
type CacheClient struct {
	client    redis.UniversalClient // Universal client supports both single and cluster
	ttl       time.Duration
	keyPrefix string
}

// NewCacheClient creates a new Redis cache client with cluster support
func NewCacheClient(addrs []string, password string, clusterMode bool, ttl time.Duration, keyPrefix string) *CacheClient {
	var client redis.UniversalClient

	if clusterMode {
		// Redis Cluster configuration for enterprise scaling (3 masters + 3 replicas)
		client = redis.NewClusterClient(&redis.ClusterOptions{
			Addrs:        addrs,
			Password:     password,
			MaxRetries:   3,
			PoolSize:     50, // Larger pool for cluster
			MinIdleConns: 5,
			PoolTimeout:  30 * time.Second,
			// Cluster-specific options
			MaxRedirects:   8,
			ReadOnly:       false, // Allow reads from replicas
			RouteByLatency: true,  // Route to lowest latency node
		})
	} else {
		// Single Redis instance for development
		addr := "localhost:6379"
		if len(addrs) > 0 {
			addr = addrs[0]
		}
		client = redis.NewClient(&redis.Options{
			Addr:     addr,
			Password: password,
			DB:       0, // DB is not supported in cluster mode
			PoolSize: 10,
		})
	}

	return &CacheClient{
		client:    client,
		ttl:       ttl,
		keyPrefix: keyPrefix,
	}
}

// GetInventory retrieves inventory from cache
func (c *CacheClient) GetInventory(ctx context.Context, sku string) (*models.Inventory, error) {
	key := c.inventoryKey(sku)

	val, err := c.client.Get(ctx, key).Result()
	if err != nil {
		if err == redis.Nil {
			// Cache miss
			return nil, nil
		}
		log.Error().Err(err).Str("sku", sku).Msg("Failed to get inventory from cache")
		return nil, fmt.Errorf("failed to get inventory from cache: %w", err)
	}

	var inventory models.Inventory
	if err := json.Unmarshal([]byte(val), &inventory); err != nil {
		log.Error().Err(err).Str("sku", sku).Msg("Failed to unmarshal cached inventory")
		return nil, fmt.Errorf("failed to unmarshal cached inventory: %w", err)
	}

	log.Debug().Str("sku", sku).Msg("Cache hit for inventory")
	return &inventory, nil
}

// GetAvailableStock retrieves only the available stock quantity with timeout support
func (c *CacheClient) GetAvailableStock(ctx context.Context, sku string) (int, error) {
	inventory, err := c.GetInventory(ctx, sku)
	if err != nil {
		return 0, err
	}
	if inventory == nil {
		return 0, fmt.Errorf("inventory not found in cache: %s", sku)
	}
	return inventory.AvailableQty, nil
}

// GetAvailableStockWithTimeout retrieves available stock with a specific timeout
func (c *CacheClient) GetAvailableStockWithTimeout(ctx context.Context, sku string, timeout time.Duration) (int, error) {
	timeoutCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	return c.GetAvailableStock(timeoutCtx, sku)
}

// SetInventory stores inventory in cache
func (c *CacheClient) SetInventory(ctx context.Context, inventory *models.Inventory) error {
	key := c.inventoryKey(inventory.SKU)

	data, err := json.Marshal(inventory)
	if err != nil {
		return fmt.Errorf("failed to marshal inventory: %w", err)
	}

	err = c.client.Set(ctx, key, data, c.ttl).Err()
	if err != nil {
		log.Error().Err(err).Str("sku", inventory.SKU).Msg("Failed to set inventory in cache")
		return fmt.Errorf("failed to set inventory in cache: %w", err)
	}

	log.Debug().Str("sku", inventory.SKU).Msg("Cached inventory")
	return nil
}

// DeleteInventory removes inventory from cache
func (c *CacheClient) DeleteInventory(ctx context.Context, sku string) error {
	key := c.inventoryKey(sku)

	err := c.client.Del(ctx, key).Err()
	if err != nil {
		log.Error().Err(err).Str("sku", sku).Msg("Failed to delete inventory from cache")
		return fmt.Errorf("failed to delete inventory from cache: %w", err)
	}

	log.Debug().Str("sku", sku).Msg("Deleted inventory from cache")
	return nil
}

// UpdateInventoryFromState updates cache from Kafka state event
func (c *CacheClient) UpdateInventoryFromState(ctx context.Context, state *models.InventoryState) error {
	inventory := &models.Inventory{
		SKU:          state.SKU,
		AvailableQty: state.AvailableQty,
		ReservedQty:  state.ReservedQty,
		Version:      state.Version,
		UpdatedAt:    state.UpdatedAt,
	}

	return c.SetInventory(ctx, inventory)
}

// Ping checks if Redis is available
func (c *CacheClient) Ping(ctx context.Context) error {
	return c.client.Ping(ctx).Err()
}

// Close closes the Redis connection
func (c *CacheClient) Close() error {
	return c.client.Close()
}

// inventoryKey generates the cache key for inventory with prefix
func (c *CacheClient) inventoryKey(sku string) string {
	return fmt.Sprintf("%sinventory:%s", c.keyPrefix, sku)
}
