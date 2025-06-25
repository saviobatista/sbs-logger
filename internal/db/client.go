package db

import (
	"database/sql"
	"time"

	"github.com/lib/pq"
	"github.com/savio/sbs-logger/internal/types"
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

// GetActiveFlights retrieves all active flights
func (c *Client) GetActiveFlights() ([]*types.Flight, error) {
	query := `
		SELECT session_id, hex_ident, callsign, started_at, ended_at,
			first_latitude, first_longitude, last_latitude, last_longitude,
			max_altitude, max_ground_speed
		FROM flights
		WHERE ended_at IS NULL
	`
	rows, err := c.db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var flights []*types.Flight
	for rows.Next() {
		var f types.Flight
		if err := rows.Scan(
			&f.SessionID, &f.HexIdent, &f.Callsign, &f.StartedAt, &f.EndedAt,
			&f.FirstLatitude, &f.FirstLongitude, &f.LastLatitude, &f.LastLongitude,
			&f.MaxAltitude, &f.MaxGroundSpeed,
		); err != nil {
			return nil, err
		}
		flights = append(flights, &f)
	}
	return flights, rows.Err()
}

// CreateFlight creates a new flight
func (c *Client) CreateFlight(flight *types.Flight) error {
	query := `
		INSERT INTO flights (
			session_id, hex_ident, callsign, started_at,
			first_latitude, first_longitude, last_latitude, last_longitude,
			max_altitude, max_ground_speed
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
	`
	_, err := c.db.Exec(query,
		flight.SessionID, flight.HexIdent, flight.Callsign, flight.StartedAt,
		flight.FirstLatitude, flight.FirstLongitude, flight.LastLatitude, flight.LastLongitude,
		flight.MaxAltitude, flight.MaxGroundSpeed,
	)
	return err
}

// UpdateFlight updates an existing flight
func (c *Client) UpdateFlight(flight *types.Flight) error {
	query := `
		UPDATE flights SET
			callsign = $1, ended_at = $2,
			last_latitude = $3, last_longitude = $4,
			max_altitude = $5, max_ground_speed = $6
		WHERE session_id = $7
	`
	_, err := c.db.Exec(query,
		flight.Callsign, flight.EndedAt,
		flight.LastLatitude, flight.LastLongitude,
		flight.MaxAltitude, flight.MaxGroundSpeed,
		flight.SessionID,
	)
	return err
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
		msgTypesArray[i] = int64(v)
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
	defer rows.Close()

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
				msgTypes[i] = uint64(v)
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
