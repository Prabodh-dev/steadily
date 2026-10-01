package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"steadily/internal/config"
	"steadily/internal/metrics"
	"steadily/internal/proxy"
)

func main() {
	configPath := flag.String("config", "", "path to config file")
	flag.Parse()

	cfg, err := config.Load(*configPath)
	if err != nil {
		slog.Error("failed to load configuration", "error", err)
		os.Exit(1)
	}

	slog.Info("starting steadily load balancer", "mode", cfg.Mode, "algorithm", cfg.Algorithm, "listen_address", cfg.ListenAddress)

	pool, err := proxy.NewPool(cfg)
	if err != nil {
		slog.Error("failed to create pool", "error", err)
		os.Exit(1)
	}

	mainCtx, mainCancel := context.WithCancel(context.Background())
	defer mainCancel()

	pool.Start(mainCtx)
	defer pool.Stop()

	watcher := config.NewWatcher(*configPath, func(newCfg *config.Config) {
		slog.Info("applying reloaded configuration")
		if err := pool.UpdateConfig(newCfg); err != nil {
			slog.Error("failed to apply reloaded configuration", "error", err)
		}
	})
	if err := watcher.Start(mainCtx); err != nil {
		slog.Warn("config watcher start failed", "error", err)
	}

	sighupChan := make(chan os.Signal, 1)
	signal.Notify(sighupChan, syscall.SIGHUP)
	go func() {
		for {
			select {
			case <-mainCtx.Done():
				return
			case <-sighupChan:
				slog.Info("received SIGHUP, reloading configuration")
				newCfg, reloadErr := config.Load(*configPath)
				if reloadErr != nil {
					slog.Error("failed to reload configuration on SIGHUP", "error", reloadErr)
				} else if applyErr := pool.UpdateConfig(newCfg); applyErr != nil {
					slog.Error("failed to apply reloaded configuration on SIGHUP", "error", applyErr)
				} else {
					slog.Info("successfully reloaded configuration on SIGHUP")
				}
			}
		}
	}()

	if cfg.MetricsAddress != "" {
		metricsMux := http.NewServeMux()
		metricsMux.Handle("/metrics", metrics.Handler())
		metricsMux.HandleFunc("/admin/drain", pool.HandleDrain)
		metricsMux.HandleFunc("/drain", pool.HandleDrain)
		metricsServer := &http.Server{
			Addr:    cfg.MetricsAddress,
			Handler: metricsMux,
		}
		go func() {
			slog.Info("metrics server listening", "address", cfg.MetricsAddress)
			if err := metricsServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				slog.Error("metrics server error", "error", err)
			}
		}()
	}

	shutdownChan := make(chan os.Signal, 1)
	signal.Notify(shutdownChan, os.Interrupt, syscall.SIGTERM)

	var httpServer *http.Server
	var l4Proxy *proxy.L4Proxy
	serverErrChan := make(chan error, 1)

	if cfg.Mode == "l7" {
		l7Proxy := proxy.NewL7Proxy(pool)
		mux := http.NewServeMux()
		mux.HandleFunc("/admin/drain", pool.HandleDrain)
		mux.HandleFunc("/drain", pool.HandleDrain)
		if cfg.MetricsAddress == "" {
			mux.Handle("/metrics", metrics.Handler())
		}
		mux.Handle("/", l7Proxy)

		httpServer = &http.Server{
			Addr:    cfg.ListenAddress,
			Handler: mux,
		}

		go func() {
			slog.Info("l7 proxy server listening", "address", cfg.ListenAddress)
			if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				serverErrChan <- fmt.Errorf("http server error: %w", err)
			}
		}()
	} else {
		l4Proxy = proxy.NewL4Proxy(pool, cfg.HealthCheck.Timeout)
		go func() {
			if err := l4Proxy.ListenAndServe(mainCtx, cfg.ListenAddress); err != nil {
				serverErrChan <- fmt.Errorf("l4 proxy error: %w", err)
			}
		}()
	}

	select {
	case sig := <-shutdownChan:
		slog.Info("received shutdown signal", "signal", sig.String())
	case err := <-serverErrChan:
		slog.Error("server fatal error", "error", err)
		os.Exit(1)
	}

	slog.Info("initiating graceful shutdown", "timeout", cfg.ShutdownTimeout.String())

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer shutdownCancel()

	if httpServer != nil {
		if err := httpServer.Shutdown(shutdownCtx); err != nil {
			slog.Error("http server graceful shutdown error", "error", err)
		}
	}

	if l4Proxy != nil {
		if err := l4Proxy.Close(shutdownCtx); err != nil {
			slog.Error("l4 proxy graceful shutdown error", "error", err)
		}
	}

	slog.Info("steadily shutdown complete")
	os.Exit(0)
}
