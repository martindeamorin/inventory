# Inventory System – High Concurrency, High Consistency

This project implements a **horizontally scalable inventory system**.  
The system prioritizes **strong consistency** first, **low latency** second.  

It is implemented in **Go 1.23** using **Gin** for HTTP APIs, **Postgres** for durable storage, **Kafka** for event streaming, and **Redis Cluster** for low-latency caching.  

---

## 🏗️ System Overview

```
┌─────────────┐    ┌─────────────┐    ┌─────────────┐
│   Client    │    │   Client    │    │   Client    │
│ Application │    │ Application │    │ Application │
└──────┬──────┘    └──────┬──────┘    └──────┬──────┘
       │                  │                  │
       │ Reads            │ Writes           │ Reads
       │                  │                  │
       ▼                  ▼                  ▼
┌─────────────┐    ┌─────────────┐    ┌─────────────┐
│   READER    │    │    QUEUE    │    │   READER    │
│  SERVICE    │    │   SERVICE   │    │  SERVICE    │
│  :8080      │    │    :8081    │    │  :8080      │
└──────┬──────┘    └──────┬──────┘    └──────┬──────┘
       │                  │                  │
       │                  │                  │
       ▼                  ▼                  ▼
┌─────────────┐    ┌─────────────┐    ┌─────────────┐
│    REDIS    │    │ POSTGRESQL  │    │    REDIS    │
│   CLUSTER   │◄───┤   DATABASE  ├───►│   CLUSTER   │
│  (Cache)    │    │  (Source)   │    │  (Cache)    │
└─────────────┘    └──────┬──────┘    └─────────────┘
       ▲                  │                  ▲
       │                  ▼                  │
       │           ┌─────────────┐           │
       │           │    KAFKA    │           │
       │           │   CLUSTER   │           │
       │           │ (Events)    │           │
       │           └──────┬──────┘           │
       │                  │                  │
       │                  ▼                  │
       │           ┌─────────────┐           │
       │           │ PROCESSOR   │           │
       └───────────┤  SERVICE    ├───────────┘
                   │   :8082     │
                   └─────────────┘

📊 Data Flow:
1. Queue Service receives write requests (reserve/commit/release)
2. Queue Service writes to PostgreSQL + publishes events to Kafka
3. Processor Service consumes events from Kafka
4. Processor Service updates PostgreSQL inventory (with locking)
5. Processor Service publishes state changes to Kafka
6. Reader Service subscribes to state changes and updates Redis cache
7. Reader Service serves read requests from Redis cache (with PostgreSQL fallback)
```

---

## 🚀 Core Features

### 🧩 Microservices
- **Queue Service**  
  - API entrypoint for clients.  
  - Accepts reservation, commit, and release requests.  
  - Writes events to Postgres Outbox → Kafka.  
  - Idempotency guaranteed via `idempotency_key`.  

- **Processor Service**  
  - Consumes events from Kafka.  
  - Updates Postgres inventory with **`SELECT ... FOR UPDATE`** (pessimistic locking).  
  - Publishes new stock states to Kafka (`inventory.state`).  
  - Sweeps expired reservations automatically.  

- **Reader Service**  
  - Handles read queries (availability checks).  
  - First tries **Redis cache**, falls back to Postgres if needed.  
  - Subscribes to `inventory.state` to update cache in near real-time.  

---

### 🔒 Consistency
- **Reservation States:**  
  - `PENDING` → hold stock temporarily.  
  - `COMMITTED` → confirmed sale.  
  - `RELEASED/EXPIRED` → stock returned.  

- **Concurrency Control:**  
  - Postgres row-level locks prevent overselling.  
  - Queue Service + Outbox ensure atomic event + DB writes.  

---

### ⚡ Low Latency
- **Redis Cluster** serves availability queries in ~1–2ms.  
- **Kafka** partitions events by SKU → ordered, scalable processing.  
- **CQRS split**: writes and reads are handled by different services.  

---

### 📈 Horizontal Scalability
- **Kafka**: KRaft mode, partitioned by SKU, replicated
- **Redis**: Cluster mode with consistent hashing  
- **PostgreSQL**: Read replicas + table partitioning
- **Services**: Stateless design enables horizontal scaling  

---

### 🛡️ Fault Tolerance
- **Kafka**: Replication (acks=all, min.insync.replicas=2)
- **Redis**: Master auto-failover with replica promotion  
- **PostgreSQL**: Connection pooling + retry logic
- **Services**: Graceful shutdown, health checks, and circuit breakers  

### **Dependencies**
- Kafka 
- Kafka UI 
- Redis Cluster
- PostgreSQL

---

## ✅ Why This Design Works
- **Consistency**: Postgres locking + Outbox pattern guarantee correctness  
- **Latency**: Redis cache + Kafka state updates provide fresh reads (~1-2ms)
- **Scalability**: Kafka partitions, Redis sharding, and service replication allow high throughput  
- **Resilience**: Each component can fail independently with graceful degradation  
- **Observability**: Structured logging and health checks across all services  


### 🧪 Test Suite 

- The system includes comprehensive unit tests,

## 🤖 AI Usage

- This project used mainly Claude Sonnet 4 plus Github Copilot to generate implementation of many of the code features, scripts and documentation. Refactors were made where neccesary and high system design decision were provided to the model.

## 📖 Resources

- For this project I looked into multiple sources:
    1. https://www.youtube.com/watch?v=uH163go3pvQ
    2. https://www.youtube.com/watch?v=2811UT5r5Jk
    3. https://www.youtube.com/watch?v=zFtTuKXGRiw
    4. https://medium.com/@bugfreeai/key-system-design-component-design-an-inventory-system-2e2befe45844
    5. https://www.cockroachlabs.com/blog/inventory-management-reference-architecture/
    6. https://ak67373.medium.com/designing-an-inventory-management-system-a143f014d501
