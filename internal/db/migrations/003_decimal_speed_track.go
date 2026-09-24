package migrations

// DecimalSpeedTrack stores ground speed and track as DOUBLE PRECISION.
//
// SBS/BaseStation sends both with a decimal ("157.9" knots, "295.7" degrees)
// and the Go types are float64, but 001 created the columns as INTEGER, so
// every state with a fractional value failed with
// `pq: invalid input syntax for type integer`. flights.max_ground_speed gets
// the same value and had the same type.
//
// Widening INTEGER to DOUBLE PRECISION is lossless for the existing rows. It
// rewrites the tables (aircraft_states chunks included) under an ACCESS
// EXCLUSIVE lock, which is short at the lab's volume.
var DecimalSpeedTrack = &Migration{
	ID:   "003_decimal_speed_track",
	Name: "003_decimal_speed_track",
	UpSQL: `
	ALTER TABLE aircraft_states
		ALTER COLUMN ground_speed TYPE DOUBLE PRECISION,
		ALTER COLUMN track TYPE DOUBLE PRECISION;

	ALTER TABLE flights
		ALTER COLUMN max_ground_speed TYPE DOUBLE PRECISION;
	`,
	DownSQL: `
	ALTER TABLE flights
		ALTER COLUMN max_ground_speed TYPE INTEGER USING round(max_ground_speed)::INTEGER;

	ALTER TABLE aircraft_states
		ALTER COLUMN ground_speed TYPE INTEGER USING round(ground_speed)::INTEGER,
		ALTER COLUMN track TYPE INTEGER USING round(track)::INTEGER;
	`,
}
