package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"go.uber.org/zap"

	"github.com/fleetq/fleetq-bridge/internal/relay"
)

func main() {
	apiURL := getenv("FLEETQ_API_URL", "https://fleetq.net")
	redisURL := buildRedisURL()
	listen := getenv("RELAY_LISTEN", ":8070")
	logLevel := getenv("LOG_LEVEL", "info")

	logger := buildLogger(logLevel)
	defer logger.Sync() //nolint:errcheck

	srv, err := relay.NewServer(relay.Config{
		APIURL:   apiURL,
		RedisURL: redisURL,
	}, logger)
	if err != nil {
		log.Fatalf("relay init: %v", err)
	}

	httpSrv := &http.Server{
		Addr:    listen,
		Handler: srv,
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	go func() {
		<-ctx.Done()
		logger.Info("shutting down relay")
		httpSrv.Shutdown(context.Background()) //nolint:errcheck
	}()

	fmt.Printf("fleetq-relay listening on %s  (api=%s  redis=%s)\n", listen, apiURL, redisURL)
	if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("relay: %v", err)
	}
}

func getenv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// buildRedisURL builds the Redis URL, injecting REDIS_PASSWORD if set.
func buildRedisURL() string {
	url := getenv("REDIS_URL", "redis://redis:6379/0")
	pass := os.Getenv("REDIS_PASSWORD")
	if pass != "" && !strings.Contains(url, "@") {
		// Inject password: redis://host:port → redis://:password@host:port
		url = strings.Replace(url, "redis://", "redis://:"+pass+"@", 1)
	}
	return url
}

func buildLogger(level string) *zap.Logger {
	cfg := zap.NewProductionConfig()
	switch level {
	case "debug":
		cfg.Level.SetLevel(zap.DebugLevel)
	case "warn":
		cfg.Level.SetLevel(zap.WarnLevel)
	case "error":
		cfg.Level.SetLevel(zap.ErrorLevel)
	}
	l, err := cfg.Build()
	if err != nil {
		log.Fatalf("logger: %v", err)
	}
	return l
}
