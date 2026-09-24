package migrations

// FlightLastSeen adds what the tracker needs to keep flight sessions across
// restarts without duplicating them.
//
// last_seen_at is when the aircraft was last heard. The tracker ends a flight
// once the aircraft has been silent for longer than its timeout, with
// ended_at = last_seen_at, and on startup it reloads the active flights: it
// needs last_seen_at to tell a flight to continue from one that already
// ended while the tracker was down. Rows from before this migration get
// COALESCE(ended_at, started_at).
//
// The partial unique index allows one active flight (ended_at IS NULL) per
// hex ident, so a second tracker or a bug cannot open a duplicate session. If
// duplicates exist, all but the most recent are ended first so the index can
// be built.
//
// Lock impact: ADD COLUMN without a default only changes the catalog (brief
// ACCESS EXCLUSIVE on flights); the UPDATEs and CREATE INDEX (SHARE lock,
// blocks writes to flights while it builds) scan flights only. flights is a
// small plain table (it is not a hypertable), so this takes milliseconds.
// aircraft_states is not touched.
var FlightLastSeen = &Migration{
	ID:   "004_flight_last_seen",
	Name: "004_flight_last_seen",
	UpSQL: `
	ALTER TABLE flights ADD COLUMN IF NOT EXISTS last_seen_at TIMESTAMPTZ;

	UPDATE flights
		SET last_seen_at = COALESCE(ended_at, started_at)
		WHERE last_seen_at IS NULL;

	UPDATE flights f
		SET ended_at = f.last_seen_at
		WHERE f.ended_at IS NULL
		AND EXISTS (
			SELECT 1 FROM flights g
			WHERE g.hex_ident = f.hex_ident
			AND g.ended_at IS NULL
			AND (g.started_at, g.session_id) > (f.started_at, f.session_id)
		);

	CREATE UNIQUE INDEX IF NOT EXISTS uq_flights_active_hex_ident
		ON flights (hex_ident) WHERE ended_at IS NULL;
	`,
	DownSQL: `
	DROP INDEX IF EXISTS uq_flights_active_hex_ident;
	ALTER TABLE flights DROP COLUMN IF EXISTS last_seen_at;
	`,
}
