-- Migration: 0002_add_processed_events.sql
-- Add processed_events table for event deduplication

-- Create processed_events table to track processed events and prevent duplicates
CREATE TABLE processed_events (
    event_id VARCHAR(255) PRIMARY KEY,
    event_type VARCHAR(100) NOT NULL,
    sku VARCHAR(255) NOT NULL,
    processed_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW()
);

-- Index for efficient cleanup of old processed events
CREATE INDEX idx_processed_events_processed_at ON processed_events(processed_at);

-- Index for event type and SKU lookups
CREATE INDEX idx_processed_events_sku_type ON processed_events(sku, event_type);
