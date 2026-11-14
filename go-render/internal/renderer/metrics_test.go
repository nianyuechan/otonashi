package renderer

import (
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"go-render/pkg/render/api"
)

func TestMetricsObserveUsesAPIErrorCode(t *testing.T) {
	registry := prometheus.NewRegistry()
	metrics := NewMetrics(registry)
	metrics.observe(time.Now(), &api.Error{Code: api.CodeTimeout, Message: "deadline"})

	families, err := registry.Gather()
	if err != nil {
		t.Fatalf("Gather() error = %v", err)
	}
	for _, family := range families {
		if family.GetName() != "render_requests_total" {
			continue
		}
		for _, metric := range family.Metric {
			for _, label := range metric.Label {
				if label.GetName() == "result" && label.GetValue() == api.CodeTimeout && metric.Counter.GetValue() == 1 {
					return
				}
			}
		}
	}
	t.Fatalf("timeout metric not found in %+v", families)
}
