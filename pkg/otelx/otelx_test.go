package otelx

import (
	"slices"
	"testing"

	metricsdk "go.opentelemetry.io/otel/sdk/metric"
)

func TestSecondsHistogramViewCoversSLOThreshold(t *testing.T) {
	stream, ok := secondsHistogramView("server_requests_seconds")(metricsdk.Instrument{Name: "server_requests_seconds"})
	if !ok {
		t.Fatal("request duration histogram view did not match")
	}
	aggregation, ok := stream.Aggregation.(metricsdk.AggregationExplicitBucketHistogram)
	if !ok {
		t.Fatalf("aggregation type = %T", stream.Aggregation)
	}
	if !slices.Contains(aggregation.Boundaries, 2.0) {
		t.Fatalf("histogram boundaries %v do not cover the 2s latency SLO", aggregation.Boundaries)
	}
}
