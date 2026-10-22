// Package observability configures optional OpenTelemetry providers.
package observability

import (
	"context"
	"os"
	"strings"
	"time"

	otelruntime "go.opentelemetry.io/contrib/instrumentation/runtime"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	"go.opentelemetry.io/otel/sdk/trace"
)

// Init configures bounded OTLP exporters. An empty endpoint intentionally keeps no-op providers.
func Init(ctx context.Context, service string) (func(context.Context) error, error) {
	endpoint := os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT")
	endpoint = strings.TrimPrefix(strings.TrimPrefix(endpoint, "http://"), "https://")
	r, err := resource.New(ctx, resource.WithAttributes(
		attribute.String("service.name", service),
		attribute.String("deployment.environment", env("OTEL_DEPLOYMENT_ENVIRONMENT", "development")),
	))
	if err != nil {
		return nil, err
	}
	traceOptions := []trace.TracerProviderOption{trace.WithResource(r), trace.WithSampler(trace.ParentBased(trace.AlwaysSample()))}
	metricOptions := []metric.Option{metric.WithResource(r)}
	if endpoint != "" {
		tx, err := otlptracegrpc.New(ctx, otlptracegrpc.WithEndpoint(endpoint), otlptracegrpc.WithInsecure(), otlptracegrpc.WithTimeout(3*time.Second))
		if err != nil {
			return nil, err
		}
		metricExporter, err := otlpmetricgrpc.New(ctx, otlpmetricgrpc.WithEndpoint(endpoint), otlpmetricgrpc.WithInsecure(), otlpmetricgrpc.WithTimeout(3*time.Second))
		if err != nil {
			return nil, err
		}
		traceOptions = append(traceOptions, trace.WithBatcher(tx, trace.WithBatchTimeout(2*time.Second), trace.WithMaxExportBatchSize(256)))
		metricOptions = append(metricOptions, metric.WithReader(metric.NewPeriodicReader(metricExporter, metric.WithInterval(15*time.Second))))
	}
	tp := trace.NewTracerProvider(traceOptions...)
	mp := metric.NewMeterProvider(metricOptions...)
	otel.SetTracerProvider(tp)
	otel.SetMeterProvider(mp)
	otel.SetTextMapPropagator(propagation.TraceContext{})
	if endpoint != "" {
		_ = otelruntime.Start(otelruntime.WithMeterProvider(mp))
	}
	return func(shutdownCtx context.Context) error {
		traceErr := tp.Shutdown(shutdownCtx)
		metricErr := mp.Shutdown(shutdownCtx)
		if traceErr != nil {
			return traceErr
		}
		return metricErr
	}, nil
}

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
