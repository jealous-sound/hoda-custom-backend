package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/danbestan/hoda-custom-backend/internal/account"
	"github.com/danbestan/hoda-custom-backend/internal/api"
	"github.com/danbestan/hoda-custom-backend/internal/profile"
	"github.com/danbestan/hoda-custom-backend/internal/session"
)

const cliDescription = "Backend for the Heroes of Dragon Age native client: sessions, server clock and player profiles."

const shutdownGrace = 5 * time.Second

type options struct {
	addr        string
	dataDir     string
	seedProfile string
	seedPlayer  string
}

func parseOptions() options {
	var o options
	flag.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(), "%s\n\nUsage of %s:\n", cliDescription, os.Args[0])
		flag.PrintDefaults()
	}
	flag.StringVar(&o.addr, "addr", "127.0.0.1:8787", "listen address")
	flag.StringVar(&o.dataDir, "data", "runtime", "directory for player accounts and profiles")
	flag.StringVar(&o.seedProfile, "seed-profile", "", "client save file to import when the seed player has no profile")
	flag.StringVar(&o.seedPlayer, "seed-player", "local", "player that -seed-profile is imported for")
	flag.Parse()
	return o
}

func seed(store *profile.Store, o options, log *slog.Logger) error {
	if o.seedProfile == "" {
		return nil
	}
	if _, err := store.Get(o.seedPlayer); !errors.Is(err, profile.ErrNotFound) {
		return err
	}
	raw, err := os.ReadFile(o.seedProfile)
	if errors.Is(err, os.ErrNotExist) {
		log.Info("no save to import", "path", o.seedProfile)
		return nil
	}
	if err != nil {
		return err
	}
	body, err := profile.Validate(raw)
	if err != nil {
		return fmt.Errorf("seed profile %s: %w", o.seedProfile, err)
	}
	if _, err := store.Put(o.seedPlayer, 0, body); err != nil {
		return err
	}
	log.Info("imported save", "player", o.seedPlayer, "path", o.seedProfile)
	return nil
}

func run(o options, log *slog.Logger) error {
	store, err := profile.NewStore(o.dataDir, time.Now)
	if err != nil {
		return err
	}
	if err := seed(store, o, log); err != nil {
		return err
	}
	accounts, err := account.NewStore(o.dataDir, time.Now)
	if err != nil {
		return err
	}
	server := &http.Server{
		Addr:              o.addr,
		Handler:           api.NewServer(accounts, store, session.NewRegistry(), time.Now, log).Handler(),
		ReadHeaderTimeout: 5 * time.Second,
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	failed := make(chan error, 1)
	go func() {
		log.Info("listening", "addr", o.addr, "data", o.dataDir)
		failed <- server.ListenAndServe()
	}()
	select {
	case err := <-failed:
		return err
	case <-ctx.Done():
	}
	shutdown, cancel := context.WithTimeout(context.Background(), shutdownGrace)
	defer cancel()
	return server.Shutdown(shutdown)
}

func main() {
	o := parseOptions()
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	if err := run(o, log); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Error("server stopped", "error", err)
		os.Exit(1)
	}
}
