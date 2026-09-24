package db

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/lib/pq"
	"github.com/saviobatista/sbs-logger/internal/types"
)

type Client struct {
	db *sql.DB
}

// New creates a new database client
func New(connStr string) (*Client, error) {
	db, err := sql.Open("postgres", connStr)
	if err != nil {
		return nil, err
	}
	return &Client{db: db}, nil
}

// Close closes the database connection
func (c *Client) Close() error {
	return c.db.Close()
}

// ErrFlightNotFound is returned by UpdateFlight when no row has the flight's
// session_id. Before, an UPDATE that matched nothing succeeded silently, which
// hid that flights were never inserted.
var ErrFlightNotFound = errors.New("flight not found")

// GetActiveFlights retrieves all active flights (ended_at IS NULL)
func (c *Client) GetActiveFlights() ([]*types.Flight, error) {
	query := `
		SELECT session_id, hex_ident, callsign, started_at, last_seen_at,
			first_latitude, first_longitude, last_latitude, last_longitude,
			max_altitude, max_ground_speed
		FROM flights
		WHERE ended_at IS NULL
	`
	rows, err := c.db.Query(query)
	if err != nil {
		return nil, err
	}
	defer func() {
		if cerr := rows.Close(); cerr != nil {
			fmt.Fprintf(os.Stderr, "error closing rows: %v\n", cerr)
		}
	}()

	var flights []*types.Flight
	for rows.Next() {
		var (
			f                          types.Flight
			callsign                   sql.NullString
			lastSeen                   sql.NullTime
			firstLat, firstLon         sql.NullFloat64
			lastLat, lastLon, maxSpeed sql.NullFloat64
			maxAlt                     sql.NullInt64
		)
		// Only active rows are selected, so ended_at is always NULL and is
		// not read: scanning a NULL into time.Time fails, which made the
		// tracker unable to start as soon as one active flight existed.
		if err := rows.Scan(
			&f.SessionID, &f.HexIdent, &callsign, &f.StartedAt, &lastSeen,
			&firstLat, &firstLon, &lastLat, &lastLon,
			&maxAlt, &maxSpeed,
		); err != nil {
			return nil, err
		}
		f.Callsign = callsign.String
		f.LastSeenAt = f.StartedAt
		if lastSeen.Valid {
			f.LastSeenAt = lastSeen.Time
		}
		f.FirstLatitude, f.FirstLongitude = firstLat.Float64, firstLon.Float64
		f.LastLatitude, f.LastLongitude = lastLat.Float64, lastLon.Float64
		f.MaxAltitude = int(maxAlt.Int64)
		f.MaxGroundSpeed = maxSpeed.Float64
		flights = append(flights, &f)
	}
	return flights, rows.Err()
}

// nullTime maps the zero time to NULL
func nullTime(t time.Time) sql.NullTime {
	return sql.NullTime{Time: t, Valid: !t.IsZero()}
}

// CreateFlight creates a new flight
func (c *Client) CreateFlight(flight *types.Flight) error {
	query := `
		INSERT INTO flights (
			session_id, hex_ident, callsign, started_at, ended_at, last_seen_at,
			first_latitude, first_longitude, last_latitude, last_longitude,
			max_altitude, max_ground_speed
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
	`
	_, err := c.db.Exec(query,
		flight.SessionID, flight.HexIdent, flight.Callsign, flight.StartedAt,
		nullTime(flight.EndedAt), nullTime(flight.LastSeenAt),
		flight.FirstLatitude, flight.FirstLongitude, flight.LastLatitude, flight.LastLongitude,
		flight.MaxAltitude, flight.MaxGroundSpeed,
	)
	return err
}

// UpdateFlight writes the flight's current values. A zero EndedAt keeps the
// flight active (ended_at NULL); it used to be written as 0001-01-01, which
// would have ended every flight on its first update.
func (c *Client) UpdateFlight(flight *types.Flight) error {
	query := `
		UPDATE flights SET
			callsign = $1, ended_at = $2, last_seen_at = $3,
			first_latitude = $4, first_longitude = $5,
			last_latitude = $6, last_longitude = $7,
			max_altitude = $8, max_ground_speed = $9
		WHERE session_id = $10
	`
	res, err := c.db.Exec(query,
		flight.Callsign, nullTime(flight.EndedAt), nullTime(flight.LastSeenAt),
		flight.FirstLatitude, flight.FirstLongitude,
		flight.LastLatitude, flight.LastLongitude,
		flight.MaxAltitude, flight.MaxGroundSpeed,
		flight.SessionID,
	)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return fmt.Errorf("%w: session_id %q", ErrFlightNotFound, flight.SessionID)
	}
	return nil
}

// StoreAircraftState stores an aircraft state
func (c *Client) StoreAircraftState(state *types.AircraftState) error {
	query := `
		INSERT INTO aircraft_states (
			time, hex_ident, callsign, altitude, ground_speed,
			track, latitude, longitude, vertical_rate, squawk,
			on_ground, msg_type
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
	`
	_, err := c.db.Exec(query,
		state.Timestamp, state.HexIdent, state.Callsign, state.Altitude,
		state.GroundSpeed, state.Track, state.Latitude, state.Longitude,
		state.VerticalRate, state.Squawk, state.OnGround, state.MsgType,
	)
	return err
}

// stateInsertChunk bounds the rows of one multi-row INSERT (12 parameters
// per row, PostgreSQL allows 65535 per statement).
const stateInsertChunk = 1000

// StoreAircraftStates stores many aircraft states in one transaction with
// multi-row INSERTs. One INSERT per state made every state wait for its own
// WAL flush (~130 ms on the lab disk), which capped the tracker at a few
// states per second against ~55 msg/s from the receiver.
func (c *Client) StoreAircraftStates(states []*types.AircraftState) error {
	if len(states) == 0 {
		return nil
	}
	tx, err := c.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	for start := 0; start < len(states); start += stateInsertChunk {
		end := min(start+stateInsertChunk, len(states))
		query, args := buildStatesInsert(states[start:end])
		if _, err := tx.Exec(query, args...); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func buildStatesInsert(states []*types.AircraftState) (string, []interface{}) {
	const cols = 12
	var b strings.Builder
	b.WriteString(`INSERT INTO aircraft_states (
			time, hex_ident, callsign, altitude, ground_speed,
			track, latitude, longitude, vertical_rate, squawk,
			on_ground, msg_type
		) VALUES `)
	args := make([]interface{}, 0, len(states)*cols)
	for i, state := range states {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteByte('(')
		for j := 1; j <= cols; j++ {
			if j > 1 {
				b.WriteString(", ")
			}
			b.WriteString("$" + strconv.Itoa(i*cols+j))
		}
		b.WriteByte(')')
		args = append(args,
			state.Timestamp, state.HexIdent, state.Callsign, state.Altitude,
			state.GroundSpeed, state.Track, state.Latitude, state.Longitude,
			state.VerticalRate, state.Squawk, state.OnGround, state.MsgType,
		)
	}
	return b.String(), args
}

// StoreSystemStats stores system statistics
func (c *Client) StoreSystemStats(stats map[string]interface{}) error {
	query := `
		INSERT INTO system_stats (
			time, total_messages, parsed_messages, failed_messages,
			stored_states, created_flights, updated_flights, ended_flights,
			active_aircraft, active_flights, message_types,
			processing_time_ms, uptime_seconds
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13
		)
	`

	// Convert message types array
	msgTypes := stats["message_types"].([10]uint64)
	msgTypesArray := make([]int64, len(msgTypes))
	for i, v := range msgTypes {
		if v > uint64(1<<63-1) {
			msgTypesArray[i] = int64(1<<63 - 1) // Max int64 value
		} else {
			msgTypesArray[i] = int64(v)
		}
	}

	// Convert processing time to milliseconds
	processingTime := stats["processing_time"].(time.Duration).Milliseconds()

	// Calculate uptime in seconds
	uptime := time.Since(stats["last_message_time"].(time.Time)).Seconds()

	_, err := c.db.Exec(query,
		time.Now(),
		stats["total_messages"],
		stats["parsed_messages"],
		stats["failed_messages"],
		stats["stored_states"],
		stats["created_flights"],
		stats["updated_flights"],
		stats["ended_flights"],
		stats["active_aircraft"],
		stats["active_flights"],
		pq.Array(msgTypesArray),
		processingTime,
		int64(uptime),
	)

	return err
}

// GetSystemStats retrieves system statistics for a time range
func (c *Client) GetSystemStats(start, end time.Time) ([]map[string]interface{}, error) {
	query := `
		SELECT 
			time, total_messages, parsed_messages, failed_messages,
			stored_states, created_flights, updated_flights, ended_flights,
			active_aircraft, active_flights, message_types,
			processing_time_ms, uptime_seconds
		FROM system_stats
		WHERE time BETWEEN $1 AND $2
		ORDER BY time DESC
	`

	rows, err := c.db.Query(query, start, end)
	if err != nil {
		return nil, err
	}
	defer func() {
		if cerr := rows.Close(); cerr != nil {
			fmt.Fprintf(os.Stderr, "error closing rows: %v\n", cerr)
		}
	}()

	var stats []map[string]interface{}
	for rows.Next() {
		var (
			timestamp        time.Time
			totalMessages    int64
			parsedMessages   int64
			failedMessages   int64
			storedStates     int64
			createdFlights   int64
			updatedFlights   int64
			endedFlights     int64
			activeAircraft   int64
			activeFlights    int64
			messageTypes     []int64
			processingTimeMs int64
			uptimeSeconds    int64
		)

		if err := rows.Scan(
			&timestamp,
			&totalMessages,
			&parsedMessages,
			&failedMessages,
			&storedStates,
			&createdFlights,
			&updatedFlights,
			&endedFlights,
			&activeAircraft,
			&activeFlights,
			pq.Array(&messageTypes),
			&processingTimeMs,
			&uptimeSeconds,
		); err != nil {
			return nil, err
		}

		// Convert message types array
		msgTypes := [10]uint64{}
		for i, v := range messageTypes {
			if i < len(msgTypes) {
				if v < 0 {
					msgTypes[i] = 0 // Min uint64 value
				} else {
					msgTypes[i] = uint64(v)
				}
			}
		}

		stat := map[string]interface{}{
			"time":            timestamp,
			"total_messages":  totalMessages,
			"parsed_messages": parsedMessages,
			"failed_messages": failedMessages,
			"stored_states":   storedStates,
			"created_flights": createdFlights,
			"updated_flights": updatedFlights,
			"ended_flights":   endedFlights,
			"active_aircraft": activeAircraft,
			"active_flights":  activeFlights,
			"message_types":   msgTypes,
			"processing_time": time.Duration(processingTimeMs) * time.Millisecond,
			"uptime_seconds":  uptimeSeconds,
		}

		stats = append(stats, stat)
	}

	return stats, rows.Err()
}
