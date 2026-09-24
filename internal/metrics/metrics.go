// Package metrics is a minimal Prometheus exporter: counters and gauges with
// at most one label, served in the text exposition format (version 0.0.4).
// It avoids a dependency on the Prometheus client for a handful of series.
package metrics

import (
	"context"
	"errors"
	"fmt"
	"log"
	"math"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type kind string

const (
	counterKind kind = "counter"
	gaugeKind   kind = "gauge"
)

// Registry holds the metrics of one process.
type Registry struct {
	mu      sync.Mutex
	metrics []*family
	byName  map[string]*family
}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry {
	return &Registry{byName: make(map[string]*family)}
}

type family struct {
	name   string
	help   string
	kind   kind
	label  string // empty for an unlabeled metric
	mu     sync.Mutex
	series map[string]*uint64 // label value -> float64 bits
	fn     func() float64     // gauge computed at scrape time
}

func (r *Registry) register(name, help string, k kind, label string) *family {
	r.mu.Lock()
	defer r.mu.Unlock()
	if f, ok := r.byName[name]; ok {
		return f
	}
	f := &family{name: name, help: help, kind: k, label: label, series: make(map[string]*uint64)}
	r.metrics = append(r.metrics, f)
	r.byName[name] = f
	return f
}

func (f *family) get(labelValue string) *uint64 {
	f.mu.Lock()
	defer f.mu.Unlock()
	v, ok := f.series[labelValue]
	if !ok {
		v = new(uint64)
		f.series[labelValue] = v
	}
	return v
}

func add(p *uint64, delta float64) {
	for {
		old := atomic.LoadUint64(p)
		n := math.Float64bits(math.Float64frombits(old) + delta)
		if atomic.CompareAndSwapUint64(p, old, n) {
			return
		}
	}
}

// Counter is a monotonically increasing value.
type Counter struct{ v *uint64 }

// Inc adds one.
func (c Counter) Inc() { add(c.v, 1) }

// Add adds a non-negative delta.
func (c Counter) Add(delta float64) {
	if delta > 0 {
		add(c.v, delta)
	}
}

// Value returns the current value.
func (c Counter) Value() float64 { return math.Float64frombits(atomic.LoadUint64(c.v)) }

// Gauge is a value that can go up and down.
type Gauge struct{ v *uint64 }

// Set sets the value.
func (g Gauge) Set(v float64) { atomic.StoreUint64(g.v, math.Float64bits(v)) }

// Value returns the current value.
func (g Gauge) Value() float64 { return math.Float64frombits(atomic.LoadUint64(g.v)) }

// Counter registers (or returns) an unlabeled counter.
func (r *Registry) Counter(name, help string) Counter {
	return Counter{r.register(name, help, counterKind, "").get("")}
}

// Gauge registers (or returns) an unlabeled gauge.
func (r *Registry) Gauge(name, help string) Gauge {
	return Gauge{r.register(name, help, gaugeKind, "").get("")}
}

// GaugeFunc registers a gauge whose value is computed at scrape time.
func (r *Registry) GaugeFunc(name, help string, fn func() float64) {
	f := r.register(name, help, gaugeKind, "")
	f.mu.Lock()
	f.fn = fn
	f.mu.Unlock()
}

// CounterFunc registers a counter whose value is read at scrape time from a
// value the program already counts.
func (r *Registry) CounterFunc(name, help string, fn func() float64) {
	f := r.register(name, help, counterKind, "")
	f.mu.Lock()
	f.fn = fn
	f.mu.Unlock()
}

// CounterVec is a counter with one label.
type CounterVec struct{ f *family }

// CounterVec registers a counter with one label.
func (r *Registry) CounterVec(name, help, label string) CounterVec {
	return CounterVec{r.register(name, help, counterKind, label)}
}

// With returns the counter for a label value.
func (v CounterVec) With(labelValue string) Counter { return Counter{v.f.get(labelValue)} }

// GaugeVec is a gauge with one label.
type GaugeVec struct{ f *family }

// GaugeVec registers a gauge with one label.
func (r *Registry) GaugeVec(name, help, label string) GaugeVec {
	return GaugeVec{r.register(name, help, gaugeKind, label)}
}

// With returns the gauge for a label value.
func (v GaugeVec) With(labelValue string) Gauge { return Gauge{v.f.get(labelValue)} }

var labelEscaper = strings.NewReplacer(`\`, `\\`, "\n", `\n`, `"`, `\"`)

func formatValue(v float64) string {
	switch {
	case math.IsInf(v, 1):
		return "+Inf"
	case math.IsInf(v, -1):
		return "-Inf"
	case math.IsNaN(v):
		return "NaN"
	}
	return strconv.FormatFloat(v, 'g', -1, 64)
}

// WriteText writes every metric in the Prometheus text format.
func (r *Registry) WriteText(w *strings.Builder) {
	r.mu.Lock()
	families := append([]*family(nil), r.metrics...)
	r.mu.Unlock()
	sort.Slice(families, func(i, j int) bool { return families[i].name < families[j].name })

	for _, f := range families {
		fmt.Fprintf(w, "# HELP %s %s\n# TYPE %s %s\n", f.name, f.help, f.name, f.kind)
		f.mu.Lock()
		if f.fn != nil {
			fmt.Fprintf(w, "%s %s\n", f.name, formatValue(f.fn()))
			f.mu.Unlock()
			continue
		}
		keys := make([]string, 0, len(f.series))
		for k := range f.series {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			v := formatValue(math.Float64frombits(atomic.LoadUint64(f.series[k])))
			if f.label == "" {
				fmt.Fprintf(w, "%s %s\n", f.name, v)
			} else {
				fmt.Fprintf(w, "%s{%s=\"%s\"} %s\n", f.name, f.label, labelEscaper.Replace(k), v)
			}
		}
		f.mu.Unlock()
	}
}

// Handler serves the registry on GET.
func (r *Registry) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		var b strings.Builder
		r.WriteText(&b)
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		_, _ = w.Write([]byte(b.String()))
	})
}

// Serve starts an HTTP server with /metrics on addr and stops it when ctx is
// done. An empty addr disables it. Errors are logged, not fatal: metrics must
// not take the service down.
func (r *Registry) Serve(ctx context.Context, addr string) {
	if addr == "" {
		return
	}
	mux := http.NewServeMux()
	mux.Handle("/metrics", r.Handler())
	srv := &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()
	go func() {
		log.Printf("Serving metrics on %s/metrics", addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("Metrics server stopped: %v", err)
		}
	}()
}

// Addr returns the metrics listen address: $METRICS_ADDR if set (an empty
// value disables the endpoint), else ":" + defaultPort.
func Addr(defaultPort string) string {
	if v, ok := lookupEnv("METRICS_ADDR"); ok {
		return v
	}
	return ":" + defaultPort
}

// lookupEnv is os.LookupEnv, replaceable in tests.
var lookupEnv = os.LookupEnv
