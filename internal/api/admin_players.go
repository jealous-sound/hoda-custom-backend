package api

import (
	"cmp"
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"sync"
	"time"

	"github.com/jealous-sound/hoda-custom-backend/internal/account"
	"github.com/jealous-sound/hoda-custom-backend/internal/guild"
	"github.com/jealous-sound/hoda-custom-backend/internal/profile"
)

const storedPlayersMaxAge = 10 * time.Second

type adminPlayer struct {
	PlayerID         string `json:"player_id"`
	Login            string `json:"login,omitempty"`
	Name             string `json:"name"`
	Level            int    `json:"level"`
	Gold             int64  `json:"gold"`
	Gems             int64  `json:"gems"`
	Trophies         int    `json:"trophies"`
	BestTrophies     int    `json:"best_trophies"`
	PvpWins          int    `json:"pvp_wins"`
	PvpLosses        int    `json:"pvp_losses"`
	Heroes           int    `json:"heroes"`
	PacksOpened      int    `json:"packs_opened"`
	TutorialComplete bool   `json:"tutorial_complete"`
	Registered       bool   `json:"registered"`
	HasProfile       bool   `json:"has_profile"`
	Unreadable       bool   `json:"unreadable,omitempty"`
	CreatedAt        int64  `json:"created_at"`
	LastSessionAt    int64  `json:"last_session_at"`
	Revision         int64  `json:"revision"`
	UpdatedAt        int64  `json:"updated_at"`
	ProfileBytes     int    `json:"profile_bytes"`
	GuildID          string `json:"guild_id,omitempty"`
	GuildName        string `json:"guild_name,omitempty"`
	Online           bool   `json:"online"`
	LastSeenAt       int64  `json:"last_seen_at"`
	LastActiveAt     int64  `json:"last_active_at"`
	Client           string `json:"client"`
}

func unixOrZero(moment time.Time) int64 {
	if moment.IsZero() {
		return 0
	}
	return moment.Unix()
}

func (p *adminPlayer) describeAccount(record account.Account) {
	p.Registered = true
	p.Login = record.Login
	p.CreatedAt = unixOrZero(record.CreatedAt)
	p.LastSessionAt = unixOrZero(record.LastSessionAt)
}

func (p *adminPlayer) describeProfile(summary profile.Summary) {
	p.HasProfile = true
	p.Name, p.Level, p.Gold, p.Gems = summary.Name, summary.Level, summary.Gold, summary.Gems
	p.Trophies, p.BestTrophies = summary.Trophies, summary.BestTrophies
	p.PvpWins, p.PvpLosses = summary.PvpWins, summary.PvpLosses
	p.Heroes, p.PacksOpened, p.TutorialComplete = summary.Heroes, summary.PacksOpened, summary.TutorialComplete
	p.Unreadable = summary.Unreadable
	p.Revision, p.UpdatedAt, p.ProfileBytes = summary.Revision, unixOrZero(summary.UpdatedAt), summary.ProfileBytes
}

type storedPlayers struct {
	mutex   sync.Mutex
	readAt  time.Time
	players []adminPlayer
}

func (s *Server) readStoredPlayers() ([]adminPlayer, error) {
	accounts, err := s.accounts.All()
	if err != nil {
		return nil, err
	}
	summaries, err := s.profiles.Summaries()
	if err != nil {
		return nil, err
	}
	byID := map[string]*adminPlayer{}
	entry := func(playerID string) *adminPlayer {
		if _, known := byID[playerID]; !known {
			byID[playerID] = &adminPlayer{PlayerID: playerID}
		}
		return byID[playerID]
	}
	for _, record := range accounts {
		entry(record.PlayerID).describeAccount(record)
	}
	for _, summary := range summaries {
		entry(summary.PlayerID).describeProfile(summary)
	}
	players := make([]adminPlayer, 0, len(byID))
	for _, player := range byID {
		players = append(players, *player)
	}
	slices.SortFunc(players, func(a, b adminPlayer) int { return cmp.Compare(a.PlayerID, b.PlayerID) })
	return players, nil
}

func (c *storedPlayers) current(now time.Time, read func() ([]adminPlayer, error)) ([]adminPlayer, error) {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	if age := now.Sub(c.readAt); c.players == nil || age >= storedPlayersMaxAge || age < 0 {
		players, err := read()
		if err != nil {
			return nil, err
		}
		c.players, c.readAt = players, now
	}
	return slices.Clone(c.players), nil
}

func (s *Server) adminDirectory() ([]adminPlayer, error) {
	now := s.now()
	players, err := s.storedPlayers.current(now, s.readStoredPlayers)
	if err != nil {
		return nil, err
	}
	index := make(map[string]int, len(players))
	for i, player := range players {
		index[player.PlayerID] = i
	}
	at := func(playerID string) *adminPlayer {
		i, known := index[playerID]
		if !known {
			players = append(players, adminPlayer{PlayerID: playerID})
			i = len(players) - 1
			index[playerID] = i
		}
		return &players[i]
	}
	for _, presence := range s.activity.Sightings() {
		player := at(presence.PlayerID)
		player.LastSeenAt, player.Client, player.Online = presence.LastSeen, presence.Client, presence.OnlineAt(now)
	}
	s.guilds.View(func(state *guild.State) {
		for _, g := range state.Guilds {
			for _, member := range g.Members {
				player := at(member.PlayerID)
				player.GuildID, player.GuildName = g.ID, g.Name
			}
		}
	})
	for i := range players {
		players[i].LastActiveAt = max(players[i].LastSessionAt, players[i].UpdatedAt, players[i].LastSeenAt)
	}
	return players, nil
}

type playersReply struct {
	ServerTime int64         `json:"server_time"`
	Players    []adminPlayer `json:"players"`
}

func (s *Server) adminPlayers(w http.ResponseWriter, _ *http.Request) {
	players, err := s.adminDirectory()
	if err != nil {
		s.log.Error("admin players", "error", err)
		writeError(w, http.StatusInternalServerError, "could not read players")
		return
	}
	writeJSON(w, http.StatusOK, playersReply{ServerTime: s.now().Unix(), Players: players})
}

type pendingBattleView struct {
	ID        int64  `json:"id"`
	Mode      string `json:"mode"`
	StartedAt int64  `json:"started_at"`
}

type playerReply struct {
	ServerTime    int64              `json:"server_time"`
	Player        adminPlayer        `json:"player"`
	PendingBattle *pendingBattleView `json:"pending_battle,omitempty"`
	Profile       json.RawMessage    `json:"profile,omitempty"`
}

func (s *Server) adminPlayer(w http.ResponseWriter, r *http.Request) {
	playerID := r.PathValue("player")
	players, err := s.adminDirectory()
	if err != nil {
		s.log.Error("admin player", "player", playerID, "error", err)
		writeError(w, http.StatusInternalServerError, "could not read players")
		return
	}
	found := slices.IndexFunc(players, func(p adminPlayer) bool { return p.PlayerID == playerID })
	if found < 0 {
		writeError(w, http.StatusNotFound, "no such player")
		return
	}
	reply := playerReply{ServerTime: s.now().Unix(), Player: players[found]}
	record, err := s.profiles.Get(playerID)
	switch {
	case errors.Is(err, profile.ErrNotFound):
	case err != nil:
		s.log.Error("admin player", "player", playerID, "error", err)
		writeError(w, http.StatusInternalServerError, "could not read profile")
		return
	default:
		reply.Player.describeProfile(profile.Summarize(record))
		reply.Player.LastActiveAt = max(reply.Player.LastActiveAt, reply.Player.UpdatedAt)
		reply.Profile = record.Profile
		if pending := record.Server.Battle; pending != nil {
			reply.PendingBattle = &pendingBattleView{ID: pending.ID, Mode: pending.Mode, StartedAt: pending.StartedAt}
		}
	}
	writeJSON(w, http.StatusOK, reply)
}

type passwordResetReply struct {
	PlayerID string `json:"player_id"`
	Login    string `json:"login"`
	Password string `json:"password"`
}

func (s *Server) adminResetPassword(w http.ResponseWriter, r *http.Request) {
	reset, err := s.accounts.ResetPassword(r.PathValue("login"))
	if errors.Is(err, account.ErrUnknownLogin) {
		writeError(w, http.StatusNotFound, "no account has this login")
		return
	}
	if err != nil {
		s.log.Error("admin password reset", "error", err)
		writeError(w, http.StatusInternalServerError, "could not reset the password")
		return
	}
	s.sessions.CloseAll(reset.PlayerID)
	s.log.Info("operator reset a password", "player", reset.PlayerID)
	writeJSON(w, http.StatusOK, passwordResetReply{PlayerID: reset.PlayerID, Login: reset.Login, Password: reset.Password})
}
