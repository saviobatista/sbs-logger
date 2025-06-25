package migrations

var RetentionPolicies = &Migration{
	ID:   "002_retention_policies",
	Name: "002_retention_policies",
	UpSQL: `
	-- Set retention policy for aircraft_states (30 days)
	SELECT add_retention_policy('aircraft_states', INTERVAL '30 days');

	-- Set retention policy for system_stats (90 days)
	SELECT add_retention_policy('system_stats', INTERVAL '90 days');

	-- Create continuous aggregate for daily system stats
	CREATE MATERIALIZED VIEW IF NOT EXISTS system_stats_daily
	WITH (timescaledb.continuous) AS
	SELECT
		time_bucket('1 day', time) AS day,
		SUM(total_messages) AS total_messages,
		SUM(parsed_messages) AS parsed_messages,
		SUM(failed_messages) AS failed_messages,
		SUM(stored_states) AS stored_states,
		SUM(created_flights) AS created_flights,
		SUM(updated_flights) AS updated_flights,
		SUM(ended_flights) AS ended_flights
	FROM system_stats
	GROUP BY day
	WITH NO DATA;

	-- Create continuous aggregate for hourly aircraft states
	CREATE MATERIALIZED VIEW IF NOT EXISTS aircraft_states_hourly
	WITH (timescaledb.continuous) AS
	SELECT
		time_bucket('1 hour', time) AS hour,
		COUNT(*) AS state_count
	FROM aircraft_states
	GROUP BY hour
	WITH NO DATA;
	`,
	DownSQL: `
	DROP MATERIALIZED VIEW IF EXISTS system_stats_daily;
	DROP MATERIALIZED VIEW IF EXISTS aircraft_states_hourly;
	-- Remove retention policies
	SELECT remove_retention_policy('aircraft_states');
	SELECT remove_retention_policy('system_stats');
	`,
} 