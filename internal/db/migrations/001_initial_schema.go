package migrations

import "time"

// InitialSchema creates the initial database schema
var InitialSchema = &Migration{
	ID:   "001_initial_schema",
	Name: "001_initial_schema",
	UpSQL: `
		-- Enable TimescaleDB extension
		CREATE EXTENSION IF NOT EXISTS timescaledb;

		-- Create aircraft_states hypertable
		CREATE TABLE IF NOT EXISTS aircraft_states (
			time TIMESTAMPTZ NOT NULL,
			hex_ident TEXT NOT NULL,
			callsign TEXT,
			altitude INTEGER,
			ground_speed INTEGER,
			track INTEGER,
			latitude DOUBLE PRECISION,
			longitude DOUBLE PRECISION,
			vertical_rate INTEGER,
			squawk TEXT,
			on_ground BOOLEAN,
			msg_type INTEGER,
			source TEXT
		);

		-- Create hypertable
		SELECT create_hypertable('aircraft_states', 'time');

		-- Create indexes
		CREATE INDEX IF NOT EXISTS idx_aircraft_states_hex_ident ON aircraft_states (hex_ident);
		CREATE INDEX IF NOT EXISTS idx_aircraft_states_callsign ON aircraft_states (callsign);

		-- Create flights table
		CREATE TABLE IF NOT EXISTS flights (
			session_id TEXT PRIMARY KEY,
			hex_ident TEXT NOT NULL,
			callsign TEXT,
			started_at TIMESTAMPTZ NOT NULL,
			ended_at TIMESTAMPTZ,
			first_latitude DOUBLE PRECISION,
			first_longitude DOUBLE PRECISION,
			last_latitude DOUBLE PRECISION,
			last_longitude DOUBLE PRECISION,
			max_altitude INTEGER,
			max_ground_speed INTEGER
		);

		-- Create indexes for flights
		CREATE INDEX IF NOT EXISTS idx_flights_hex_ident ON flights (hex_ident);
		CREATE INDEX IF NOT EXISTS idx_flights_started_at ON flights (started_at);
		CREATE INDEX IF NOT EXISTS idx_flights_ended_at ON flights (ended_at);

		-- Create statistics table
		CREATE TABLE IF NOT EXISTS system_stats (
			time TIMESTAMPTZ NOT NULL,
			total_messages BIGINT NOT NULL,
			parsed_messages BIGINT NOT NULL,
			failed_messages BIGINT NOT NULL,
			stored_states BIGINT NOT NULL,
			created_flights BIGINT NOT NULL,
			updated_flights BIGINT NOT NULL,
			ended_flights BIGINT NOT NULL,
			active_aircraft BIGINT NOT NULL,
			active_flights BIGINT NOT NULL,
			message_types BIGINT[] NOT NULL,
			processing_time_ms BIGINT NOT NULL,
			uptime_seconds BIGINT NOT NULL
		);

		-- Create hypertable for statistics
		SELECT create_hypertable('system_stats', 'time');

		-- Create index for statistics
		CREATE INDEX IF NOT EXISTS idx_system_stats_time ON system_stats (time DESC);
	`,
	DownSQL: `
		DROP TABLE IF EXISTS system_stats;
		DROP TABLE IF EXISTS flights;
		DROP TABLE IF EXISTS aircraft_states;
	`,
	CreatedAt: time.Now(),
} 