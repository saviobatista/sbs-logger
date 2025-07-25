package redis

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/saviobatista/sbs-logger/internal/types"
)

// RedisClientInterface defines the Redis operations used by our client
type RedisClientInterface interface {
	Ping(ctx context.Context) *redis.StatusCmd
	Set(ctx context.Context, key string, value interface{}, expiration time.Duration) *redis.StatusCmd
	Get(ctx context.Context, key string) *redis.StringCmd
	Del(ctx context.Context, keys ...string) *redis.IntCmd
	Close() error
}

// Client manages Redis connections and operations
type Client struct {
	client RedisClientInterface
}

// New creates a new Redis client
func New(addr string) (*Client, error) {
	client := redis.NewClient(&redis.Options{
		Addr:     addr,
		Password: "", // no password set
		DB:       0,  // use default DB
	})

	// Test connection
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("failed to connect to Redis: %w", err)
	}

	return &Client{client: client}, nil
}

// NewWithClient creates a new Redis client with a custom RedisClientInterface (useful for testing)
func NewWithClient(client RedisClientInterface) *Client {
	return &Client{client: client}
}

// Close closes the Redis connection
func (c *Client) Close() error {
	return c.client.Close()
}

// StoreFlight stores flight data in Redis
func (c *Client) StoreFlight(ctx context.Context, flight *types.Flight) error {
	data, err := json.Marshal(flight)
	if err != nil {
		return fmt.Errorf("failed to marshal flight data: %w", err)
	}

	key := fmt.Sprintf("flight:%s", flight.HexIdent)
	return c.client.Set(ctx, key, data, 24*time.Hour).Err()
}

// getData retrieves data from Redis and unmarshals it into the target
func (c *Client) getData(ctx context.Context, key string, target interface{}, dataType string) error {
	data, err := c.client.Get(ctx, key).Bytes()
	if err == redis.Nil {
		return nil // Data not found
	}
	if err != nil {
		return fmt.Errorf("failed to get %s data: %w", dataType, err)
	}

	if err := json.Unmarshal(data, target); err != nil {
		return fmt.Errorf("failed to unmarshal %s data: %w", dataType, err)
	}

	return nil
}

// GetFlight retrieves flight data from Redis
func (c *Client) GetFlight(ctx context.Context, hexIdent string) (*types.Flight, error) {
	key := fmt.Sprintf("flight:%s", hexIdent)
	var flight types.Flight
	if err := c.getData(ctx, key, &flight, "flight"); err != nil {
		return nil, err
	}
	return &flight, nil
}

// DeleteFlight removes flight data from Redis
func (c *Client) DeleteFlight(ctx context.Context, hexIdent string) error {
	key := fmt.Sprintf("flight:%s", hexIdent)
	return c.client.Del(ctx, key).Err()
}

// StoreAircraftState stores the latest aircraft state in Redis
func (c *Client) StoreAircraftState(ctx context.Context, state *types.AircraftState) error {
	data, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("failed to marshal aircraft state: %w", err)
	}

	key := fmt.Sprintf("aircraft:%s", state.HexIdent)
	return c.client.Set(ctx, key, data, 1*time.Hour).Err()
}

// GetAircraftState retrieves the latest aircraft state from Redis
func (c *Client) GetAircraftState(ctx context.Context, hexIdent string) (*types.AircraftState, error) {
	key := fmt.Sprintf("aircraft:%s", hexIdent)
	var state types.AircraftState
	if err := c.getData(ctx, key, &state, "aircraft state"); err != nil {
		return nil, err
	}
	return &state, nil
}

// DeleteAircraftState removes aircraft state from Redis
func (c *Client) DeleteAircraftState(ctx context.Context, hexIdent string) error {
	key := fmt.Sprintf("aircraft:%s", hexIdent)
	return c.client.Del(ctx, key).Err()
}

// SetFlightValidation sets flight validation data
func (c *Client) SetFlightValidation(ctx context.Context, hexIdent string, valid bool) error {
	key := fmt.Sprintf("validation:%s", hexIdent)
	value := "1"
	if !valid {
		value = "0"
	}
	return c.client.Set(ctx, key, value, 24*time.Hour).Err()
}

// GetFlightValidation gets flight validation status
func (c *Client) GetFlightValidation(ctx context.Context, hexIdent string) (bool, error) {
	key := fmt.Sprintf("validation:%s", hexIdent)
	val, err := c.client.Get(ctx, key).Result()
	if err == redis.Nil {
		return false, nil // No validation data
	}
	if err != nil {
		return false, fmt.Errorf("failed to get validation data: %w", err)
	}

	return val == "1", nil
}
