package api

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/jealous-sound/hoda-custom-backend/internal/account"
	"github.com/jealous-sound/hoda-custom-backend/internal/catalog"
	"github.com/jealous-sound/hoda-custom-backend/internal/economy"
	"github.com/jealous-sound/hoda-custom-backend/internal/guild"
	"github.com/jealous-sound/hoda-custom-backend/internal/profile"
	"github.com/jealous-sound/hoda-custom-backend/internal/report"
	"github.com/jealous-sound/hoda-custom-backend/internal/session"
)

const RevisionHeader = "X-Profile-Revision"

type Server struct {
	accounts              *account.Store
	profiles              *profile.Store
	sessions              *session.Registry
	catalog               *catalog.Catalog
	guilds                *guild.Store
	reports               *report.Log
	now                   func() time.Time
	log                   *slog.Logger
	openUnregistered      bool
	trustCloudflareHeader bool
	limiters              limiters
	battleSeeds           economy.SeedSource
}

type Config struct {
	OpenUnregistered      bool
	TrustCloudflareHeader bool
	Limits                Limits
	BattleSeeds           economy.SeedSource
}

func NewServer(
	accounts *account.Store, profiles *profile.Store, sessions *session.Registry, shopCatalog *catalog.Catalog,
	guilds *guild.Store, reports *report.Log, now func() time.Time, log *slog.Logger, config Config,
) *Server {
	return &Server{
		accounts: accounts, profiles: profiles, sessions: sessions, catalog: shopCatalog, guilds: guilds,
		reports: reports, now: now, log: log, openUnregistered: config.OpenUnregistered,
		trustCloudflareHeader: config.TrustCloudflareHeader,
		limiters:              newLimiters(config.Limits, now), battleSeeds: config.BattleSeeds,
	}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.health)
	mux.HandleFunc("GET /v1/time", s.serverTime)
	mux.HandleFunc("POST /v1/players", s.createPlayer)
	mux.HandleFunc("POST /v1/sessions", s.openSession)
	mux.HandleFunc("GET /v1/players/{player}/profile", s.authorized(s.getProfile))
	mux.HandleFunc("PUT /v1/players/{player}/profile", s.authorized(s.putProfile))
	mux.HandleFunc("POST /v1/players/{player}/store/purchases", s.authorized(s.purchase))
	mux.HandleFunc("POST /v1/players/{player}/refills", s.authorized(s.refill))
	mux.HandleFunc("POST /v1/players/{player}/actions", s.authorized(s.playerAction))
	mux.HandleFunc("GET /v1/players/{player}/guild", s.authorized(s.getGuild))
	mux.HandleFunc("POST /v1/players/{player}/guild/settings", s.authorized(s.changeGuildSettings))
	mux.HandleFunc("POST /v1/players/{player}/guild/leave", s.authorized(s.leaveGuild))
	mux.HandleFunc("POST /v1/players/{player}/guild/messages", s.authorized(s.guildMessages))
	mux.HandleFunc("POST /v1/players/{player}/guild/applications/{member}/accept",
		s.authorized(s.acceptGuildApplication))
	mux.HandleFunc("POST /v1/players/{player}/guild/applications/{member}/deny", s.authorized(s.denyGuildApplication))
	mux.HandleFunc("POST /v1/players/{player}/guild/members/{member}/rank", s.authorized(s.changeGuildRank))
	mux.HandleFunc("POST /v1/players/{player}/guild/members/{member}/kick", s.authorized(s.kickGuildMember))
	mux.HandleFunc("GET /v1/players/{player}/guild/members/{member}/duel", s.authorized(s.duelOpponent))
	mux.HandleFunc("GET /v1/players/{player}/guild/onslaught", s.authorized(s.onslaughtStandings))
	mux.HandleFunc("POST /v1/players/{player}/guild/onslaught/claim", s.authorized(s.claimOnslaught))
	mux.HandleFunc("POST /v1/players/{player}/reports", s.authorized(s.reportAbuse))
	mux.HandleFunc("POST /v1/players/{player}/guilds", s.authorized(s.createGuild))
	mux.HandleFunc("GET /v1/players/{player}/guilds/recommended", s.authorized(s.recommendedGuilds))
	mux.HandleFunc("GET /v1/players/{player}/guilds/search", s.authorized(s.searchGuilds))
	mux.HandleFunc("GET /v1/players/{player}/guilds/{guild}", s.authorized(s.observeGuild))
	mux.HandleFunc("POST /v1/players/{player}/guilds/{guild}/join", s.authorized(s.joinGuild))
	mux.HandleFunc("POST /v1/players/{player}/guilds/{guild}/cancel", s.authorized(s.cancelGuildApplication))
	return s.logged(mux)
}

func (s *Server) logged(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		recorder := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		started := time.Now()
		next.ServeHTTP(recorder, r)
		s.log.Info("request", "method", r.Method, "path", r.URL.Path, "status", recorder.status,
			"duration", time.Since(started), "client", s.visitor(r))
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

func (s *Server) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) serverTime(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]int64{"server_time": s.now().Unix()})
}

type createPlayerResponse struct {
	PlayerID   string `json:"player_id"`
	Secret     string `json:"secret"`
	ServerTime int64  `json:"server_time"`
}

func (s *Server) createPlayer(w http.ResponseWriter, r *http.Request) {
	if !s.allow(w, s.limiters.accountsByAddress, s.visitor(r)) || !s.allow(w, s.limiters.accountsTotal, "") {
		return
	}
	credentials, err := s.accounts.CreateGuest()
	if err != nil {
		s.log.Error("create player", "error", err)
		writeError(w, http.StatusInternalServerError, "could not create player")
		return
	}
	s.log.Info("created guest player", "player", credentials.PlayerID)
	writeJSON(w, http.StatusCreated, createPlayerResponse{
		PlayerID:   credentials.PlayerID,
		Secret:     credentials.Secret,
		ServerTime: s.now().Unix(),
	})
}

type sessionRequest struct {
	PlayerID string `json:"player_id"`
	Secret   string `json:"secret"`
}

type sessionResponse struct {
	Token      string `json:"token"`
	PlayerID   string `json:"player_id"`
	ServerTime int64  `json:"server_time"`
}

func (s *Server) openSession(w http.ResponseWriter, r *http.Request) {
	if !s.allow(w, s.limiters.sessionsByAddress, s.visitor(r)) {
		return
	}
	var request sessionRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, "body must be {\"player_id\": \"...\", \"secret\": \"...\"}")
		return
	}
	if !profile.ValidPlayerID(request.PlayerID) {
		writeError(w, http.StatusBadRequest, "player_id must be 1-64 letters, digits, '-' or '_'")
		return
	}
	verified, err := s.accounts.Verify(request.PlayerID, request.Secret)
	if err != nil && !errors.Is(err, account.ErrNotRegistered) {
		s.log.Error("verify player", "player", request.PlayerID, "error", err)
		writeError(w, http.StatusInternalServerError, "could not verify player")
		return
	}
	registered := err == nil
	if !registered && !s.openUnregistered {
		writeError(w, http.StatusNotFound, "no account for this player; create one with POST /v1/players")
		return
	}
	if registered && !verified {
		writeError(w, http.StatusUnauthorized, "wrong secret for player")
		return
	}
	if registered {
		if err := s.accounts.RecordSession(request.PlayerID); err != nil {
			s.log.Error("record session", "player", request.PlayerID, "error", err)
			writeError(w, http.StatusInternalServerError, "could not open session")
			return
		}
	}
	token, err := s.sessions.Open(request.PlayerID)
	if err != nil {
		s.log.Error("open session", "error", err)
		writeError(w, http.StatusInternalServerError, "could not open session")
		return
	}
	writeJSON(w, http.StatusOK, sessionResponse{Token: token, PlayerID: request.PlayerID, ServerTime: s.now().Unix()})
}

func (s *Server) authorized(next func(http.ResponseWriter, *http.Request, string)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token, found := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		sessionPlayer, open := s.sessions.Player(token)
		if !found || !open {
			writeError(w, http.StatusUnauthorized, "open a session with POST /v1/sessions first")
			return
		}
		player := r.PathValue("player")
		if player != sessionPlayer {
			writeError(w, http.StatusForbidden, "session belongs to another player")
			return
		}
		if !s.allow(w, s.limiters.requestsByPlayer, player) {
			return
		}
		next(w, r, player)
	}
}

func (s *Server) getProfile(w http.ResponseWriter, _ *http.Request, player string) {
	record, err := s.profiles.Get(player)
	if errors.Is(err, profile.ErrNotFound) {
		writeError(w, http.StatusNotFound, "no profile yet")
		return
	}
	if err != nil {
		s.log.Error("read profile", "player", player, "error", err)
		writeError(w, http.StatusInternalServerError, "could not read profile")
		return
	}
	writeRevision(w, record.Revision)
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Length", strconv.Itoa(len(record.Profile)))
	w.WriteHeader(http.StatusOK)
	w.Write(record.Profile)
}

func (s *Server) putProfile(w http.ResponseWriter, r *http.Request, player string) {
	expected, ok := parseIfMatch(r.Header.Get("If-Match"))
	if !ok {
		writeError(w, http.StatusPreconditionRequired, "If-Match with the last seen revision is required")
		return
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, profile.MaxProfileBytes+1))
	if err != nil {
		writeError(w, http.StatusBadRequest, "could not read body")
		return
	}
	body, err := profile.Validate(raw)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	record, err := s.profiles.PutMerged(player, expected, func(current profile.Record, next *profile.Record) error {
		merged, err := economy.MergeUpload(s.catalog, current, &next.Server, body, s.now())
		next.Profile = merged
		return err
	})
	var refusal *economy.Refusal
	if errors.As(err, &refusal) {
		writeError(w, http.StatusUnprocessableEntity, refusal.Message)
		return
	}
	if errors.Is(err, profile.ErrRevisionMismatch) {
		writeRevision(w, record.Revision)
		writeError(w, http.StatusConflict, "profile was changed since revision "+strconv.FormatInt(expected, 10))
		return
	}
	if err != nil {
		s.log.Error("write profile", "player", player, "error", err)
		writeError(w, http.StatusInternalServerError, "could not store profile")
		return
	}
	writeRevision(w, record.Revision)
	writeJSON(w, http.StatusOK, map[string]int64{"revision": record.Revision})
}

func parseIfMatch(header string) (int64, bool) {
	value := strings.Trim(strings.TrimSpace(header), `"`)
	if value == "" {
		return 0, false
	}
	revision, err := strconv.ParseInt(value, 10, 64)
	return revision, err == nil && revision >= 0
}

func writeRevision(w http.ResponseWriter, revision int64) {
	w.Header().Set(RevisionHeader, strconv.FormatInt(revision, 10))
	w.Header().Set("ETag", strconv.Quote(strconv.FormatInt(revision, 10)))
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	body, err := json.Marshal(value)
	if err != nil {
		status, body = http.StatusInternalServerError, []byte(`{"error":"could not encode reply"}`)
	}
	body = append(body, '\n')
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Length", strconv.Itoa(len(body)))
	w.WriteHeader(status)
	w.Write(body)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}
