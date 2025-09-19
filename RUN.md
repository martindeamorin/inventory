# 🚀 Run Guide - Inventory Management System

## Quick Start

Get the entire inventory management system running with a single command:

```bash
./start-all.sh
```

**That's it!** The script handles everything automatically. ⚡

## 📋 Prerequisites

### **Required Software**
- **Docker**: Version 20.10+ ([Install Docker](https://docs.docker.com/get-docker/))
- **Docker Compose**: Version 2.0+ (usually included with Docker Desktop)
- **Git**: For cloning the repository
- **Bash**: For running the start script (WSL on Windows, Terminal on macOS/Linux)

### **System Requirements**

| Environment | RAM | CPU | Disk Space |
|-------------|-----|-----|------------|
| **Development** | 8GB | 4 cores | 10GB |
| **Production** | 16GB+ | 8+ cores | 50GB+ |

### **Port Requirements**

Ensure these ports are available:
- **8080**: Reader Service
- **8081**: Queue Service  
- **8082**: Processor Service
- **5432**: PostgreSQL
- **6379**: Redis
- **9092**: Kafka
- **8090**: Kafka UI

## 🏃‍♂️ Running the System

### **1. Clone the Repository**
```bash
git clone <repository-url>
cd inventory-app-2
```

### **2. Make Script Executable (Linux/macOS)**
```bash
chmod +x start-all.sh
```

### **3. Start All Services**
```bash
./start-all.sh
```

**What the script does:**
1. ✅ Stops any existing containers
2. ✅ Builds all service images
3. ✅ Starts infrastructure (PostgreSQL, Redis, Kafka)
4. ✅ Runs database migrations with sample data
5. ✅ Starts application services
6. ✅ Performs health checks
7. ✅ Displays service URLs


### **Quick Health Check**
```bash
# Test all services
curl http://localhost:8080/health  # Reader
curl http://localhost:8081/health  # Queue  
curl http://localhost:8082/health  # Processor
```

## 🎮 Using the System - Complete Examples

The database is pre-populated with sample inventory data from migrations:


### **1. Check Inventory Availability**
```bash
curl http://localhost:8080/api/v1/inventory/LAPTOP-001/availability
```

**Response:**
```json
{
  "sku": "LAPTOP-001",
  "available_qty": 100,
  "reserved_qty": 0,
  "cache_hit": false,
  "last_updated": "2025-09-08T10:30:00Z"
}
```

### **2. Reserve Stock**
```bash
curl -X POST http://localhost:8081/api/v1/inventory/LAPTOP-001/reserve \
  -H "Content-Type: application/json" \
  -d '{
    "qty": 2,
    "owner_id": "user-123", 
    "idempotency_key": "req-456"
  }'
```

**Response:**
```json
{
  "reservation_id": "550e8400-e29b-41d4-a716-446655440000",
  "sku": "LAPTOP-001",
  "qty": 2,
  "status": "PENDING",
  "expires_at": "2025-09-08T10:35:00Z",
  "message": "Stock reserved successfully"
}
```

### **3. Commit Reservation (Complete Sale)**
```bash
curl -X POST http://localhost:8081/api/v1/reservations/550e8400-e29b-41d4-a716-446655440000/commit \
  -H "Content-Type: application/json" \
  -d '{
    "idempotency_key": "commit-456"
  }'
```

**Response:**
- 204 No Content

### **4. Release Reservation (Cancel Sale)**
```bash
curl -X POST http://localhost:8081/api/v1/reservations/550e8400-e29b-41d4-a716-446655440001/release \
  -H "Content-Type: application/json" \
  -d '{
    "idempotency_key": "release-789"
  }'
```


## 🧪 Running Tests

The inventory system includes comprehensive unit tests covering all core functionality. Use these commands to run the test suite:

### **Run All Tests**
```bash
go test ./internal/test -v
```

### **Run Specific Test File**
```bash
# Run only inventory service tests
go test ./internal/test -v -run TestInventoryService

# Run only model validation tests
go test ./internal/test -v -run TestModel

# Run only cache tests
go test ./internal/test -v -run TestCache

# Run only repository tests
go test ./internal/test -v -run TestMockDB
```