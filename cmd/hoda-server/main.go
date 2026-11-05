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

	"github.com/jealous-sound/hoda-custom-backend/internal/account"
	"github.com/jealous-sound/hoda-custom-backend/internal/api"
	"github.com/jealous-sound/hoda-custom-backend/internal/catalog"
	"github.com/jealous-sound/hoda-custom-backend/internal/guild"
	"github.com/jealous-sound/hoda-custom-backend/internal/profile"
	"github.com/jealous-sound/hoda-custom-backend/internal/prune"
	"github.com/jealous-sound/hoda-custom-backend/internal/report"
	"github.com/jealous-sound/hoda-custom-backend/internal/session"
)

const cliDescription = "Backend for the Heroes of Dragon Age native client: sessions, server clock and player profiles."

const shutdownGrace = 5 * time.Second

const (
	defaultGuestIdle  = 14 * 24 * time.Hour
	defaultPruneEvery = time.Hour
)

type options struct {
	addr        string
	dataDir     string
	seedProfile string
	seedPlayer  string
	guestIdle   time.Duration
	pruneEvery  time.Duration
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
	flag.DurationVar(&o.guestIdle, "guest-idle", defaultGuestIdle,
		"delete unnamed guests that never finished the tutorial after this long without a session; 0 keeps them")
	flag.DurationVar(&o.pruneEvery, "prune-every", defaultPruneEvery, "how often to look for abandoned guests")
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

func pruneGuests(ctx context.Context, pruner *prune.Pruner, every time.Duration, log *slog.Logger) {
	ticker := time.NewTicker(every)
	defer ticker.Stop()
	for {
		pruned, err := pruner.AbandonedGuests()
		if len(pruned) > 0 {
			log.Info("pruned abandoned guests", "count", len(pruned), "players", pruned)
		}
		if err != nil {
			log.Error("prune guests", "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
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
	if o.guestIdle > 0 && o.pruneEvery <= 0 {
		return errors.New("-prune-every must be positive while -guest-idle is set")
	}
	shopCatalog, err := catalog.Default()
	if err != nil {
		return fmt.Errorf("load store catalog: %w", err)
	}
	guilds, err := guild.NewStore(o.dataDir)
	if err != nil {
		return err
	}
	reports, err := report.NewLog(o.dataDir)
	if err != nil {
		return err
	}
	sessions := session.NewRegistry()
	server := &http.Server{
		Addr:              o.addr,
		Handler:           api.NewServer(accounts, store, sessions, shopCatalog, guilds, reports, time.Now, log).Handler(),
		ReadHeaderTimeout: 5 * time.Second,
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if o.guestIdle > 0 {
		go pruneGuests(ctx, prune.NewPruner(accounts, store, sessions, time.Now, o.guestIdle), o.pruneEvery, log)
	}
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
