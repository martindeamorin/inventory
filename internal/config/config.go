package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Config holds all configuration for the application with enterprise scaling support
type Config struct {
	// Database configuration with read replicas support
	DatabaseURL          string
	DatabaseMaxConns     int
	DatabaseMaxIdleConns int
	DatabaseReadReplicas []string // Read replicas for Reader Service

	// Kafka Cluster configuration (3-5 broker cluster)
	KafkaBrokers           []string
	KafkaEventsTopicName   string
	KafkaStateTopicName    string
	KafkaConsumerGroup     string
	KafkaPartitions        int    // 12 partitions for events, 6 for state
	KafkaReplicationFactor int    // Replication factor = 3
	KafkaAcks              string // acks=all for durability
	KafkaMinInsyncReplicas int    // min.insync.replicas = 2
	KafkaRetries           int    // Producer retries
	KafkaIdempotent        bool   // Enable idempotent producer

	// Redis Cluster configuration (3 masters + 3 replicas)
	RedisAddrs       []string // Multiple Redis cluster nodes
	RedisPassword    string
	RedisClusterMode bool // Enable cluster mode
	RedisMaxRetries  int
	RedisPoolSize    int
	RedisTTL         time.Duration
	RedisKeyPrefix   string // For key namespacing

	// Server configuration
	ServerAddr string
	ServerPort string

	// Reservation configuration
	ReservationTTL time.Duration

	// Stock pre-check configuration
	MaxReasonableReservation int           // Maximum allowed reservation quantity
	CacheTimeout             time.Duration // Timeout for cache operations during pre-check
	StockSafetyBuffer        float64       // Safety buffer percentage (0.1 = 10%)
	EnableStockPreCheck      bool          // Enable/disable stock pre-check feature

	// Service configuration
	ServiceName  string
	InstanceID   string // Unique instance identifier
	ReplicaCount int    // Number of service replicas

	// Scaling configuration
	Environment              string // dev, staging, prod
	EnableMetrics            bool   // Enable Prometheus metrics
	EnableDistributedTracing bool   // Enable tracing

	// Performance tuning
	WorkerPoolSize    int // Worker goroutine pool size
	BatchSize         int // Batch processing size
	ProcessingTimeout time.Duration
}

// LoadConfig loads configuration from environment variables with enterprise defaults
func LoadConfig() *Config {
	instanceID := getEnv("INSTANCE_ID", uuid.New().String()[:8])
	environment := getEnv("ENVIRONMENT", "development")

	cfg := &Config{
		// Database - Enhanced for scaling with connection pooling
		DatabaseURL:          getEnv("DATABASE_URL", "postgres://postgres:postgres@localhost:5432/inventory?sslmode=disable"),
		DatabaseMaxConns:     getEnvAsInt("DATABASE_MAX_CONNS", getDefaultMaxConns(environment)),
		DatabaseMaxIdleConns: getEnvAsInt("DATABASE_MAX_IDLE_CONNS", getDefaultIdleConns(environment)),
		DatabaseReadReplicas: getEnvAsStringSlice("DATABASE_READ_REPLICAS", []string{}),

		// Kafka Cluster - Production configuration (3-5 brokers)
		KafkaBrokers:           getEnvAsStringSlice("KAFKA_BROKERS", []string{"localhost:9092"}),
		KafkaEventsTopicName:   getEnv("KAFKA_EVENTS_TOPIC", "inventory.events"),
		KafkaStateTopicName:    getEnv("KAFKA_STATE_TOPIC", "inventory.state"),
		KafkaConsumerGroup:     getEnv("KAFKA_CONSUMER_GROUP", "inventory-processor"),
		KafkaPartitions:        getEnvAsInt("KAFKA_PARTITIONS", getDefaultPartitions(environment)),
		KafkaReplicationFactor: getEnvAsInt("KAFKA_REPLICATION_FACTOR", getDefaultReplicationFactor(environment)),
		KafkaAcks:              getEnv("KAFKA_ACKS", "all"), // Wait for all replicas
		KafkaMinInsyncReplicas: getEnvAsInt("KAFKA_MIN_INSYNC_REPLICAS", getDefaultMinInsyncReplicas(environment)),
		KafkaRetries:           getEnvAsInt("KAFKA_RETRIES", 10),
		KafkaIdempotent:        getEnvAsBool("KAFKA_IDEMPOTENT", true),

		// Redis Cluster - Enhanced for horizontal scaling (3 masters + 3 replicas)
		RedisAddrs:       getEnvAsStringSlice("REDIS_ADDRS", []string{"localhost:6379"}),
		RedisPassword:    getEnv("REDIS_PASSWORD", ""),
		RedisClusterMode: getEnvAsBool("REDIS_CLUSTER_MODE", len(getEnvAsStringSlice("REDIS_ADDRS", []string{})) > 1),
		RedisMaxRetries:  getEnvAsInt("REDIS_MAX_RETRIES", 3),
		RedisPoolSize:    getEnvAsInt("REDIS_POOL_SIZE", getDefaultRedisPoolSize(environment)),
		RedisTTL:         time.Duration(getEnvAsInt("REDIS_TTL_SEC", 300)) * time.Second,
		RedisKeyPrefix:   getEnv("REDIS_KEY_PREFIX", fmt.Sprintf("inv:%s:", environment)),

		// Server
		ServerAddr: getEnv("SERVER_ADDR", "0.0.0.0"),
		ServerPort: getEnv("SERVER_PORT", "8080"),

		// Reservation
		ReservationTTL: time.Duration(getEnvAsInt("RESERVATION_TTL_SEC", 300)) * time.Second,

		// Stock pre-check configuration
		MaxReasonableReservation: getEnvAsInt("MAX_RESERVATION_QTY", 1000),
		CacheTimeout:             time.Duration(getEnvAsInt("CACHE_TIMEOUT_MS", 2)) * time.Millisecond,
		StockSafetyBuffer:        getEnvAsFloat("STOCK_BUFFER_PCT", 0.1), // 10%
		EnableStockPreCheck:      getEnvAsBool("ENABLE_STOCK_PRECHECK", true),

		// Service identification
		ServiceName:  getEnv("SERVICE_NAME", "inventory-service"),
		InstanceID:   instanceID,
		ReplicaCount: getEnvAsInt("REPLICA_COUNT", 1),

		// Scaling configuration
		Environment:              environment,
		EnableMetrics:            getEnvAsBool("ENABLE_METRICS", environment == "production"),
		EnableDistributedTracing: getEnvAsBool("ENABLE_TRACING", environment == "production"),

		// Performance tuning
		WorkerPoolSize:    getEnvAsInt("WORKER_POOL_SIZE", getDefaultWorkerPoolSize(environment)),
		BatchSize:         getEnvAsInt("BATCH_SIZE", 100),
		ProcessingTimeout: time.Duration(getEnvAsInt("PROCESSING_TIMEOUT_SEC", 30)) * time.Second,
	}

	return cfg
}

// Environment-specific defaults for production scaling

func getDefaultMaxConns(env string) int {
	switch env {
	case "production":
		return 25 // Per instance, adjust based on your DB limits
	case "staging":
		return 15
	default:
		return 10
	}
}

func getDefaultIdleConns(env string) int {
	switch env {
	case "production":
		return 5
	case "staging":
		return 3
	default:
		return 2
	}
}

func getDefaultPartitions(env string) int {
	switch env {
	case "production":
		return 12 // inventory.events: 12 partitions as specified
	case "staging":
		return 6
	default:
		return 3
	}
}

func getDefaultReplicationFactor(env string) int {
	switch env {
	case "production":
		return 3 // replication.factor=3 as specified
	case "staging":
		return 2
	default:
		return 1
	}
}

func getDefaultMinInsyncReplicas(env string) int {
	switch env {
	case "production":
		return 2 // min.insync.replicas=2 as specified
	case "staging":
		return 1
	default:
		return 1
	}
}

func getDefaultRedisPoolSize(env string) int {
	switch env {
	case "production":
		return 50 // Larger pool for Redis cluster
	case "staging":
		return 20
	default:
		return 10
	}
}

func getDefaultWorkerPoolSize(env string) int {
	switch env {
	case "production":
		return 20 // More workers for high throughput
	case "staging":
		return 10
	default:
		return 5
	}
}

// Helper functions

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func getEnvAsInt(key string, defaultValue int) int {
	valueStr := getEnv(key, "")
	if value, err := strconv.Atoi(valueStr); err == nil {
		return value
	}
	return defaultValue
}

func getEnvAsBool(key string, defaultValue bool) bool {
	valueStr := getEnv(key, "")
	if value, err := strconv.ParseBool(valueStr); err == nil {
		return value
	}
	return defaultValue
}

func getEnvAsFloat(key string, defaultValue float64) float64 {
	valueStr := getEnv(key, "")
	if value, err := strconv.ParseFloat(valueStr, 64); err == nil {
		return value
	}
	return defaultValue
}

func getEnvAsStringSlice(key string, defaultValue []string) []string {
	valueStr := getEnv(key, "")
	if valueStr == "" {
		return defaultValue
	}

	// Support both comma and semicolon separated values
	values := strings.FieldsFunc(valueStr, func(c rune) bool {
		return c == ',' || c == ';'
	})

	// Trim spaces
	for i, v := range values {
		values[i] = strings.TrimSpace(v)
	}

	return values
}

// GetKafkaProducerConfig returns optimized Kafka producer configuration for durability
func (c *Config) GetKafkaProducerConfig() map[string]interface{} {
	return map[string]interface{}{
		"acks":                                  c.KafkaAcks,       // "all" for durability
		"retries":                               c.KafkaRetries,    // High retries
		"enable.idempotence":                    c.KafkaIdempotent, // Exactly-once semantics
		"max.in.flight.requests.per.connection": 5,                 // Parallelism with ordering
		"compression.type":                      "snappy",          // Better CPU/bandwidth trade-off
		"linger.ms":                             5,                 // Small batching for latency
		"batch.size":                            16384,             // 16KB batch size
		"request.timeout.ms":                    30000,             // 30s timeout
		"delivery.timeout.ms":                   120000,            // 2 min total timeout
	}
}

// GetKafkaConsumerConfig returns optimized Kafka consumer configuration
func (c *Config) GetKafkaConsumerConfig() map[string]interface{} {
	return map[string]interface{}{
		"enable.auto.commit":      false,       // Manual commit for exactly-once
		"auto.offset.reset":       "earliest",  // Start from beginning
		"session.timeout.ms":      30000,       // 30s session timeout
		"heartbeat.interval.ms":   3000,        // 3s heartbeat
		"max.poll.records":        c.BatchSize, // Batch processing
		"fetch.min.bytes":         1,           // Don't wait for full batch
		"fetch.max.wait.ms":       500,         // Max wait 500ms
		"connections.max.idle.ms": 540000,      // 9 min idle timeout
	}
}

// IsProduction returns true if running in production environment
func (c *Config) IsProduction() bool {
	return c.Environment == "production"
}

// GetDatabaseConfig returns database configuration string with connection pooling
func (c *Config) GetDatabaseConfig() string {
	if strings.Contains(c.DatabaseURL, "?") {
		return fmt.Sprintf("%s&pool_max_conns=%d&pool_max_conn_idle_time=30s",
			c.DatabaseURL, c.DatabaseMaxConns)
	}
	return fmt.Sprintf("%s?pool_max_conns=%d&pool_max_conn_idle_time=30s",
		c.DatabaseURL, c.DatabaseMaxConns)
}

// GetStateTopicPartitions returns partitions for state topic (compacted)
func (c *Config) GetStateTopicPartitions() int {
	if c.IsProduction() {
		return 6 // inventory.state: 6 partitions as specified
	}
	return c.KafkaPartitions / 2
}
