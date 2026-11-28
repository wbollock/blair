package telemetry

import (
	"context"
	"sync"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// Attribute values must be small fixed sets (event names, tool names, outcomes): never ids,
// titles, emails or tokens. See docs/OBSERVABILITY.md for the list.
var (
	meter       = otel.Meter("github.com/wbollock/blair")
	counters    sync.Map
	histograms  sync.Map
	instrumentM sync.Mutex
)

// Count adds one to a counter, creating it on first use. It is a no-op until a provider is set.
func Count(ctx context.Context, name string, attrs ...attribute.KeyValue) {
	c, ok := counters.Load(name)
	if !ok {
		instrumentM.Lock()
		if c, ok = counters.Load(name); !ok {
			ic, err := meter.Int64Counter(name)
			if err != nil {
				instrumentM.Unlock()
				return
			}
			c = ic
			counters.Store(name, c)
		}
		instrumentM.Unlock()
	}
	c.(metric.Int64Counter).Add(ctx, 1, metric.WithAttributes(attrs...))
}

// ObserveMs records a duration in milliseconds in a histogram.
func ObserveMs(ctx context.Context, name string, ms float64, attrs ...attribute.KeyValue) {
	h, ok := histograms.Load(name)
	if !ok {
		instrumentM.Lock()
		if h, ok = histograms.Load(name); !ok {
			ih, err := meter.Float64Histogram(name, metric.WithUnit("ms"))
			if err != nil {
				instrumentM.Unlock()
				return
			}
			h = ih
			histograms.Store(name, h)
		}
		instrumentM.Unlock()
	}
	h.(metric.Float64Histogram).Record(ctx, ms, metric.WithAttributes(attrs...))
}
