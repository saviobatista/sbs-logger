package metrics

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestWriteText(t *testing.T) {
	r := NewRegistry()
	c := r.Counter("app_messages_total", "Messages.")
	c.Inc()
	c.Add(2)
	c.Add(-5) // ignored: counters only go up
	g := r.Gauge("app_active", "Active things.")
	g.Set(1.5)
	v := r.CounterVec("app_bytes_total", "Bytes per source.", "source")
	v.With("10.0.0.1:30003").Add(100)
	v.With(`we"ird`).Inc()
	r.GaugeFunc("app_lag_seconds", "Lag.", func() float64 { return 0.25 })
	r.CounterFunc("app_counted_total", "Counted elsewhere.", func() float64 { return 7 })

	var b strings.Builder
	r.WriteText(&b)
	want := `# HELP app_active Active things.
# TYPE app_active gauge
app_active 1.5
# HELP app_bytes_total Bytes per source.
# TYPE app_bytes_total counter
app_bytes_total{source="10.0.0.1:30003"} 100
app_bytes_total{source="we\"ird"} 1
# HELP app_counted_total Counted elsewhere.
# TYPE app_counted_total counter
app_counted_total 7
# HELP app_lag_seconds Lag.
# TYPE app_lag_seconds gauge
app_lag_seconds 0.25
# HELP app_messages_total Messages.
# TYPE app_messages_total counter
app_messages_total 3
`
	if b.String() != want {
		t.Errorf("got:\n%s\nwant:\n%s", b.String(), want)
	}
	if c.Value() != 3 || g.Value() != 1.5 {
		t.Errorf("values: counter %v gauge %v", c.Value(), g.Value())
	}
	// Registering the same name returns the same series.
	if r.Counter("app_messages_total", "Messages.").Value() != 3 {
		t.Error("re-registration returned a new counter")
	}
}

func TestHandlerAndServe(t *testing.T) {
	r := NewRegistry()
	r.Counter("x_total", "X.").Inc()

	rec := httptest.NewRecorder()
	r.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/plain; version=0.0.4") {
		t.Errorf("content type %q", ct)
	}
	if !strings.Contains(rec.Body.String(), "x_total 1\n") {
		t.Errorf("body %q", rec.Body.String())
	}

	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := l.Addr().String()
	_ = l.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r.Serve(ctx, addr)
	var body []byte
	for i := 0; i < 50; i++ {
		resp, err := http.Get("http://" + addr + "/metrics")
		if err == nil {
			body, _ = io.ReadAll(resp.Body)
			_ = resp.Body.Close()
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !strings.Contains(string(body), "x_total 1") {
		t.Errorf("served body %q", body)
	}
}

func TestAddr(t *testing.T) {
	defer func(f func(string) (string, bool)) { lookupEnv = f }(lookupEnv)
	lookupEnv = func(string) (string, bool) { return "", false }
	if got := Addr("9101"); got != ":9101" {
		t.Errorf("default addr %q", got)
	}
	lookupEnv = func(string) (string, bool) { return "127.0.0.1:1234", true }
	if got := Addr("9101"); got != "127.0.0.1:1234" {
		t.Errorf("env addr %q", got)
	}
	lookupEnv = func(string) (string, bool) { return "", true }
	if got := Addr("9101"); got != "" {
		t.Errorf("disabled addr %q", got)
	}
}
