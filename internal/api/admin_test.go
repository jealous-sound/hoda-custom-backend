package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/jealous-sound/hoda-custom-backend/internal/activity"
	"github.com/jealous-sound/hoda-custom-backend/internal/clientversion"
	"github.com/jealous-sound/hoda-custom-backend/internal/guild"
	"github.com/jealous-sound/hoda-custom-backend/internal/ratelimit"
	"github.com/jealous-sound/hoda-custom-backend/internal/report"
	"github.com/jealous-sound/hoda-custom-backend/internal/traffic"
)

const playedOnClient = "v0.6.1"

var adminReads = []string{
	adminOverviewPath, adminActivityPath, adminTrafficPath, adminPlayersPath, adminPlayersPath + "/local",
	adminGuildsPath, adminGuildsPath + "/guild-1", adminWorldChatPath, adminReportsPath,
}

func (h *harness) adminRead(path string, into any) {
	h.t.Helper()
	response, body := h.do("GET", path, testAdminToken, "", "")
	expectStatus(h.t, response, body, http.StatusOK)
	if err := json.Unmarshal([]byte(body), into); err != nil {
		h.t.Fatalf("decode %s: %v", body, err)
	}
}

func (h *harness) overview() overviewReply {
	h.t.Helper()
	var reply overviewReply
	h.adminRead(adminOverviewPath, &reply)
	return reply
}

func (h *harness) listedPlayers() map[string]adminPlayer {
	h.t.Helper()
	var reply playersReply
	h.adminRead(adminPlayersPath, &reply)
	players := map[string]adminPlayer{}
	for _, player := range reply.Players {
		players[player.PlayerID] = player
	}
	return players
}

func (h *harness) pass(wait time.Duration) {
	*h.now = h.now.Add(wait)
}

func (h *harness) playOn(player, token, client string) {
	h.t.Helper()
	headers := map[string]string{}
	if client != "" {
		headers["User-Agent"] = clientversion.UserAgentProduct + client
	}
	response, body := h.doFrom("GET", "/v1/players/"+player+"/profile", token, "", headers)
	expectStatus(h.t, response, body, http.StatusOK)
}

func TestAdminReadsAreAbsentWithoutAConfiguredToken(t *testing.T) {
	h := newHarness(t)
	for _, path := range adminReads {
		response, body := h.do("GET", path, testAdminToken, "", "")
		expectStatus(t, response, body, http.StatusNotFound)
	}
}

func TestAdminReadsNeedTheToken(t *testing.T) {
	h := newMaintenanceHarness(t)
	for _, path := range adminReads {
		response, body := h.do("GET", path, "", "", "")
		expectStatus(t, response, body, http.StatusUnauthorized)
	}
}

func TestTheRightAdminTokenIsNeverLimited(t *testing.T) {
	h := newHarnessWith(t, Config{
		OpenUnregistered: true, AdminToken: testAdminToken,
		Limits: Limits{AdminGuessesPerAddress: ratelimit.Rule{Burst: 2, Every: time.Minute}},
	})
	for range 20 {
		response, body := h.do("GET", adminOverviewPath, testAdminToken, "", "")
		expectStatus(t, response, body, http.StatusOK)
	}
}

func TestOverviewCountsWhoIsOnlineByClientVersion(t *testing.T) {
	h := newMaintenanceHarness(t)
	hawke := h.guildPlayer("local", "Hawke", 9, 100)
	varric := h.guildPlayer("other", "Varric", 7, 100)
	h.playOn("local", hawke, playedOnClient)
	h.playOn("other", varric, "")

	overview := h.overview()
	wantClients := []clientCount{{Client: activity.UnknownClient, Count: 1}, {Client: playedOnClient, Count: 1}}
	if overview.Online.Count != 2 || len(overview.Online.Clients) != 2 ||
		overview.Online.Clients[0] != wantClients[0] || overview.Online.Clients[1] != wantClients[1] {
		t.Fatalf("online = %+v", overview.Online)
	}
	if overview.Today.Players != 2 || overview.Players.Profiles != 2 || overview.Players.Named != 2 ||
		overview.Players.Active24h != 2 {
		t.Fatalf("today = %+v players = %+v", overview.Today, overview.Players)
	}
	if overview.Server.StartedAt != fixedNow.Unix() || overview.Server.OpenSessions != 2 ||
		overview.Server.DataFiles == 0 || overview.Server.DataBytes == 0 {
		t.Fatalf("server = %+v", overview.Server)
	}

	h.pass(activity.OnlineWindow - time.Second)
	h.playOn("other", varric, "")
	h.pass(time.Second)
	overview = h.overview()
	if overview.Online.Count != 1 || overview.Online.Players[0].PlayerID != "other" ||
		overview.Online.Players[0].Name != "Varric" {
		t.Fatalf("online after Hawke went quiet = %+v", overview.Online)
	}
}

func TestOverviewCountsSignupsAndAnnouncedMaintenance(t *testing.T) {
	h := newMaintenanceHarness(t)
	h.createGuest()
	h.createGuest()
	at := h.scheduleIn(30 * time.Minute)
	overview := h.overview()
	if overview.Today.Signups != 2 || overview.Players.Accounts != 2 || overview.Players.Created24h != 2 ||
		overview.Players.Profiles != 0 {
		t.Fatalf("today = %+v players = %+v", overview.Today, overview.Players)
	}
	if overview.MaintenanceAt != at.Unix() || overview.ServerTime != h.now.Unix() {
		t.Fatalf("maintenance_at = %d, want %d", overview.MaintenanceAt, at.Unix())
	}
}

func TestPlayersListJoinsAccountProfileGuildAndPresence(t *testing.T) {
	h := newMaintenanceHarness(t)
	guest := h.createGuest()
	hawke := h.guildPlayer("local", "Hawke", 9, 12000)
	response, body := h.do("POST", "/v1/players/local/guilds", hawke, "1", createChampions)
	expectStatus(t, response, body, http.StatusOK)
	h.playOn("local", hawke, playedOnClient)

	players := h.listedPlayers()
	leader := players["local"]
	if leader.Name != "Hawke" || leader.Level != 9 || leader.Gold != 2000 || leader.Heroes != 1 ||
		leader.GuildName != "Champions" || !leader.Online || leader.Client != playedOnClient ||
		leader.Registered || !leader.HasProfile || leader.Revision != 2 || leader.LastActiveAt != h.now.Unix() {
		t.Fatalf("leader = %+v", leader)
	}
	newcomer := players[guest.PlayerID]
	if !newcomer.Registered || newcomer.HasProfile || newcomer.Online || newcomer.CreatedAt != h.now.Unix() {
		t.Fatalf("newcomer = %+v", newcomer)
	}
}

func TestPlayerDetailCarriesTheStoredProfile(t *testing.T) {
	h := newMaintenanceHarness(t)
	h.guildPlayer("local", "Hawke", 9, 12000)
	var detail playerReply
	h.adminRead(adminPlayersPath+"/local", &detail)
	if detail.Player.Name != "Hawke" || !strings.Contains(string(detail.Profile), `"name":"Hawke"`) {
		t.Fatalf("detail = %+v %s", detail.Player, detail.Profile)
	}
	response, body := h.do("GET", adminPlayersPath+"/nobody", testAdminToken, "", "")
	expectStatus(t, response, body, http.StatusNotFound)
}

func TestGuildsWorldChatAndReportsCanBeReadByTheAdmin(t *testing.T) {
	h := newMaintenanceHarness(t)
	hawke := h.guildPlayer("local", "Hawke", 9, 12000)
	varric := h.guildPlayer("other", "Varric", 7, 0)
	response, body := h.do("POST", "/v1/players/local/guilds", hawke, "1", createChampions)
	expectStatus(t, response, body, http.StatusOK)
	response, _, body = h.world("other", varric, `{"text":"buy gold at my site"}`)
	expectStatus(t, response, body, http.StatusOK)
	response, body = h.do("POST", "/v1/players/local/reports", hawke, "",
		`{"kind":"world_message","abuse_type":"OTHER","message_id":1}`)
	expectStatus(t, response, body, http.StatusOK)

	var guilds guildsReply
	h.adminRead(adminGuildsPath, &guilds)
	if len(guilds.Guilds) != 1 || guilds.Guilds[0].Name != "Champions" || guilds.Guilds[0].LeaderName != "Hawke" ||
		guilds.Guilds[0].MemberCount != 1 {
		t.Fatalf("guilds = %+v", guilds.Guilds)
	}
	var detail guildReply
	h.adminRead(adminGuildsPath+"/"+guilds.Guilds[0].ID, &detail)
	if len(detail.Members) != 1 || detail.Members[0].Rank != guild.RankLeader || len(detail.Messages) == 0 {
		t.Fatalf("guild detail = %+v", detail)
	}
	response, body = h.do("GET", adminGuildsPath+"/guild-missing", testAdminToken, "", "")
	expectStatus(t, response, body, http.StatusNotFound)

	var chat worldChatReply
	h.adminRead(adminWorldChatPath, &chat)
	if len(chat.Messages) != 1 || chat.Messages[0].Text != "buy gold at my site" ||
		chat.Messages[0].PlayerID != "other" {
		t.Fatalf("world chat = %+v", chat.Messages)
	}
	var reports reportsReply
	h.adminRead(adminReportsPath, &reports)
	if reports.Total != 1 || reports.Reports[0].Reporter != "local" || reports.Reports[0].IssueType != report.Other ||
		!strings.Contains(reports.Reports[0].Description, "buy gold at my site") {
		t.Fatalf("reports = %+v", reports)
	}
	if overview := h.overview(); overview.Reports != 1 || overview.Guilds.Guilds != 1 ||
		overview.Guilds.WorldMessages != 1 || overview.Players.InGuild != 1 {
		t.Fatalf("overview = %+v", overview)
	}
}

func TestTrafficCountsPlayerRoutesAndRefusedActionsButNotThePanel(t *testing.T) {
	h := newMaintenanceHarness(t)
	token := h.shopPlayer()
	unaffordable := `{"request_id":"buy-too-much","pack":"recruit","currency":"gold","price":350000}`
	response, body := h.do("POST", "/v1/players/local/store/purchases", token, "1", unaffordable)
	expectStatus(t, response, body, http.StatusUnprocessableEntity)
	response, body = h.do("POST", "/v1/players/local/store/purchases", token, "1", recruitPurchase)
	expectStatus(t, response, body, http.StatusOK)
	response, body = h.do("GET", "/wp-login.php", "", "", "")
	expectStatus(t, response, body, http.StatusNotFound)

	var reply trafficReply
	h.adminRead(adminTrafficPath, &reply)
	h.adminRead(adminTrafficPath, &reply)
	routes := map[string]traffic.Route{}
	for _, route := range reply.Routes {
		routes[route.Route] = route
	}
	purchases := routes["POST /v1/players/{player}/store/purchases"]
	if purchases.Requests != 2 || purchases.ClientErrors != 1 || routes[unmatchedRoute].Requests != 1 {
		t.Fatalf("routes = %+v", reply.Routes)
	}
	for route := range routes {
		if strings.Contains(route, adminPathPrefix) {
			t.Fatalf("the panel's own reads were counted: %+v", reply.Routes)
		}
	}
	if len(reply.Actions) != 1 || reply.Actions[0] != (traffic.Action{Action: purchaseAction, Count: 2, Refused: 1}) {
		t.Fatalf("actions = %+v", reply.Actions)
	}
	if len(reply.Refusals) != 1 || reply.Refusals[0].PlayerID != "local" || reply.Refusals[0].Code == "" ||
		reply.Refusals[0].Code != reply.RefusalCodes[0].Code {
		t.Fatalf("refusals = %+v codes = %+v", reply.Refusals, reply.RefusalCodes)
	}
	total := 0
	for _, minute := range reply.Minutes {
		total += minute.Requests
	}
	if total != 4 {
		t.Fatalf("minutes = %+v, want the session, both purchases and the stray request", reply.Minutes)
	}
}

func TestActivityReportsTodayAndTheBucketLength(t *testing.T) {
	h := newMaintenanceHarness(t)
	token := h.guildPlayer("local", "Hawke", 9, 0)
	h.playOn("local", token, playedOnClient)
	var reply activityReply
	h.adminRead(adminActivityPath, &reply)
	if reply.BucketSeconds != int64(activity.BucketLength/time.Second) || len(reply.Days) != 1 ||
		reply.Days[0].Players != 1 || reply.Days[0].Date != fixedNow.UTC().Format(time.DateOnly) {
		t.Fatalf("activity = %+v", reply)
	}
}

var adminListReads = []string{
	adminOverviewPath, adminActivityPath, adminTrafficPath, adminPlayersPath, adminGuildsPath, adminWorldChatPath,
	adminReportsPath,
}

func TestAnEmptyServerAnswersAdminReadsWithEmptyListsNotNull(t *testing.T) {
	h := newMaintenanceHarness(t)
	for _, path := range adminListReads {
		response, body := h.do("GET", path, testAdminToken, "", "")
		expectStatus(t, response, body, http.StatusOK)
		if strings.Contains(body, "null") {
			t.Errorf("%s answered with a null: %s", path, body)
		}
	}
}
