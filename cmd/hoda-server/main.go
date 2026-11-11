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
	"github.com/jealous-sound/hoda-custom-backend/internal/clientversion"
	"github.com/jealous-sound/hoda-custom-backend/internal/guild"
	"github.com/jealous-sound/hoda-custom-backend/internal/maintenance"
	"github.com/jealous-sound/hoda-custom-backend/internal/profile"
	"github.com/jealous-sound/hoda-custom-backend/internal/prune"
	"github.com/jealous-sound/hoda-custom-backend/internal/report"
	"github.com/jealous-sound/hoda-custom-backend/internal/session"
)

const cliDescription = "Backend for the Heroes of Dragon Age native client: sessions, server clock and player profiles."

const shutdownGrace = 5 * time.Second

const adminTokenVariable = "HOD_ADMIN_TOKEN"

const (
	defaultGuestIdle   = 14 * 24 * time.Hour
	defaultPruneEvery  = time.Hour
	defaultSessionIdle = 24 * time.Hour
)

const (
	defaultClientReleases      = "https://api.github.com/repos/jealous-sound/hoda-custom-client/releases/latest"
	defaultClientReleasesEvery = 15 * time.Minute
	defaultClientDownloadURL   = "https://hodareborn.com/#download"
	clientReleasesTimeout      = 10 * time.Second
)

type options struct {
	addr            string
	dataDir         string
	seedProfile     string
	seedPlayer      string
	guestIdle       time.Duration
	pruneEvery      time.Duration
	sessionIdle     time.Duration
	unregistered    bool
	trustCloudflare bool
	minimumClient   string
	clientReleases  string
	releasesEvery   time.Duration
	clientDownload  string
	adminToken      string
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
	flag.DurationVar(&o.sessionIdle, "session-idle", defaultSessionIdle,
		"unused session tokens expire after this long; 0 keeps them until restart")
	flag.BoolVar(&o.unregistered, "open-unregistered", false,
		"let player ids without an account, such as -seed-player, open sessions without a secret")
	flag.BoolVar(&o.trustCloudflare, "trust-cf-connecting-ip", false,
		"take the visitor address from CF-Connecting-IP whenever it is present; only behind an edge that refuses "+
			"that header from anywhere but Cloudflare, as Railway's does")
	flag.StringVar(&o.minimumClient, "min-client-version", "",
		"answer 426 Upgrade Required to clients older than this tag, such as v0.5.0; empty lets every version in")
	flag.StringVar(&o.clientReleases, "client-releases", defaultClientReleases,
		"GitHub latest-release URL whose tag is the newest client version; empty turns the lookup off")
	flag.DurationVar(&o.releasesEvery, "client-releases-every", defaultClientReleasesEvery,
		"how often to look up the newest client release")
	flag.StringVar(&o.clientDownload, "client-download-url", defaultClientDownloadURL,
		"page the client opens to download a new version")
	flag.Parse()
	o.adminToken = os.Getenv(adminTokenVariable)
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

func followClientReleases(ctx context.Context, feed *clientversion.ReleaseFeed, every time.Duration, log *slog.Logger) {
	ticker := time.NewTicker(every)
	defer ticker.Stop()
	for {
		lookup, cancel := context.WithTimeout(ctx, clientReleasesTimeout)
		previous := feed.Latest()
		err := feed.Refresh(lookup)
		cancel()
		if err != nil {
			log.Error("look up latest client release", "error", err)
		} else if latest := feed.Latest(); latest.String() != previous.String() {
			log.Info("latest client release", "version", latest.String())
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func clientUpdates(o options, feed *clientversion.ReleaseFeed) (api.ClientUpdates, error) {
	updates := api.ClientUpdates{DownloadURL: o.clientDownload}
	if o.minimumClient != "" {
		minimum, ok := clientversion.Parse(o.minimumClient)
		if !ok {
			return updates, fmt.Errorf("-min-client-version %q is not a version such as v0.5.0", o.minimumClient)
		}
		updates.Minimum = minimum
	}
	if feed != nil {
		updates.Latest = feed.Latest
	}
	return updates, nil
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
	var releases *clientversion.ReleaseFeed
	if o.clientReleases != "" {
		if o.releasesEvery <= 0 {
			return errors.New("-client-releases-every must be positive while -client-releases is set")
		}
		releases = clientversion.NewReleaseFeed(o.clientReleases, &http.Client{Timeout: clientReleasesTimeout})
	}
	updates, err := clientUpdates(o, releases)
	if err != nil {
		return err
	}
	if o.adminToken != "" && len(o.adminToken) < api.MinAdminTokenLength {
		return fmt.Errorf("%s must be at least %d characters", adminTokenVariable, api.MinAdminTokenLength)
	}
	schedule, err := maintenance.Open(o.dataDir, time.Now)
	if err != nil {
		return err
	}
	if at, scheduled := schedule.At(); scheduled {
		log.Info("maintenance is still scheduled", "at", at.UTC().Format(time.RFC3339))
	}
	sessions := session.NewRegistry(time.Now, o.sessionIdle)
	server := &http.Server{
		Addr: o.addr,
		Handler: api.NewServer(accounts, store, sessions, shopCatalog, guilds, reports, time.Now, log, api.Config{
			OpenUnregistered: o.unregistered, TrustCloudflareHeader: o.trustCloudflare, Limits: api.DefaultLimits(),
			ClientUpdates: updates, Maintenance: schedule, AdminToken: o.adminToken,
		}).Handler(),
		ReadHeaderTimeout: 5 * time.Second,
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if releases != nil {
		go followClientReleases(ctx, releases, o.releasesEvery, log)
	}
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
