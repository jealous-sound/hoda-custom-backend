package api

import (
	"cmp"
	"crypto/subtle"
	"net/http"
	"runtime"
	"slices"
	"strings"
	"time"

	"github.com/jealous-sound/hoda-custom-backend/internal/activity"
	"github.com/jealous-sound/hoda-custom-backend/internal/clientversion"
	"github.com/jealous-sound/hoda-custom-backend/internal/guild"
	"github.com/jealous-sound/hoda-custom-backend/internal/report"
	"github.com/jealous-sound/hoda-custom-backend/internal/traffic"
)

const (
	adminPathPrefix     = "/admin/"
	adminOverviewPath   = "/admin/overview"
	adminActivityPath   = "/admin/activity"
	adminTrafficPath    = "/admin/traffic"
	adminPlayersPath    = "/admin/players"
	adminGuildsPath     = "/admin/guilds"
	adminWorldChatPath  = "/admin/chat/world"
	adminReportsPath    = "/admin/reports"
	adminLoginsPath     = "/admin/logins"
	MinAdminTokenLength = 32
	adminOnlineListed   = 100
	adminReportsListed  = 500
)

func (s *Server) administered(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.adminToken == "" {
			writeError(w, http.StatusNotFound, "the server runs without an admin token")
			return
		}
		visitor := s.visitor(r)
		token, found := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if found && subtle.ConstantTimeCompare([]byte(token), []byte(s.adminToken)) == 1 {
			if s.notLockedOut(w, s.limiters.adminGuessesByAddress, visitor) {
				next(w, r)
			}
			return
		}
		if s.allow(w, s.limiters.adminGuessesByAddress, visitor) {
			writeError(w, http.StatusUnauthorized, "the admin token is required")
		}
	}
}

func (s *Server) adminRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET "+adminMaintenancePath, s.administered(s.showMaintenance))
	mux.HandleFunc("PUT "+adminMaintenancePath, s.administered(s.scheduleMaintenance))
	mux.HandleFunc("DELETE "+adminMaintenancePath, s.administered(s.cancelMaintenance))
	mux.HandleFunc("GET "+adminOverviewPath, s.administered(s.adminOverview))
	mux.HandleFunc("GET "+adminActivityPath, s.administered(s.adminActivity))
	mux.HandleFunc("GET "+adminTrafficPath, s.administered(s.adminTraffic))
	mux.HandleFunc("GET "+adminPlayersPath, s.administered(s.adminPlayers))
	mux.HandleFunc("GET "+adminPlayersPath+"/{player}", s.administered(s.adminPlayer))
	mux.HandleFunc("GET "+adminGuildsPath, s.administered(s.adminGuilds))
	mux.HandleFunc("GET "+adminGuildsPath+"/{guild}", s.administered(s.adminGuild))
	mux.HandleFunc("GET "+adminWorldChatPath, s.administered(s.adminWorldChat))
	mux.HandleFunc("GET "+adminReportsPath, s.administered(s.adminReports))
	mux.HandleFunc("PUT "+adminLoginsPath+"/{login}/password", s.administered(s.adminResetPassword))
}

func polledByPanel(r *http.Request, status int) bool {
	return strings.HasPrefix(r.URL.Path, adminPathPrefix) && r.Method == http.MethodGet &&
		status < http.StatusBadRequest
}

func clientOf(r *http.Request) string {
	version, known := clientversion.FromUserAgent(r.Header.Get("User-Agent"))
	if !known {
		return activity.UnknownClient
	}
	return version.String()
}

type serverStatus struct {
	StartedAt      int64  `json:"started_at"`
	Revision       string `json:"revision"`
	GoVersion      string `json:"go_version"`
	Goroutines     int    `json:"goroutines"`
	MemoryBytes    uint64 `json:"memory_bytes"`
	HeapBytes      uint64 `json:"heap_bytes"`
	DataBytes      int64  `json:"data_bytes"`
	DataFiles      int    `json:"data_files"`
	DiskFreeBytes  uint64 `json:"disk_free_bytes"`
	DiskTotalBytes uint64 `json:"disk_total_bytes"`
	OpenSessions   int    `json:"open_sessions"`
}

type clientCount struct {
	Client string `json:"client"`
	Count  int    `json:"count"`
}

type onlinePlayer struct {
	PlayerID   string `json:"player_id"`
	Name       string `json:"name"`
	Level      int    `json:"level"`
	Client     string `json:"client"`
	LastSeenAt int64  `json:"last_seen_at"`
}

type onlineStatus struct {
	Count         int            `json:"count"`
	WindowSeconds int64          `json:"window_seconds"`
	Clients       []clientCount  `json:"clients"`
	Players       []onlinePlayer `json:"players"`
}

type playerTotals struct {
	Accounts         int `json:"accounts"`
	Profiles         int `json:"profiles"`
	Named            int `json:"named"`
	TutorialComplete int `json:"tutorial_complete"`
	InGuild          int `json:"in_guild"`
	Active24h        int `json:"active_24h"`
	Active7d         int `json:"active_7d"`
	Created24h       int `json:"created_24h"`
	Created7d        int `json:"created_7d"`
}

type guildTotals struct {
	Guilds        int `json:"guilds"`
	Members       int `json:"members"`
	Applications  int `json:"applications"`
	WorldMessages int `json:"world_messages"`
}

type overviewReply struct {
	ServerTime    int64        `json:"server_time"`
	MaintenanceAt int64        `json:"maintenance_at,omitempty"`
	Server        serverStatus `json:"server"`
	Client        struct {
		Latest      string `json:"latest"`
		Minimum     string `json:"minimum"`
		DownloadURL string `json:"download_url"`
	} `json:"client"`
	Online  onlineStatus `json:"online"`
	Today   activity.Day `json:"today"`
	Players playerTotals `json:"players"`
	Guilds  guildTotals  `json:"guilds"`
	Reports int          `json:"reports"`
}

func (s *Server) serverStatus() serverStatus {
	var memory runtime.MemStats
	runtime.ReadMemStats(&memory)
	usage := s.dataUsage.current(s.dataDir, s.now())
	return serverStatus{
		StartedAt: s.started.Unix(), Revision: s.revision, GoVersion: runtime.Version(),
		Goroutines: runtime.NumGoroutine(), MemoryBytes: memory.Sys, HeapBytes: memory.HeapAlloc,
		DataBytes: usage.bytes, DataFiles: usage.files, DiskFreeBytes: usage.diskFree, DiskTotalBytes: usage.diskTotal,
		OpenSessions: s.sessions.Count(),
	}
}

func onlineOf(players []adminPlayer) onlineStatus {
	status := onlineStatus{
		WindowSeconds: int64(activity.OnlineWindow / time.Second), Clients: []clientCount{},
		Players: []onlinePlayer{},
	}
	byClient := map[string]int{}
	for _, player := range players {
		if !player.Online {
			continue
		}
		status.Count++
		byClient[player.Client]++
		status.Players = append(status.Players, onlinePlayer{
			PlayerID: player.PlayerID, Name: player.Name, Level: player.Level, Client: player.Client,
			LastSeenAt: player.LastSeenAt,
		})
	}
	slices.SortFunc(status.Players, func(a, b onlinePlayer) int {
		return cmp.Or(cmp.Compare(b.LastSeenAt, a.LastSeenAt), cmp.Compare(a.PlayerID, b.PlayerID))
	})
	status.Players = status.Players[:min(len(status.Players), adminOnlineListed)]
	for client, count := range byClient {
		status.Clients = append(status.Clients, clientCount{Client: client, Count: count})
	}
	slices.SortFunc(status.Clients, func(a, b clientCount) int {
		return cmp.Or(cmp.Compare(b.Count, a.Count), cmp.Compare(a.Client, b.Client))
	})
	return status
}

func totalsOf(players []adminPlayer, now time.Time) playerTotals {
	var totals playerTotals
	dayAgo, weekAgo := now.Add(-24*time.Hour).Unix(), now.Add(-7*24*time.Hour).Unix()
	for _, player := range players {
		if player.Registered {
			totals.Accounts++
		}
		if player.HasProfile {
			totals.Profiles++
		}
		if player.Name != "" {
			totals.Named++
		}
		if player.HasProfile && player.TutorialComplete {
			totals.TutorialComplete++
		}
		if player.GuildID != "" {
			totals.InGuild++
		}
		if player.LastActiveAt > dayAgo {
			totals.Active24h++
		}
		if player.LastActiveAt > weekAgo {
			totals.Active7d++
		}
		if player.Registered && player.CreatedAt > dayAgo {
			totals.Created24h++
		}
		if player.Registered && player.CreatedAt > weekAgo {
			totals.Created7d++
		}
	}
	return totals
}

func (s *Server) guildTotals() guildTotals {
	var totals guildTotals
	s.guilds.View(func(state *guild.State) {
		totals.Guilds = len(state.Guilds)
		for _, g := range state.Guilds {
			totals.Members += len(g.Members)
			totals.Applications += len(g.Applications)
		}
		totals.WorldMessages = len(state.World.Messages)
	})
	return totals
}

func (s *Server) adminOverview(w http.ResponseWriter, _ *http.Request) {
	players, err := s.adminDirectory()
	if err != nil {
		s.log.Error("admin overview", "error", err)
		writeError(w, http.StatusInternalServerError, "could not read players")
		return
	}
	reports, err := s.reports.Entries()
	if err != nil {
		s.log.Error("admin overview", "error", err)
		writeError(w, http.StatusInternalServerError, "could not read reports")
		return
	}
	update := s.clientUpdate()
	reply := overviewReply{
		ServerTime: update.ServerTime, MaintenanceAt: update.MaintenanceAt, Server: s.serverStatus(),
		Online: onlineOf(players), Today: s.activity.Today(), Players: totalsOf(players, s.now()),
		Guilds: s.guildTotals(), Reports: len(reports),
	}
	reply.Client.Latest, reply.Client.Minimum, reply.Client.DownloadURL = update.Latest, update.Minimum,
		update.DownloadURL
	writeJSON(w, http.StatusOK, reply)
}

type activityReply struct {
	ServerTime    int64 `json:"server_time"`
	BucketSeconds int64 `json:"bucket_seconds"`
	activity.History
}

func (s *Server) adminActivity(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, activityReply{
		ServerTime: s.now().Unix(), BucketSeconds: int64(activity.BucketLength / time.Second),
		History: s.activity.History(),
	})
}

type trafficReply struct {
	ServerTime int64 `json:"server_time"`
	traffic.Snapshot
}

func (s *Server) adminTraffic(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, trafficReply{ServerTime: s.now().Unix(), Snapshot: s.traffic.Snapshot()})
}

type worldChatReply struct {
	ServerTime int64           `json:"server_time"`
	Messages   []guild.Message `json:"messages"`
}

func (s *Server) adminWorldChat(w http.ResponseWriter, _ *http.Request) {
	reply := worldChatReply{ServerTime: s.now().Unix()}
	s.guilds.View(func(state *guild.State) { reply.Messages = state.World.MessagesAfter(0) })
	writeJSON(w, http.StatusOK, reply)
}

type reportsReply struct {
	ServerTime int64          `json:"server_time"`
	Total      int            `json:"total"`
	Reports    []report.Entry `json:"reports"`
}

func (s *Server) adminReports(w http.ResponseWriter, _ *http.Request) {
	entries, err := s.reports.Entries()
	if err != nil {
		s.log.Error("admin reports", "error", err)
		writeError(w, http.StatusInternalServerError, "could not read reports")
		return
	}
	total := len(entries)
	slices.Reverse(entries)
	writeJSON(w, http.StatusOK, reportsReply{
		ServerTime: s.now().Unix(), Total: total, Reports: entries[:min(total, adminReportsListed)],
	})
}
