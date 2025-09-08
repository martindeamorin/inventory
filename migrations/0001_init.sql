-- Migration: 0001_init.sql
-- Create initial database schema for inventory management system

-- Enable UUID extension if not already enabled
CREATE EXTENSION IF NOT EXISTS "uuid-ossp";

-- Create inventory table
-- This table stores the current stock levels for each SKU
CREATE TABLE inventory (
    sku VARCHAR(255) PRIMARY KEY,
    available_qty INTEGER NOT NULL DEFAULT 0 CHECK (available_qty >= 0),
    reserved_qty INTEGER NOT NULL DEFAULT 0 CHECK (reserved_qty >= 0),
    version BIGINT NOT NULL DEFAULT 0, -- For optimistic locking and event ordering
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW()
);

-- Create reservation table
-- This table tracks all inventory reservations and their states
CREATE TABLE reservation (
    reservation_id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    sku VARCHAR(255) NOT NULL,
    qty INTEGER NOT NULL CHECK (qty > 0),
    status VARCHAR(20) NOT NULL CHECK (status IN ('PENDING', 'COMMITTED', 'RELEASED', 'EXPIRED')),
    expires_at TIMESTAMP WITH TIME ZONE NOT NULL,
    owner_id VARCHAR(255) NOT NULL,
    idempotency_key VARCHAR(255) NOT NULL UNIQUE,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    
    -- Foreign key constraint
    CONSTRAINT fk_reservation_inventory FOREIGN KEY (sku) REFERENCES inventory(sku)
);

-- Create outbox table for reliable event publishing (Outbox Pattern)
-- This ensures events are published even if Kafka is temporarily unavailable
CREATE TABLE outbox (
    id SERIAL PRIMARY KEY,
    event_type VARCHAR(100) NOT NULL,
    key VARCHAR(255) NOT NULL, -- Typically the SKU for partitioning
    payload TEXT NOT NULL, -- JSON payload of the event
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    published BOOLEAN NOT NULL DEFAULT FALSE,
    published_at TIMESTAMP WITH TIME ZONE NULL, -- When event was successfully published
    publish_attempts INTEGER NOT NULL DEFAULT 0,
    last_error TEXT NULL,
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW()
);

-- Indexes for performance

-- Inventory indexes
CREATE INDEX idx_inventory_updated_at ON inventory(updated_at);

-- Reservation indexes
CREATE INDEX idx_reservation_sku ON reservation(sku);
CREATE INDEX idx_reservation_status ON reservation(status);
CREATE INDEX idx_reservation_expires_at ON reservation(expires_at);
CREATE INDEX idx_reservation_owner_id ON reservation(owner_id);
CREATE INDEX idx_reservation_idempotency_key ON reservation(idempotency_key);
CREATE INDEX idx_reservation_status_expires ON reservation(status, expires_at) WHERE status = 'PENDING';

-- Outbox indexes
CREATE INDEX idx_outbox_published ON outbox(published, created_at) WHERE published = FALSE;
CREATE INDEX idx_outbox_event_type ON outbox(event_type);
CREATE INDEX idx_outbox_created_at ON outbox(created_at);
-- Optimized index for ordered outbox processing with advisory lock
CREATE INDEX idx_outbox_unpublished_ordered ON outbox(published, id) WHERE published = FALSE;

-- Add updated_at trigger for reservation table
CREATE OR REPLACE FUNCTION update_updated_at_column()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$ language 'plpgsql';

CREATE TRIGGER update_reservation_updated_at 
    BEFORE UPDATE ON reservation 
    FOR EACH ROW 
    EXECUTE FUNCTION update_updated_at_column();

CREATE TRIGGER update_inventory_updated_at 
    BEFORE UPDATE ON inventory 
    FOR EACH ROW 
    EXECUTE FUNCTION update_updated_at_column();

-- Trigger to automatically update updated_at for outbox table
CREATE TRIGGER update_outbox_updated_at 
    BEFORE UPDATE ON outbox 
    FOR EACH ROW 
    EXECUTE FUNCTION update_updated_at_column();

-- Insert some sample inventory data for testing
INSERT INTO inventory (sku, available_qty, reserved_qty, version) VALUES 
    ('LAPTOP-001', 100, 0, 0),
    ('PHONE-002', 50, 0, 0),
    ('TABLET-003', 75, 0, 0),
    ('HEADPHONES-004', 200, 0, 0),
    ('CAMERA-005', 25, 0, 0);

-- Comments explaining the schema design

COMMENT ON TABLE inventory IS 'Stores current inventory levels for each SKU. Uses optimistic locking via version field.';
COMMENT ON COLUMN inventory.available_qty IS 'Quantity available for new reservations';
COMMENT ON COLUMN inventory.reserved_qty IS 'Quantity currently reserved (PENDING status)';
COMMENT ON COLUMN inventory.version IS 'Version number for optimistic locking and event ordering (starts at 0, incremented with each change)';

COMMENT ON TABLE reservation IS 'Tracks inventory reservations through their lifecycle: PENDING -> COMMITTED/RELEASED/EXPIRED';
COMMENT ON COLUMN reservation.status IS 'Reservation state: PENDING (temporary hold), COMMITTED (sale completed), RELEASED (manually released), EXPIRED (automatically released)';
COMMENT ON COLUMN reservation.expires_at IS 'When PENDING reservations automatically expire and release stock';
COMMENT ON COLUMN reservation.idempotency_key IS 'Unique key to prevent duplicate reservations from the same request';

COMMENT ON TABLE outbox IS 'Outbox pattern table for reliable event publishing to Kafka. Ensures events are not lost even if Kafka is unavailable.';
COMMENT ON COLUMN outbox.payload IS 'JSON serialized event data to be published to Kafka';
COMMENT ON COLUMN outbox.publish_attempts IS 'Number of times we attempted to publish this event (for retry logic)';

-- Grant permissions (adjust as needed for your environment)
-- GRANT SELECT, INSERT, UPDATE, DELETE ON inventory TO inventory_service;
-- GRANT SELECT, INSERT, UPDATE, DELETE ON reservation TO inventory_service;
-- GRANT SELECT, INSERT, UPDATE, DELETE ON outbox TO inventory_service;
-- GRANT USAGE, SELECT ON SEQUENCE outbox_id_seq TO inventory_service;
