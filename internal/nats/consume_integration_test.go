package nats

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/saviobatista/sbs-logger/internal/types"
)

// collector gathers the raw messages handed to a Consume handler.
type collector struct {
	mu   sync.Mutex
	raws []string
	fail int // fail this many calls before accepting
}

func (c *collector) handle(batch []*types.SBSMessage) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.fail > 0 {
		c.fail--
		return errors.New("temporary failure")
	}
	for _, m := range batch {
		c.raws = append(c.raws, m.Raw)
	}
	return nil
}

func (c *collector) snapshot() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.raws...)
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func publish(t *testing.T, c *Client, prefix string, n int) {
	t.Helper()
	for i := 0; i < n; i++ {
		if err := c.PublishSBSMessage(&types.SBSMessage{Raw: fmt.Sprintf("%s-%d", prefix, i), Timestamp: time.Now()}); err != nil {
			t.Fatal(err)
		}
	}
}

// The durable consumer starts at "now", resumes after a restart with what
// was published while the service was down, and redelivers a failed batch.
func TestConsumeDurable_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}
	containers := setupTestContainers(t)
	defer func() { _ = containers.nats.Terminate(context.Background()) }()
	url, err := containers.nats.ConnectionString(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	pub, err := New(url)
	if err != nil {
		t.Fatal(err)
	}
	defer pub.Close()
	publish(t, pub, "before", 5) // published before the consumer exists: not delivered

	opts := ConsumeOptions{Durable: ConsumerLogger, Batch: 10, MaxWait: 200 * time.Millisecond}
	start := func(c *collector) (*Client, context.CancelFunc, chan error) {
		client, err := New(url)
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan error, 1)
		go func() { done <- client.Consume(ctx, opts, c.handle) }()
		return client, cancel, done
	}

	first := &collector{fail: 1} // the first batch fails and must come back
	client, cancel, done := start(first)
	// Let the consumer be created before publishing.
	waitFor(t, "consumer", func() bool {
		_, err := pub.js.ConsumerInfo("SBS_RAW", ConsumerLogger)
		return err == nil
	})
	publish(t, pub, "live", 25)
	waitFor(t, "25 live messages", func() bool { return len(first.snapshot()) >= 25 })
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	client.Close()
	for _, raw := range first.snapshot() {
		if raw[:6] == "before" {
			t.Fatalf("got %q, published before the consumer existed", raw)
		}
	}

	publish(t, pub, "down", 7) // the service is down

	second := &collector{}
	client, cancel, done = start(second)
	defer client.Close()
	waitFor(t, "messages published while down", func() bool { return len(second.snapshot()) >= 7 })
	time.Sleep(500 * time.Millisecond) // nothing else must arrive
	cancel()
	<-done
	got := second.snapshot()
	if len(got) != 7 || got[0] != "down-0" || got[6] != "down-6" {
		t.Errorf("after restart got %v, want down-0..down-6 only", got)
	}

	info, err := pub.js.ConsumerInfo("SBS_RAW", ConsumerLogger)
	if err != nil {
		t.Fatal(err)
	}
	if info.Config.Durable != ConsumerLogger || info.NumPending != 0 || info.NumAckPending != 0 {
		t.Errorf("consumer state: durable=%q pending=%d ack_pending=%d", info.Config.Durable, info.NumPending, info.NumAckPending)
	}
}
