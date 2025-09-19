
CREATE EXTENSION IF NOT EXISTS "uuid-ossp";

CREATE TABLE inventory (
    sku VARCHAR(255) PRIMARY KEY,
    available_qty INTEGER NOT NULL DEFAULT 0 CHECK (available_qty >= 0),
    reserved_qty INTEGER NOT NULL DEFAULT 0 CHECK (reserved_qty >= 0),
    version BIGINT NOT NULL DEFAULT 0, -- For optimistic locking and event ordering
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW()
);

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
    
    CONSTRAINT fk_reservation_inventory FOREIGN KEY (sku) REFERENCES inventory(sku)
);

CREATE TABLE outbox (
    id SERIAL PRIMARY KEY,
    event_type VARCHAR(100) NOT NULL,
    key VARCHAR(255) NOT NULL,
    payload TEXT NOT NULL,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    published BOOLEAN NOT NULL DEFAULT FALSE,
    published_at TIMESTAMP WITH TIME ZONE NULL,
    publish_attempts INTEGER NOT NULL DEFAULT 0,
    last_error TEXT NULL,
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    consumed_at TIMESTAMP WITH TIME ZONE NULL,
    processed_by VARCHAR(100) NULL, 
    processing_status VARCHAR(20) NOT NULL DEFAULT 'pending' 
);

ALTER TABLE outbox ADD CONSTRAINT chk_outbox_processing_status 
    CHECK (processing_status IN ('pending', 'consumed', 'processed', 'failed'));

-- Inventory indexes
CREATE INDEX idx_inventory_SKU ON inventory(sku);

-- Reservation indexes
CREATE INDEX idx_reservation_sku ON reservation(sku);

-- Outbox indexes
CREATE INDEX idx_outbox_key ON outbox(key);

-- Add updated_at trigger function
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

CREATE TRIGGER update_outbox_updated_at 
    BEFORE UPDATE ON outbox 
    FOR EACH ROW 
    EXECUTE FUNCTION update_updated_at_column();

CREATE OR REPLACE FUNCTION cleanup_old_outbox_events(older_than_hours INTEGER DEFAULT 168)
RETURNS INTEGER AS $$
DECLARE
    rows_deleted INTEGER;
BEGIN
    DELETE FROM outbox 
    WHERE processing_status = 'processed' 
    AND consumed_at < NOW() - (older_than_hours || ' hours')::INTERVAL;
    
    GET DIAGNOSTICS rows_deleted = ROW_COUNT;
    
    RETURN rows_deleted;
END;
$$ LANGUAGE plpgsql;

-- Insert sample inventory data for testing
INSERT INTO inventory (sku, available_qty, reserved_qty, version) VALUES 
    ('LAPTOP-001', 100, 0, 0),
    ('PHONE-002', 50, 0, 0),
    ('TABLET-003', 75, 0, 0),
    ('HEADPHONES-004', 200, 0, 0),
    ('CAMERA-005', 25, 0, 0);