package skalpai

import (
	"context"
	"testing"

	"go.opentelemetry.io/otel/sdk/resource"
	semconv "go.opentelemetry.io/otel/semconv/v1.24.0"
)

// Two replicas of one service report an identical resource unless the
// deployment supplies a per-instance attribute. A consumer grouping points
// into series then merges both replicas, and their independent cumulative
// counters read as one counter that keeps resetting.
func TestResourceCarriesInstanceIDFromEnv(t *testing.T) {
	t.Setenv("OTEL_RESOURCE_ATTRIBUTES", "service.instance.id=core-abc12")

	res, err := resource.New(context.Background(),
		resource.WithFromEnv(),
		resource.WithAttributes(
			semconv.ServiceName("synthiz-core"),
		),
	)
	if err != nil {
		t.Fatalf("build resource: %v", err)
	}

	var instanceID string
	for _, attr := range res.Attributes() {
		if string(attr.Key) == "service.instance.id" {
			instanceID = attr.Value.AsString()
		}
	}
	if instanceID != "core-abc12" {
		t.Fatalf("service.instance.id = %q, want %q", instanceID, "core-abc12")
	}
}

// The explicit attributes win over the environment, so a stray
// OTEL_SERVICE_NAME cannot rename a service behind the caller's back.
func TestResourceAttributesOverrideEnvServiceName(t *testing.T) {
	t.Setenv("OTEL_RESOURCE_ATTRIBUTES", "service.name=from-env")

	res, err := resource.New(context.Background(),
		resource.WithFromEnv(),
		resource.WithAttributes(
			semconv.ServiceName("synthiz-core"),
		),
	)
	if err != nil {
		t.Fatalf("build resource: %v", err)
	}

	var serviceName string
	for _, attr := range res.Attributes() {
		if string(attr.Key) == "service.name" {
			serviceName = attr.Value.AsString()
		}
	}
	if serviceName != "synthiz-core" {
		t.Fatalf("service.name = %q, want %q", serviceName, "synthiz-core")
	}
}
