// Package telemetry configures OpenTelemetry for JGO applications.
package telemetry

import "go.opentelemetry.io/otel/propagation"

// Propagator returns JGO's standard W3C trace-context and baggage propagator.
func Propagator() propagation.TextMapPropagator {
	return propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	)
}
