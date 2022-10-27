package otelx

import (
	"go.opentelemetry.io/otel/trace"

	"github.com/ory/x/logrusx"
	"github.com/ory/x/stringsx"
)

// Tracer wraps an OpenTelemetry tracer.
// DEPRECATED: use a trace.Tracer directly instead.
type Tracer struct {
	t trace.Tracer
}

// Wrap wraps an OpenTelemetry Tracer. For migration only, don't use in new code.
func Wrap(t trace.Tracer) *Tracer {
	if t == nil {
		return nil
	}
	return &Tracer{t}
}

// Tracer returns the wrapped OpenTelemetry tracer.
func (t *Tracer) Tracer() trace.Tracer {
	if t == nil {
		return nil
	}
	return t.t
}

// IsLoaded returns true if the tracer has been loaded.
func (t *Tracer) IsLoaded() bool {
	return t == nil || t.t == nil
}

// NewNoop creates a new no-op tracer.
// DEPRECATED: Use NewNoopOTLP instead
func NewNoop(_ *logrusx.Logger, _ *Config) *Tracer {
	return Wrap(NewNoopOTLP())
}

// NewNoopOTLP creates a new no-op tracer.
func NewNoopOTLP() trace.Tracer {
	return trace.NewNoopTracerProvider().Tracer("")
}

// New creates a new tracer. If name is empty, a default tracer name is used
// instead. See: https://godocs.io/go.opentelemetry.io/otel/sdk/trace#TracerProvider.Tracer
// DEPRECATED: use NewFromConfig instead.
func New(name string, l *logrusx.Logger, c *Config) (*Tracer, error) {
	t, err := NewFromConfig(name, l, c)
	return Wrap(t), err
}

// NewFromConfig creates a new tracer. If name is empty, a default tracer name is used
// instead. See: https://godocs.io/go.opentelemetry.io/otel/sdk/trace#TracerProvider.Tracer
func NewFromConfig(name string, l *logrusx.Logger, c *Config) (trace.Tracer, error) {
	switch f := stringsx.SwitchExact(c.Provider); {
	case f.AddCase("jaeger"):
		tracer, err := SetupJaeger(name, c)
		if err != nil {
			return nil, err
		}
		l.Infof("Jaeger tracer configured! Sending spans to %s", c.Providers.Jaeger.LocalAgentAddress)
		return tracer, nil
	case f.AddCase("zipkin"):
		tracer, err := SetupZipkin(name, c)
		if err != nil {
			return nil, err
		}
		l.Infof("Zipkin tracer configured! Sending spans to %s", c.Providers.Zipkin.ServerURL)
		return tracer, nil
	case f.AddCase("otel"):
		tracer, err := SetupOTLP(name, c)
		if err != nil {
			return nil, err
		}
		l.Infof("OTLP tracer configured! Sending spans to %s", c.Providers.OTLP.ServerURL)
		return tracer, nil
	case f.AddCase(""):
		l.Infof("No tracer configured - skipping tracing setup")
		return trace.NewNoopTracerProvider().Tracer(name), nil
	default:
		return nil, f.ToUnknownCaseErr()
	}
}
