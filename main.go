package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"net/netip"
	"os"
	"os/signal"
	"syscall"
	"time"
)

const version = "2.0.0"

var logger = slog.New(
	slog.NewTextHandler(
		os.Stdout,
		&slog.HandlerOptions{
			Level: slog.LevelInfo,
		},
	),
)

type App struct {
	config Config

	cloudflare *CloudflareClient

	ipv4Client *http.Client
	ipv6Client *http.Client
}

func newApp(cfg Config) *App {
	return &App{
		config: cfg,

		cloudflare: newCloudflareClient(
			os.Getenv("CLOUDFLARE_API_TOKEN"),
		),

		ipv4Client: newIPClient(4),
		ipv6Client: newIPClient(6),
	}
}

func (a *App) sync(ctx context.Context) error {
	var ipv4 netip.Addr
	var ipv6 netip.Addr

	var errs []error

	if *a.config.IPv4 {
		ip, err := detectPublicIP(
			ctx,
			a.ipv4Client,
			4,
		)

		if err != nil {
			logger.Warn(
				"IPv4 detection failed",
				"error", err,
			)
			errs = append(errs, fmt.Errorf("IPv4: %w", err))
		} else {
			ipv4 = ip
			logger.Debug("IPv4 detected", "ip", ip.String())
		}
	}

	if *a.config.IPv6 && ctx.Err() == nil {
		ip, err := detectPublicIP(
			ctx,
			a.ipv6Client,
			6,
		)

		if err != nil {
			logger.Warn(
				"IPv6 detection failed",
				"error", err,
			)
			errs = append(errs, fmt.Errorf("IPv6: %w", err))
		} else {
			ipv6 = ip
			logger.Debug("IPv6 detected", "ip", ip.String())
		}
	}

	if ctx.Err() != nil {
		return ctx.Err()
	}

	for _, zone := range a.config.Zones {
		for _, name := range zone.Records {
			if ctx.Err() != nil {
				return ctx.Err()
			}

			if ipv4.IsValid() {
				if err := a.cloudflare.reconcile(
					ctx,
					zone.ID,
					name,
					"A",
					ipv4,
				); err != nil {
					logger.Error(
						"DNS sync failed",
						"name", name,
						"type", "A",
						"error", err,
					)

					errs = append(errs, err)
				}
			}

			if ipv6.IsValid() && ctx.Err() == nil {
				if err := a.cloudflare.reconcile(
					ctx,
					zone.ID,
					name,
					"AAAA",
					ipv6,
				); err != nil {
					logger.Error(
						"DNS sync failed",
						"name", name,
						"type", "AAAA",
						"error", err,
					)

					errs = append(errs, err)
				}
			}
		}
	}

	return errors.Join(errs...)
}

func (a *App) run(ctx context.Context) {
	interval, _ := time.ParseDuration(a.config.Interval)

	logger.Info(
		"myddns started",
		"version", version,
		"interval", interval.String(),
	)

	for {
		if ctx.Err() != nil {
			break
		}

		err := a.sync(ctx)

		if err != nil && ctx.Err() == nil {
			logger.Warn(
				"sync cycle completed with errors",
				"error", err,
			)
		}

		timer := time.NewTimer(interval)

		select {
		case <-ctx.Done():
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}

			logger.Info("myddns stopped")
			return

		case <-timer.C:
		}
	}

	logger.Info("myddns stopped")
}

func main() {
	configPath := flag.String(
		"config",
		"config.json",
		"configuration file",
	)

	once := flag.Bool(
		"once",
		false,
		"run once and exit",
	)

	showVersion := flag.Bool(
		"version",
		false,
		"show version",
	)

	flag.Parse()

	if *showVersion {
		fmt.Println("myddns", version)
		return
	}

	cfg, err := LoadConfig(*configPath)
	if err != nil {
		logger.Error(
			"failed to load configuration",
			"error", err,
		)
		os.Exit(1)
	}

	app := newApp(cfg)

	ctx, stop := signal.NotifyContext(
		context.Background(),
		os.Interrupt,
		syscall.SIGTERM,
	)
	defer stop()

	if *once {
		err := app.sync(ctx)

		if err != nil {
			logger.Error(
				"sync failed",
				"error", err,
			)
			os.Exit(1)
		}

		logger.Info("sync completed")
		return
	}

	app.run(ctx)
}
