package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/jealous-sound/hoda-custom-backend/internal/ratelimit"
)

const worldMessagesPath = "/chat/world/messages"

type worldResult struct {
	Messages []struct {
		ID         int64  `json:"id"`
		Text       string `json:"text"`
		PlayerName string `json:"player_name"`
		Level      int    `json:"player_level"`
		GuildName  string `json:"guild_name"`
	} `json:"messages"`
	LastMessageID int64 `json:"last_message_id"`
}

func (h *harness) world(player, token, body string) (*http.Response, worldResult, string) {
	h.t.Helper()
	response, raw := h.do("POST", "/v1/players/"+player+worldMessagesPath, token, "", body)
	var result worldResult
	if response.StatusCode == http.StatusOK {
		if err := json.Unmarshal([]byte(raw), &result); err != nil {
			h.t.Fatalf("%v: %s", err, raw)
		}
	}
	return response, result, raw
}

func TestWorldChatReachesPlayersInAndOutOfGuilds(t *testing.T) {
	h := newHarness(t)
	hawke := h.guildPlayer("local", "Hawke", 9, 12000)
	response, body := h.do("POST", "/v1/players/local/guilds", hawke, "1", createChampions)
	expectStatus(t, response, body, http.StatusOK)
	varric := h.guildPlayer("other", "Varric", 7, 0)

	response, said, body := h.world("other", varric, `{"text":"anyone for the arena?"}`)
	expectStatus(t, response, body, http.StatusOK)
	if len(said.Messages) != 1 || said.Messages[0].PlayerName != "Varric" || said.Messages[0].GuildName != "" {
		t.Fatalf("body = %s", body)
	}
	response, said, body = h.world("local", hawke, `{"after":1,"text":"Champions are recruiting"}`)
	expectStatus(t, response, body, http.StatusOK)
	if len(said.Messages) != 1 || said.Messages[0].GuildName != "Champions" || said.LastMessageID != 2 {
		t.Fatalf("body = %s", body)
	}
	response, read, body := h.world("other", varric, `{"after":0}`)
	expectStatus(t, response, body, http.StatusOK)
	if len(read.Messages) != 2 || read.Messages[1].Text != "Champions are recruiting" || read.Messages[1].Level != 9 {
		t.Fatalf("body = %s", body)
	}
	response, read, body = h.world("other", varric, `{"after":2}`)
	expectStatus(t, response, body, http.StatusOK)
	if len(read.Messages) != 0 || !strings.Contains(body, `"messages":[]`) {
		t.Fatalf("body = %s", body)
	}
}

func TestWorldChatLetsLowLevelsReadButNotWrite(t *testing.T) {
	h := newHarness(t)
	sandal := h.guildPlayer("local", "Sandal", 3, 0)
	response, _, body := h.world("local", sandal, `{"text":"enchantment!"}`)
	expectStatus(t, response, body, http.StatusUnprocessableEntity)
	if !strings.Contains(body, `"code":"CHAT_LEVEL_TOO_LOW"`) {
		t.Fatalf("body = %s", body)
	}
	response, read, body := h.world("local", sandal, `{"after":0}`)
	expectStatus(t, response, body, http.StatusOK)
	if len(read.Messages) != 0 {
		t.Fatalf("body = %s", body)
	}
}

func TestWorldChatPostsAreLimitedButPollingIsNot(t *testing.T) {
	h := newHarnessWith(t, Config{
		OpenUnregistered: true, Limits: Limits{WorldChatPerPlayer: ratelimit.Rule{Burst: 1, Every: time.Hour}},
	})
	token := h.guildPlayer("local", "Hawke", 9, 0)
	response, _, body := h.world("local", token, `{"text":"hello"}`)
	expectStatus(t, response, body, http.StatusOK)
	response, _, body = h.world("local", token, `{"text":"again"}`)
	expectTooMany(t, response, body)
	response, _, body = h.world("local", token, `{"after":0}`)
	expectStatus(t, response, body, http.StatusOK)
}
