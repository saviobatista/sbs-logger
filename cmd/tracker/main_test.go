package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	goredis "github.com/redis/go-redis/v9"
	"github.com/saviobatista/sbs-logger/internal/db"
	"github.com/saviobatista/sbs-logger/internal/metrics"
	redisclient "github.com/saviobatista/sbs-logger/internal/redis"
	"github.com/saviobatista/sbs-logger/internal/types"
)

// UNIT TESTS WITH MOCKS (Fast - no external dependencies)

type mockDBClient struct {
	flights      []*types.Flight
	createError  error
	updateError  error
	storeError   error
	getError     error
	creates      int
	updates      int
	stateBatches []int
}

func (m *mockDBClient) GetActiveFlights() ([]*types.Flight, error) {
	if m.getError != nil {
		return nil, m.getError
	}
	var active []*types.Flight
	for _, f := range m.flights {
		if f.EndedAt.IsZero() {
			c := *f
			active = append(active, &c)
		}
	}
	return active, nil
}

func (m *mockDBClient) CreateFlight(flight *types.Flight) error {
	if m.createError != nil {
		return m.createError
	}
	m.creates++
	stored := *flight
	m.flights = append(m.flights, &stored)
	return nil
}

// UpdateFlight behaves like the real client: updating a session that was
// never inserted is an error, not a silent no-op.
func (m *mockDBClient) UpdateFlight(flight *types.Flight) error {
	if m.updateError != nil {
		return m.updateError
	}
	for i, f := range m.flights {
		if f.SessionID == flight.SessionID {
			m.updates++
			stored := *flight
			m.flights[i] = &stored
			return nil
		}
	}
	return fmt.Errorf("%w: session_id %q", db.ErrFlightNotFound, flight.SessionID)
}

func (m *mockDBClient) StoreAircraftStates(states []*types.AircraftState) error {
	if m.storeError != nil {
		return m.storeError
	}
	m.stateBatches = append(m.stateBatches, len(states))
	return nil
}

func (m *mockDBClient) Close() error { return nil }

type mockRedisClient struct {
	flights        map[string]*types.Flight
	aircraftStates map[string]*types.AircraftState
	storeError     error
	getError       error
}

func newMockRedisClient() *mockRedisClient {
	return &mockRedisClient{
		flights:        make(map[string]*types.Flight),
		aircraftStates: make(map[string]*types.AircraftState),
	}
}

func (m *mockRedisClient) StoreFlight(ctx context.Context, flight *types.Flight) error {
	if m.storeError != nil {
		return m.storeError
	}
	m.flights[flight.HexIdent] = flight
	return nil
}

func (m *mockRedisClient) GetFlight(ctx context.Context, hexIdent string) (*types.Flight, error) {
	if m.getError != nil {
		return nil, m.getError
	}
	flight, exists := m.flights[hexIdent]
	if !exists {
		return nil, nil
	}
	return flight, nil
}

func (m *mockRedisClient) DeleteFlight(ctx context.Context, hexIdent string) error {
	delete(m.flights, hexIdent)
	return nil
}

func (m *mockRedisClient) StoreAircraftState(ctx context.Context, state *types.AircraftState) error {
	if m.storeError != nil {
		return m.storeError
	}
	m.aircraftStates[state.HexIdent] = state
	return nil
}

func (m *mockRedisClient) GetAircraftState(ctx context.Context, hexIdent string) (*types.AircraftState, error) {
	if m.getError != nil {
		return nil, m.getError
	}
	state, exists := m.aircraftStates[hexIdent]
	if !exists {
		return nil, nil
	}
	return state, nil
}

func (m *mockRedisClient) DeleteAircraftState(ctx context.Context, hexIdent string) error {
	delete(m.aircraftStates, hexIdent)
	return nil
}

func (m *mockRedisClient) Close() error { return nil }

// Unit Tests

func TestStateTracker_New(t *testing.T) {
	mockDB := &mockDBClient{}
	mockRedis := newMockRedisClient()

	tracker := NewStateTracker(mockDB, mockRedis)

	if tracker.db != mockDB || tracker.redis != mockRedis {
		t.Error("Expected clients to be set correctly")
	}
	if tracker.activeFlights == nil || tracker.states == nil || tracker.stats == nil {
		t.Error("Expected maps and stats to be initialized")
	}
}

func TestStateTracker_Start(t *testing.T) {
	tests := []struct {
		name        string
		mockDB      *mockDBClient
		expectError bool
	}{
		{
			name:        "successful start",
			mockDB:      &mockDBClient{flights: []*types.Flight{}},
			expectError: false,
		},
		{
			name:        "database error",
			mockDB:      &mockDBClient{getError: fmt.Errorf("db error")},
			expectError: true,
		},
		{
			name: "start with existing flights",
			mockDB: &mockDBClient{flights: []*types.Flight{
				{SessionID: "session1", HexIdent: "ABC123", Callsign: "TEST123"},
			}},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tracker := NewStateTracker(tt.mockDB, newMockRedisClient())
			err := tracker.Start(context.Background())

			if (err != nil) != tt.expectError {
				t.Errorf("Start() error = %v, expectError %v", err, tt.expectError)
			}
		})
	}
}

func TestStateTracker_ProcessMessage(t *testing.T) {
	tests := []struct {
		name        string
		message     *types.SBSMessage
		setupMocks  func() (*mockDBClient, *mockRedisClient)
		expectError bool
	}{
		{
			name: "valid message processing",
			message: &types.SBSMessage{
				Raw:       "MSG,3,1,1,ABC123,1,2021-01-01,00:00:00.000,2021-01-01,00:00:00.000,TEST123,10000,450,180,40.7128,-74.0060,0,0,0,0,0,0",
				Timestamp: time.Now(),
				Source:    "test-source",
			},
			setupMocks: func() (*mockDBClient, *mockRedisClient) {
				mockDB := &mockDBClient{}
				mockRedis := newMockRedisClient()
				return mockDB, mockRedis
			},
			expectError: false,
		},
		{
			name: "invalid message format",
			message: &types.SBSMessage{
				Raw:       "INVALID,MESSAGE,FORMAT",
				Timestamp: time.Now(),
				Source:    "test-source",
			},
			setupMocks: func() (*mockDBClient, *mockRedisClient) {
				return &mockDBClient{}, newMockRedisClient()
			},
			expectError: true,
		},
		{
			name: "database storage error",
			message: &types.SBSMessage{
				Raw:       "MSG,3,1,1,ABC123,1,2021-01-01,00:00:00.000,2021-01-01,00:00:00.000,TEST123,10000,450,180,40.7128,-74.0060,0,0,0,0,0,0",
				Timestamp: time.Now(),
				Source:    "test-source",
			},
			setupMocks: func() (*mockDBClient, *mockRedisClient) {
				mockDB := &mockDBClient{storeError: fmt.Errorf("db error")}
				mockRedis := newMockRedisClient()
				return mockDB, mockRedis
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockDB, mockRedis := tt.setupMocks()
			tracker := NewStateTracker(mockDB, mockRedis)

			err := tracker.ProcessMessage(tt.message)
			if (err != nil) != tt.expectError {
				t.Errorf("ProcessMessage() error = %v, expectError %v", err, tt.expectError)
			}
		})
	}
}

// sbsAt builds an SBS message as the ingestor publishes it.
func sbsAt(raw string, ts time.Time) *types.SBSMessage {
	return &types.SBSMessage{Raw: raw, Timestamp: ts, Source: "test"}
}

const (
	msgIdent    = "MSG,1,1,1,E49329,1,2026/09/24,10:00:00.000,2026/09/24,10:00:00.000,TAM3456,,,,,,,,,,,0"
	msgPosition = "MSG,3,1,1,E49329,1,2026/09/24,10:00:00.000,2026/09/24,10:00:00.000,,5850,,,-23.43,-46.47,,,0,0,0,0"
	msgVelocity = "MSG,4,1,1,E49329,1,2026/09/24,10:00:00.000,2026/09/24,10:00:00.000,,,233.5,272.5,,,64,,,,,"
	msgOther    = "MSG,5,1,1,E4ABCD,1,2026/09/24,10:00:00.000,2026/09/24,10:00:00.000,,14875,,,,,,,0,,0,0"
)

// fakeGoRedis is an in-memory go-redis backend: GetFlight on a missing key
// goes through the real client code and its redis.Nil handling. The mock
// RedisClient above returned nil for a missing flight, which is what the
// tracker expected, and so hid that the real client returned an empty Flight.
type fakeGoRedis struct{ data map[string]string }

func (f *fakeGoRedis) Ping(ctx context.Context) *goredis.StatusCmd {
	return goredis.NewStatusResult("PONG", nil)
}

func (f *fakeGoRedis) Set(ctx context.Context, key string, value interface{}, _ time.Duration) *goredis.StatusCmd {
	switch v := value.(type) {
	case []byte:
		f.data[key] = string(v)
	case string:
		f.data[key] = v
	}
	return goredis.NewStatusResult("OK", nil)
}

func (f *fakeGoRedis) Get(ctx context.Context, key string) *goredis.StringCmd {
	v, ok := f.data[key]
	if !ok {
		return goredis.NewStringResult("", goredis.Nil)
	}
	return goredis.NewStringResult(v, nil)
}

func (f *fakeGoRedis) Del(ctx context.Context, keys ...string) *goredis.IntCmd {
	var n int64
	for _, k := range keys {
		if _, ok := f.data[k]; ok {
			delete(f.data, k)
			n++
		}
	}
	return goredis.NewIntResult(n, nil)
}

func (f *fakeGoRedis) Close() error { return nil }

func newTrackerWithRealRedisClient(mockDB *mockDBClient) (*StateTracker, *fakeGoRedis) {
	backend := &fakeGoRedis{data: map[string]string{}}
	return NewStateTracker(mockDB, redisclient.NewWithClient(backend)), backend
}

func process(t *testing.T, tracker *StateTracker, raw string, ts time.Time) {
	t.Helper()
	if err := tracker.ProcessMessage(sbsAt(raw, ts)); err != nil {
		t.Fatalf("ProcessMessage(%q): %v", raw, err)
	}
}

// Regression: the flights table was always empty. The tracker asked Redis
// for the aircraft's flight first, the real Redis client returned an empty
// Flight for a missing key, and the tracker "updated" that instead of
// creating one (Created Flights: 0).
func TestFlightIsCreatedForNewAircraft(t *testing.T) {
	mockDB := &mockDBClient{}
	tracker, backend := newTrackerWithRealRedisClient(mockDB)
	t0 := time.Date(2026, 9, 24, 10, 0, 0, 0, time.UTC)

	process(t, tracker, msgIdent, t0)
	process(t, tracker, msgPosition, t0.Add(time.Second))
	process(t, tracker, msgVelocity, t0.Add(2*time.Second))

	if mockDB.creates != 1 || len(mockDB.flights) != 1 {
		t.Fatalf("created %d flights (rows %d), want exactly 1", mockDB.creates, len(mockDB.flights))
	}
	row := mockDB.flights[0]
	if row.SessionID == "" || row.HexIdent != "E49329" || row.Callsign != "TAM3456" || !row.StartedAt.Equal(t0) {
		t.Errorf("unexpected flight row %+v", row)
	}
	if got := tracker.stats.GetStats()["created_flights"]; got != uint64(1) {
		t.Errorf("created_flights = %v, want 1", got)
	}

	f := tracker.activeFlights["E49329"]
	if f == nil || f.SessionID != row.SessionID {
		t.Fatalf("active flight %+v does not match the created row", f)
	}
	// The first message had no position: the first known one is used.
	if f.FirstLatitude != -23.43 || f.FirstLongitude != -46.47 || f.LastLatitude != -23.43 {
		t.Errorf("positions = first(%v,%v) last(%v,%v)", f.FirstLatitude, f.FirstLongitude, f.LastLatitude, f.LastLongitude)
	}
	if f.MaxAltitude != 5850 || f.MaxGroundSpeed != 233.5 || !f.LastSeenAt.Equal(t0.Add(2*time.Second)) || !f.EndedAt.IsZero() {
		t.Errorf("unexpected active flight %+v", f)
	}
	if _, ok := backend.data["flight:E49329"]; !ok {
		t.Error("active flight not cached in Redis")
	}
}

// Running values are written periodically, and the row stays active.
func TestActiveFlightIsFlushedPeriodically(t *testing.T) {
	mockDB := &mockDBClient{}
	tracker, _ := newTrackerWithRealRedisClient(mockDB)
	t0 := time.Date(2026, 9, 24, 10, 0, 0, 0, time.UTC)

	process(t, tracker, msgPosition, t0)
	process(t, tracker, msgVelocity, t0.Add(10*time.Second))
	if mockDB.updates != 0 {
		t.Fatalf("flight written %d times before the flush interval", mockDB.updates)
	}
	process(t, tracker, msgVelocity, t0.Add(flightFlushInterval+time.Second))
	if mockDB.updates != 1 {
		t.Fatalf("flight written %d times after the flush interval, want 1", mockDB.updates)
	}
	row := mockDB.flights[0]
	if !row.EndedAt.IsZero() || row.MaxGroundSpeed != 233.5 || !row.LastSeenAt.Equal(t0.Add(flightFlushInterval+time.Second)) {
		t.Errorf("unexpected flushed row %+v", row)
	}
}

// A flight ends when the aircraft has been silent for longer than the
// timeout, at the time it was last heard, measured on the message clock.
func TestSilentFlightIsEndedBySweep(t *testing.T) {
	mockDB := &mockDBClient{}
	tracker, backend := newTrackerWithRealRedisClient(mockDB)
	t0 := time.Date(2026, 9, 24, 10, 0, 0, 0, time.UTC)

	process(t, tracker, msgPosition, t0)
	process(t, tracker, msgVelocity, t0.Add(30*time.Second))

	// Another aircraft moves the clock, but not past the timeout yet.
	process(t, tracker, msgOther, t0.Add(4*time.Minute))
	if err := tracker.sweepStaleFlights(); err != nil {
		t.Fatal(err)
	}
	if _, ok := tracker.activeFlights["E49329"]; !ok {
		t.Fatal("flight ended before the timeout")
	}

	process(t, tracker, msgOther, t0.Add(30*time.Second+flightTimeout+time.Second))
	if err := tracker.sweepStaleFlights(); err != nil {
		t.Fatal(err)
	}
	if _, ok := tracker.activeFlights["E49329"]; ok {
		t.Fatal("silent flight still active after the timeout")
	}
	var ended *types.Flight
	for _, f := range mockDB.flights {
		if f.HexIdent == "E49329" {
			ended = f
		}
	}
	if ended == nil || !ended.EndedAt.Equal(t0.Add(30*time.Second)) {
		t.Fatalf("flight row %+v, want ended_at = last seen (%v)", ended, t0.Add(30*time.Second))
	}
	if _, ok := backend.data["flight:E49329"]; ok {
		t.Error("ended flight still cached in Redis")
	}
	if _, ok := tracker.states["E49329"]; ok {
		t.Error("state of a gone aircraft still cached")
	}
	if got := tracker.stats.GetStats()["ended_flights"]; got != uint64(1) {
		t.Errorf("ended_flights = %v, want 1", got)
	}
}

// A message after a gap longer than the timeout closes the old flight and
// opens a new session, even if the sweep has not run.
func TestMessageAfterGapStartsNewFlight(t *testing.T) {
	mockDB := &mockDBClient{}
	tracker, _ := newTrackerWithRealRedisClient(mockDB)
	t0 := time.Date(2026, 9, 24, 10, 0, 0, 0, time.UTC)

	process(t, tracker, msgPosition, t0)
	process(t, tracker, msgPosition, t0.Add(flightTimeout+time.Minute))

	if len(mockDB.flights) != 2 {
		t.Fatalf("got %d flights, want 2", len(mockDB.flights))
	}
	first, second := mockDB.flights[0], mockDB.flights[1]
	if !first.EndedAt.Equal(t0) {
		t.Errorf("first flight ended_at = %v, want %v", first.EndedAt, t0)
	}
	if !second.EndedAt.IsZero() || second.SessionID == first.SessionID || !second.StartedAt.Equal(t0.Add(flightTimeout+time.Minute)) {
		t.Errorf("unexpected second flight %+v", second)
	}
}

// The old rule compared the message time with the wall clock, so a tracker
// running behind the stream "ended" a flight on every message. Old messages
// must still build normal flights.
func TestLaggingTrackerStillBuildsFlights(t *testing.T) {
	mockDB := &mockDBClient{}
	tracker, _ := newTrackerWithRealRedisClient(mockDB)
	t0 := time.Now().Add(-2 * time.Hour)

	for i := 0; i < 5; i++ {
		process(t, tracker, msgVelocity, t0.Add(time.Duration(i)*time.Second))
	}
	if mockDB.creates != 1 {
		t.Fatalf("created %d flights, want 1", mockDB.creates)
	}
	if f := tracker.activeFlights["E49329"]; f == nil || !f.EndedAt.IsZero() {
		t.Fatalf("flight should still be active, got %+v", f)
	}
	if got := tracker.stats.GetStats()["ended_flights"]; got != uint64(0) {
		t.Errorf("ended_flights = %v, want 0", got)
	}
}

// After a restart the tracker resumes the active flights from the database
// instead of opening a second session for the same aircraft.
func TestRestartResumesActiveFlightWithoutDuplicate(t *testing.T) {
	mockDB := &mockDBClient{}
	t0 := time.Date(2026, 9, 24, 10, 0, 0, 0, time.UTC)

	first, _ := newTrackerWithRealRedisClient(mockDB)
	process(t, first, msgPosition, t0)
	process(t, first, msgVelocity, t0.Add(20*time.Second))
	if err := first.FlushActiveFlights(); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	second, _ := newTrackerWithRealRedisClient(mockDB)
	if err := second.Start(ctx); err != nil {
		t.Fatal(err)
	}
	process(t, second, msgVelocity, t0.Add(2*time.Minute))

	if mockDB.creates != 1 || len(mockDB.flights) != 1 {
		t.Fatalf("created %d flights after restart, want the resumed one only", mockDB.creates)
	}
	if f := second.activeFlights["E49329"]; f == nil || f.SessionID != mockDB.flights[0].SessionID {
		t.Fatalf("restarted tracker did not resume the session: %+v", f)
	}
}

// A flight that went silent while the tracker was down is ended at its last
// seen time, and the aircraft coming back gets a new session.
func TestRestartEndsFlightThatEndedWhileDown(t *testing.T) {
	t0 := time.Date(2026, 9, 24, 10, 0, 0, 0, time.UTC)
	mockDB := &mockDBClient{flights: []*types.Flight{
		{SessionID: "old", HexIdent: "E49329", StartedAt: t0, LastSeenAt: t0.Add(time.Minute)},
		{SessionID: "gone", HexIdent: "E4FFFF", StartedAt: t0, LastSeenAt: t0.Add(time.Minute)},
	}}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	tracker, _ := newTrackerWithRealRedisClient(mockDB)
	if err := tracker.Start(ctx); err != nil {
		t.Fatal(err)
	}

	process(t, tracker, msgPosition, t0.Add(time.Hour))
	if err := tracker.sweepStaleFlights(); err != nil {
		t.Fatal(err)
	}

	byID := map[string]*types.Flight{}
	for _, f := range mockDB.flights {
		byID[f.SessionID] = f
	}
	for _, id := range []string{"old", "gone"} {
		if !byID[id].EndedAt.Equal(t0.Add(time.Minute)) {
			t.Errorf("flight %s ended_at = %v, want its last seen time", id, byID[id].EndedAt)
		}
	}
	if len(mockDB.flights) != 3 || tracker.activeFlights["E49329"].SessionID == "old" {
		t.Errorf("returning aircraft should get a new session, flights=%d", len(mockDB.flights))
	}
}

// Two active rows for one aircraft (possible before the unique index): the
// latest is resumed and the other one is ended.
func TestStartEndsDuplicateActiveFlights(t *testing.T) {
	t0 := time.Date(2026, 9, 24, 10, 0, 0, 0, time.UTC)
	mockDB := &mockDBClient{flights: []*types.Flight{
		{SessionID: "a", HexIdent: "E49329", StartedAt: t0, LastSeenAt: t0.Add(time.Minute)},
		{SessionID: "b", HexIdent: "E49329", StartedAt: t0, LastSeenAt: t0.Add(2 * time.Minute)},
	}}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	tracker, _ := newTrackerWithRealRedisClient(mockDB)
	if err := tracker.Start(ctx); err != nil {
		t.Fatal(err)
	}
	if tracker.activeFlights["E49329"].SessionID != "b" {
		t.Errorf("resumed %s, want the latest (b)", tracker.activeFlights["E49329"].SessionID)
	}
	if !mockDB.flights[0].EndedAt.Equal(t0.Add(time.Minute)) {
		t.Errorf("duplicate a not ended: %+v", mockDB.flights[0])
	}
}

func TestFlightErrors(t *testing.T) {
	t0 := time.Date(2026, 9, 24, 10, 0, 0, 0, time.UTC)

	t.Run("create error is retried on the next message", func(t *testing.T) {
		mockDB := &mockDBClient{createError: fmt.Errorf("create error")}
		tracker, _ := newTrackerWithRealRedisClient(mockDB)
		if err := tracker.ProcessMessage(sbsAt(msgPosition, t0)); err == nil {
			t.Fatal("expected the create error")
		}
		if len(tracker.activeFlights) != 0 {
			t.Fatal("a flight that was not inserted must not become active")
		}
		mockDB.createError = nil
		process(t, tracker, msgPosition, t0.Add(time.Second))
		if mockDB.creates != 1 {
			t.Fatalf("created %d flights, want 1", mockDB.creates)
		}
	})

	t.Run("missing row is forgotten and recreated", func(t *testing.T) {
		mockDB := &mockDBClient{}
		tracker, _ := newTrackerWithRealRedisClient(mockDB)
		process(t, tracker, msgPosition, t0)
		mockDB.flights = nil // row deleted behind the tracker's back
		err := tracker.ProcessMessage(sbsAt(msgPosition, t0.Add(2*time.Minute)))
		if !errors.Is(err, db.ErrFlightNotFound) {
			t.Fatalf("error = %v, want ErrFlightNotFound", err)
		}
		process(t, tracker, msgPosition, t0.Add(2*time.Minute+time.Second))
		if mockDB.creates != 2 || len(mockDB.flights) != 1 {
			t.Fatalf("creates=%d rows=%d, want a new flight", mockDB.creates, len(mockDB.flights))
		}
	})
}

// The tracker could not keep up (~3 msg/s against ~55 msg/s) because every
// state was its own INSERT and commit. A batch is stored with one call.
func TestProcessBatchStoresStatesTogether(t *testing.T) {
	mockDB := &mockDBClient{}
	tracker, _ := newTrackerWithRealRedisClient(mockDB)
	t0 := time.Date(2026, 9, 24, 10, 0, 0, 0, time.UTC)

	batch := []*types.SBSMessage{
		sbsAt(msgIdent, t0),
		sbsAt("GARBAGE", t0),
		sbsAt(msgPosition, t0.Add(time.Second)),
		sbsAt(msgOther, t0.Add(2*time.Second)),
	}
	if err := tracker.ProcessBatch(batch); err != nil {
		t.Fatalf("ProcessBatch: %v (a bad message must not fail the batch)", err)
	}
	if len(mockDB.stateBatches) != 1 || mockDB.stateBatches[0] != 3 {
		t.Errorf("state writes = %v, want one write of 3 states", mockDB.stateBatches)
	}
	if mockDB.creates != 2 {
		t.Errorf("created %d flights, want 2", mockDB.creates)
	}
	st := tracker.stats.GetStats()
	if st["failed_messages"] != uint64(1) || st["stored_states"] != uint64(3) {
		t.Errorf("failed=%v stored=%v", st["failed_messages"], st["stored_states"])
	}

	// A failed write is reported so the batch is redelivered.
	mockDB.storeError = fmt.Errorf("db down")
	if err := tracker.ProcessBatch([]*types.SBSMessage{sbsAt(msgVelocity, t0.Add(3*time.Second))}); err == nil {
		t.Error("ProcessBatch hid the store error")
	}
}

func TestTrackerMetrics(t *testing.T) {
	mockDB := &mockDBClient{}
	tracker, _ := newTrackerWithRealRedisClient(mockDB)
	reg := metrics.NewRegistry()
	tracker.registerMetrics(reg)
	process(t, tracker, msgPosition, time.Now().Add(-3*time.Second))

	var b strings.Builder
	reg.WriteText(&b)
	out := b.String()
	for _, want := range []string{
		"sbs_tracker_messages_total 1\n",
		"sbs_tracker_flights_created_total 1\n",
		"sbs_tracker_flights_active 1\n",
		"sbs_tracker_states_stored_total 1\n",
		"# TYPE sbs_tracker_flights_ended_total counter\n",
		"sbs_tracker_lag_seconds 3",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("metrics output lacks %q:\n%s", want, out)
		}
	}
}

func TestStateTracker_MergeStates(t *testing.T) {
	tracker := NewStateTracker(&mockDBClient{}, newMockRedisClient())

	tests := []struct {
		name     string
		existing *types.AircraftState
		newState *types.AircraftState
		checkFn  func(*testing.T, *types.AircraftState)
	}{
		{
			name: "merge all fields",
			existing: &types.AircraftState{
				HexIdent:    "ABC123",
				Callsign:    "OLD123",
				Altitude:    10000,
				GroundSpeed: 400,
				Timestamp:   time.Now().Add(-1 * time.Minute),
			},
			newState: &types.AircraftState{
				HexIdent:     "ABC123",
				Callsign:     "NEW123",
				Altitude:     11000,
				Track:        90,
				VerticalRate: 500,
				Squawk:       "7700",
				OnGround:     true,
				Timestamp:    time.Now(),
			},
			checkFn: func(t *testing.T, existing *types.AircraftState) {
				if existing.Callsign != "NEW123" {
					t.Errorf("Expected callsign NEW123, got %s", existing.Callsign)
				}
				if existing.Altitude != 11000 {
					t.Errorf("Expected altitude 11000, got %d", existing.Altitude)
				}
				if existing.Track != 90 {
					t.Errorf("Expected track 90, got %f", existing.Track)
				}
				if existing.VerticalRate != 500 {
					t.Errorf("Expected vertical rate 500, got %d", existing.VerticalRate)
				}
				if existing.Squawk != "7700" {
					t.Errorf("Expected squawk 7700, got %s", existing.Squawk)
				}
				if !existing.OnGround {
					t.Error("Expected OnGround to be true")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tracker.mergeStates(tt.existing, tt.newState)
			tt.checkFn(t, tt.existing)
		})
	}
}

func TestLogStats(t *testing.T) {
	tracker := NewStateTracker(&mockDBClient{}, newMockRedisClient())

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	done := make(chan bool)
	go func() {
		tracker.logStats(ctx)
		done <- true
	}()

	select {
	case <-done:
		// Function returned as expected
	case <-time.After(200 * time.Millisecond):
		t.Error("logStats did not return when context was cancelled")
	}
}

func TestParseEnvironment(t *testing.T) {
	tests := []struct {
		name     string
		envVars  map[string]string
		expected [3]string
	}{
		{
			name:    "default values",
			envVars: map[string]string{},
			expected: [3]string{
				"nats://nats:4222",
				"postgres://sbs:sbs_password@timescaledb:5432/sbs_data?sslmode=disable",
				"redis:6379",
			},
		},
		{
			name: "custom values",
			envVars: map[string]string{
				"NATS_URL":    "nats://custom:4222",
				"DB_CONN_STR": "postgres://custom/db",
				"REDIS_ADDR":  "custom-redis:6379",
			},
			expected: [3]string{
				"nats://custom:4222",
				"postgres://custom/db",
				"custom-redis:6379",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Backup original environment
			original := map[string]string{
				"NATS_URL":    os.Getenv("NATS_URL"),
				"DB_CONN_STR": os.Getenv("DB_CONN_STR"),
				"REDIS_ADDR":  os.Getenv("REDIS_ADDR"),
			}
			defer func() {
				for k, v := range original {
					os.Setenv(k, v)
				}
			}()

			// Set test environment
			for k, v := range tt.envVars {
				os.Setenv(k, v)
			}

			natsURL, dbConnStr, redisAddr := parseEnvironment()
			result := [3]string{natsURL, dbConnStr, redisAddr}

			if result != tt.expected {
				t.Errorf("parseEnvironment() = %v, expected %v", result, tt.expected)
			}
		})
	}
}
