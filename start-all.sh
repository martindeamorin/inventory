#!/bin/bash

echo "🚀 Starting Inventory Management System with Docker Compose..."

# Navigate to the project directory
cd "$(dirname "$0")"

# Stop any existing containers
echo "🛑 Stopping existing containers..."
docker compose -f docker/docker-compose.yml down 2>/dev/null || docker-compose -f docker/docker-compose.yml down 2>/dev/null

# Clean up volumes if needed (optional - commented out to preserve data)
# docker volume prune -f

# Start all services
echo "▶️  Starting all services..."
if command -v "docker compose" &> /dev/null; then
    docker compose -f docker/docker-compose.yml up --build -d
else
    docker-compose -f docker/docker-compose.yml up --build -d
fi

# Wait a bit for services to start
echo "⏳ Waiting for infrastructure services to start..."
sleep 10

# Wait for Kafka to be ready
echo "⏳ Waiting for Kafka to be ready..."
for i in {1..30}; do
    if docker exec inventory-kafka kafka-broker-api-versions.sh --bootstrap-server localhost:9092 > /dev/null 2>&1; then
        echo "   ✅ Kafka is ready!"
        break
    fi
    if [ $i -eq 30 ]; then
        echo "   ❌ Kafka did not become ready in time"
        exit 1
    fi
    echo "   ⏳ Waiting for Kafka... (attempt $i/30)"
    sleep 2
done

# Ensure topics are created
echo "📝 Ensuring Kafka topics are created..."
docker exec inventory-kafka kafka-topics.sh --bootstrap-server localhost:9092 --create --topic inventory.events --partitions 3 --replication-factor 1 --if-not-exists > /dev/null 2>&1
docker exec inventory-kafka kafka-topics.sh --bootstrap-server localhost:9092 --create --topic inventory.state --partitions 3 --replication-factor 1 --if-not-exists > /dev/null 2>&1

# Wait for application services
echo "⏳ Waiting for application services to start..."
sleep 10

# Check status
echo ""
echo "📊 Service Status:"
if command -v "docker compose" &> /dev/null; then
    docker compose -f docker/docker-compose.yml ps
else
    docker-compose -f docker/docker-compose.yml ps
fi

# Test health endpoints
echo "🔍 Health Checks:"

# Check Kafka topics
echo "   📋 Checking Kafka topics..."
topics=$(docker exec inventory-kafka kafka-topics.sh --bootstrap-server localhost:9092 --list 2>/dev/null | grep "inventory\." | wc -l)
if [ "$topics" -ge 2 ]; then
    echo "   ✅ Kafka Topics: Ready (inventory.events, inventory.state)"
else
    echo "   ❌ Kafka Topics: Missing"
fi

for service in "Reader:8080" "Queue:8081" "Processor:8082"; do
    name=$(echo $service | cut -d: -f1)
    port=$(echo $service | cut -d: -f2)
    
    response=$(curl -s -o /dev/null -w "%{http_code}" http://localhost:$port/health 2>/dev/null || echo "000")
    if [ "$response" = "200" ]; then
        echo "   ✅ $name Service: Healthy"
    else
        echo "   ❌ $name Service: Not responding (HTTP $response)"
    fi
done

echo "🚀 All services started! Use the following to monitor:"
echo "   • View logs: docker compose -f docker/docker-compose.yml logs -f"
echo "   • Stop all:  docker compose -f docker/docker-compose.yml down"
echo "   • Monitor:   http://localhost:8090 (Kafka UI)"
