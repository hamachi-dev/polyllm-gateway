package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"github.com/taizo/polyllm-gateway/internal/api"
	"github.com/taizo/polyllm-gateway/internal/config"
	"github.com/taizo/polyllm-gateway/internal/logger"
	"github.com/taizo/polyllm-gateway/internal/provider"
	"github.com/taizo/polyllm-gateway/internal/resolver"
	"syscall"
	"time"
)

func generateRequestID() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func requestIDMiddleware(next http.Handler, log *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("X-Request-ID")
		if id == "" {
			id = generateRequestID()
		}
		ctx := logger.WithRequestID(r.Context(), id)
		w.Header().Set("X-Request-ID", id)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func main() {
	log := logger.New()

	cfgPath := "config.yaml"
	if len(os.Args) > 1 {
		cfgPath = os.Args[1]
	}

	cfg, err := config.Load(cfgPath)
	if err != nil {
		log.Error("failed to load config", "error", err)
		os.Exit(1)
	}

	routes := make(map[string]resolver.Route)
	for model, rc := range cfg.Routes {
		routes[model] = resolver.Route{
			Provider: rc.Provider,
			Model:    rc.Model,
			API:      rc.API,
		}
	}
	res := resolver.New(routes)

	providers := make(map[string]provider.Provider)
	for name, pc := range cfg.Providers {
		providers[name] = provider.NewOpenCode(provider.OpenCodeConfig{
			Endpoint: pc.Endpoint,
			APIKey:   pc.APIKey,
		})
		log.Info("registered provider", "name", name, "endpoint", pc.Endpoint)
	}

	openAIHandler := api.NewOpenAIHandler(res, providers, log)
	anthropicHandler := api.NewAnthropicHandler(res, providers, log)
	responsesHandler := api.NewResponsesHandler(res, providers, log)

	muxOpenAI := http.NewServeMux()
	muxOpenAI.Handle("/v1/chat/completions", requestIDMiddleware(openAIHandler, log))
	muxOpenAI.Handle("/v1/responses", requestIDMiddleware(responsesHandler, log))

	muxAnthropic := http.NewServeMux()
	muxAnthropic.Handle("/v1/messages", requestIDMiddleware(anthropicHandler, log))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	srvOpenAI := &http.Server{
		Addr:    cfg.Listen.OpenAI,
		Handler: muxOpenAI,
	}

	srvAnthropic := &http.Server{
		Addr:    cfg.Listen.Anthropic,
		Handler: muxAnthropic,
	}

	errCh := make(chan error, 2)

	go func() {
		log.Info("starting openai server", "addr", cfg.Listen.OpenAI)
		if err := srvOpenAI.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- fmt.Errorf("openai server: %w", err)
		}
	}()

	go func() {
		log.Info("starting anthropic server", "addr", cfg.Listen.Anthropic)
		if err := srvAnthropic.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- fmt.Errorf("anthropic server: %w", err)
		}
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	select {
	case sig := <-sigCh:
		log.Info("received signal", "signal", sig)
	case err := <-errCh:
		log.Error("server error", "error", err)
	}

	shutdownCtx, shutdownCancel := context.WithTimeout(ctx, 10*time.Second)
	defer shutdownCancel()

	if err := srvOpenAI.Shutdown(shutdownCtx); err != nil {
		log.Error("openai server shutdown error", "error", err)
	}
	if err := srvAnthropic.Shutdown(shutdownCtx); err != nil {
		log.Error("anthropic server shutdown error", "error", err)
	}
	log.Info("servers stopped")
}
