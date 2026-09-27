// Package runtime owns the common process lifecycle for every Eagle service.
package runtime

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	kratosmetrics "github.com/go-kratos/kratos/contrib/otel/v3/metrics"
	"github.com/go-kratos/kratos/contrib/otel/v3/tracing"
	"github.com/go-kratos/kratos/v3"
	kratosconfig "github.com/go-kratos/kratos/v3/config"
	"github.com/go-kratos/kratos/v3/config/env"
	"github.com/go-kratos/kratos/v3/config/file"
	"github.com/go-kratos/kratos/v3/log"
	"github.com/go-kratos/kratos/v3/transport"

	"github.com/eagle-go/eagle/pkg/healthx"
	"github.com/eagle-go/eagle/pkg/otelx"
	"github.com/eagle-go/eagle/pkg/platform/config"

	_ "go.uber.org/automaxprocs"
)

type Components struct {
	Servers []transport.Server
	Cleanup func()
}

type Builder func(*config.Bootstrap, *slog.Logger) (Components, error)

type Spec struct {
	Name         string
	Version      string
	Requirements config.Requirements
	Build        Builder
}

// Run loads configuration, initializes observability, builds one service and
// blocks until its transports stop.
func Run(spec Spec) {
	if err := run(spec); err != nil {
		panic(err)
	}
}

func run(spec Spec) error {
	if spec.Name == "" || spec.Build == nil {
		return fmt.Errorf("runtime: service name and builder are required")
	}
	flags := flag.NewFlagSet(spec.Name, flag.ContinueOnError)
	confPath := flags.String("conf", "configs", "配置文件目录或文件路径")
	if err := flags.Parse(os.Args[1:]); err != nil {
		return err
	}

	c := kratosconfig.New(
		kratosconfig.WithSource(file.NewSource(*confPath), env.NewSource("EAGLE")),
		kratosconfig.WithResolveActualTypes(true),
	)
	defer func() { _ = c.Close() }()
	if err := c.Load(); err != nil {
		return err
	}

	var bc config.Bootstrap
	if err := c.Scan(&bc); err != nil {
		return err
	}
	if err := config.Validate(&bc, spec.Requirements); err != nil {
		return err
	}

	instanceID, _ := os.Hostname()
	logger := newLogger(spec, instanceID, bc.GetObservability())
	log.SetDefault(logger)

	shutdownOtel, err := setupObservability(spec, instanceID, bc.GetObservability())
	if err != nil {
		return err
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := shutdownOtel(ctx); err != nil {
			logger.Warn("关闭可观测性组件时出错", slog.Any("error", err))
		}
	}()

	components, err := spec.Build(&bc, logger)
	if err != nil {
		return err
	}
	if components.Cleanup != nil {
		defer components.Cleanup()
	}

	app := kratos.New(
		kratos.ID(instanceID),
		kratos.Name(spec.Name),
		kratos.Version(spec.Version),
		kratos.Logger(logger),
		kratos.Server(components.Servers...),
	)
	healthx.Default.MarkInitialized()
	return app.Run()
}

func setupObservability(spec Spec, instanceID string, o *config.Observability) (func(context.Context) error, error) {
	return otelx.Setup(context.Background(), otelx.Config{
		ServiceName:    spec.Name,
		ServiceVersion: spec.Version,
		InstanceID:     instanceID,
		OTLPEndpoint:   o.GetOtlpEndpoint(),
		OTLPInsecure:   o.GetOtlpInsecure(),
		SampleRatio:    o.GetTraceSampleRatio(),
		MetricsAddr:    o.GetMetricsAddr(),
		HistogramViews: []string{
			kratosmetrics.DefaultServerSecondsHistogramName,
			kratosmetrics.DefaultClientSecondsHistogramName,
		},
	})
}

func newLogger(spec Spec, instanceID string, o *config.Observability) *slog.Logger {
	handler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		AddSource: true,
		Level:     parseLevel(o.GetLogLevel()),
	})
	return log.NewLogger(handler, log.WithExtractor(tracing.TraceAttrs)).With(
		slog.String("service.id", instanceID),
		slog.String("service.name", spec.Name),
		slog.String("service.version", spec.Version),
	)
}

func parseLevel(value string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
