package renderer

import (
	"errors"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/nianyuechan/otonashi/go-render/pkg/render/api"
)

type Metrics struct {
	active        prometheus.Gauge
	queue         prometheus.Gauge
	duration      *prometheus.HistogramVec
	requests      *prometheus.CounterVec
	browserStarts prometheus.Counter
}

func NewMetrics(registerer prometheus.Registerer) *Metrics {
	m := &Metrics{
		active: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "render_active_requests",
			Help: "Number of renders currently executing on pages.",
		}),
		queue: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "render_queue_length",
			Help: "Number of render requests waiting for a page.",
		}),
		duration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "render_duration_seconds",
			Help:    "End-to-end render request duration, including queueing.",
			Buckets: []float64{0.05, 0.1, 0.25, 0.5, 1, 1.6, 2.5, 4, 8, 15, 30},
		}, []string{"result"}),
		requests: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "render_requests_total",
			Help: "Total render requests by result.",
		}, []string{"result"}),
		browserStarts: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "render_browser_starts_total",
			Help: "Total Chromium process starts, including crash recovery.",
		}),
	}
	registerer.MustRegister(m.active, m.queue, m.duration, m.requests, m.browserStarts)
	return m
}

func (m *Metrics) observe(start time.Time, err error) {
	result := "success"
	if err != nil {
		result = api.CodeInternal
		var apiErr *api.Error
		if errors.As(err, &apiErr) {
			result = apiErr.Code
		}
	}
	m.requests.WithLabelValues(result).Inc()
	m.duration.WithLabelValues(result).Observe(time.Since(start).Seconds())
}
