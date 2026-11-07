package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

func guildProfile(name string, level, gold int) string {
	return fmt.Sprintf(`{"version":9,"account":{"name":%q,"level":%d,"experience":10,"energy":5,"energy_max":30,`+
		`"stamina":8,"stamina_max":8,"gold":%d,"gems":7},"roster":{"starters":[],"additions":[{"id":%q,`+
		`"instance_id":%d,"tier":%q,"level":%d}]},"squads":[]}`, name, level, gold, guildHeroID, guildHeroInstance,
		guildHeroTier, guildHeroLevel)
}

func (h *harness) guildPlayer(id, name string, level, gold int) string {
	h.t.Helper()
	token := h.open(id)
	if _, err := h.profiles.Put(id, 0, json.RawMessage(guildProfile(name, level, gold))); err != nil {
		h.t.Fatal(err)
	}
	return token
}

type guildStateResult struct {
	Rank  int `json:"rank"`
	Guild *struct {
		ID              string `json:"id"`
		Name            string `json:"name"`
		MemberCount     int    `json:"member_count"`
		PendingRequests []struct {
			ID string `json:"id"`
		} `json:"pending_requests"`
		LastMessageID int64 `json:"last_message_id"`
	} `json:"guild"`
	Applications []struct {
		ID string `json:"id"`
	} `json:"applications"`
	Notices []struct {
		Kind string `json:"kind"`
	} `json:"notices"`
}

func decodeGuildState(t *testing.T, body string) guildStateResult {
	t.Helper()
	var result guildStateResult
	if err := json.Unmarshal([]byte(body), &result); err != nil {
		t.Fatalf("%v: %s", err, body)
	}
	return result
}

const createChampions = `{"request_id":"create-guild-1","name":"Champions","description":"Kirkwall",` +
	`"message_of_the_day":"","language":"en","status":1,"icon":3,"required_level":5}`

func TestCreatingAGuildChargesGoldAndMakesTheLeader(t *testing.T) {
	h := newHarness(t)
	poor := h.guildPlayer("poor", "Bodahn", 9, 9999)
	response, body := h.do("POST", "/v1/players/poor/guilds", poor, "1", createChampions)
	expectStatus(t, response, body, http.StatusUnprocessableEntity)
	if !strings.Contains(body, `"code":"cant_afford"`) {
		t.Fatalf("body = %s", body)
	}

	token := h.guildPlayer("local", "Hawke", 9, 12000)
	response, body = h.do("POST", "/v1/players/local/guilds", token, "1", createChampions)
	expectStatus(t, response, body, http.StatusOK)
	var created struct {
		Revision int64 `json:"revision"`
		Profile  struct {
			Account struct{ Gold int }
		}
		Granted guildStateResult
	}
	if err := json.Unmarshal([]byte(body), &created); err != nil {
		t.Fatal(err)
	}
	if created.Profile.Account.Gold != 2000 || created.Revision != 2 {
		t.Fatalf("gold %d revision %d", created.Profile.Account.Gold, created.Revision)
	}
	if created.Granted.Rank != 2 || created.Granted.Guild == nil || created.Granted.Guild.Name != "Champions" {
		t.Fatalf("granted %+v", created.Granted)
	}

	other := h.guildPlayer("other", "Isabela", 9, 20000)
	duplicate := strings.Replace(createChampions, "create-guild-1", "create-guild-2", 1)
	response, body = h.do("POST", "/v1/players/other/guilds", other, "1", duplicate)
	expectStatus(t, response, body, http.StatusUnprocessableEntity)
	if !strings.Contains(body, `"code":"GUILD_NAME_NOT_UNIQUE"`) {
		t.Fatalf("body = %s", body)
	}
}

func TestApplyAcceptAndChat(t *testing.T) {
	h := newHarness(t)
	leader := h.guildPlayer("local", "Hawke", 9, 12000)
	response, body := h.do("POST", "/v1/players/local/guilds", leader, "1", createChampions)
	expectStatus(t, response, body, http.StatusOK)
	var created struct{ Granted guildStateResult }
	if err := json.Unmarshal([]byte(body), &created); err != nil {
		t.Fatal(err)
	}
	guildID := created.Granted.Guild.ID

	applicant := h.guildPlayer("applicant", "Merrill", 7, 0)
	response, body = h.do("GET", "/v1/players/applicant/guilds/recommended", applicant, "", "")
	expectStatus(t, response, body, http.StatusOK)
	if !strings.Contains(body, guildID) {
		t.Fatalf("recommended = %s", body)
	}
	response, body = h.do("GET", "/v1/players/applicant/guilds/search?name=champions", applicant, "", "")
	expectStatus(t, response, body, http.StatusOK)
	if !strings.Contains(body, guildID) {
		t.Fatalf("search = %s", body)
	}
	response, body = h.do("POST", "/v1/players/applicant/guilds/"+guildID+"/join", applicant, "",
		`{"application_text":"Elf mage"}`)
	expectStatus(t, response, body, http.StatusOK)
	if state := decodeGuildState(t, body); state.Guild != nil || len(state.Applications) != 1 {
		t.Fatalf("applicant state %+v", state)
	}

	response, body = h.do("GET", "/v1/players/local/guild", leader, "", "")
	expectStatus(t, response, body, http.StatusOK)
	if state := decodeGuildState(t, body); len(state.Guild.PendingRequests) != 1 {
		t.Fatalf("leader state %+v", state)
	}
	response, body = h.do("POST", "/v1/players/local/guild/applications/applicant/accept", leader, "", "")
	expectStatus(t, response, body, http.StatusOK)

	response, body = h.do("GET", "/v1/players/applicant/guild", applicant, "", "")
	expectStatus(t, response, body, http.StatusOK)
	joined := decodeGuildState(t, body)
	if joined.Rank != 0 || joined.Guild == nil || len(joined.Notices) != 1 || joined.Notices[0].Kind != "accepted" {
		t.Fatalf("joined state %+v", joined)
	}

	response, body = h.do("POST", "/v1/players/applicant/guild/messages", applicant, "",
		fmt.Sprintf(`{"after":%d,"text":"Hello, Hawke"}`, joined.Guild.LastMessageID))
	expectStatus(t, response, body, http.StatusOK)
	var chat guildMessagesReply
	if err := json.Unmarshal([]byte(body), &chat); err != nil {
		t.Fatal(err)
	}
	if len(chat.Messages) != 1 || chat.Messages[0].Text != "Hello, Hawke" || chat.Messages[0].PlayerName != "Merrill" {
		t.Fatalf("chat = %s", body)
	}

	response, body = h.do("POST", "/v1/players/local/guild/members/applicant/rank", leader, "", `{"target_rank":2}`)
	expectStatus(t, response, body, http.StatusUnprocessableEntity)
	if !strings.Contains(body, `"code":"INVALID_ARGUMENT"`) {
		t.Fatalf("body = %s", body)
	}
	response, body = h.do("POST", "/v1/players/local/guild/leave", leader, "", "")
	expectStatus(t, response, body, http.StatusUnprocessableEntity)
	if !strings.Contains(body, `"code":"LEADER_CANT_LEAVE_NON_EMPTY_GUILD"`) {
		t.Fatalf("body = %s", body)
	}
	response, body = h.do("POST", "/v1/players/local/guild/members/applicant/kick", leader, "", "")
	expectStatus(t, response, body, http.StatusOK)
	if state := decodeGuildState(t, body); state.Guild.MemberCount != 1 {
		t.Fatalf("after kick %+v", state)
	}
}

func TestObservingAnotherGuildHidesItsApplications(t *testing.T) {
	h := newHarness(t)
	leader := h.guildPlayer("local", "Hawke", 9, 12000)
	response, body := h.do("POST", "/v1/players/local/guilds", leader, "1", createChampions)
	expectStatus(t, response, body, http.StatusOK)
	var created struct{ Granted guildStateResult }
	if err := json.Unmarshal([]byte(body), &created); err != nil {
		t.Fatal(err)
	}
	guildID := created.Granted.Guild.ID
	applicant := h.guildPlayer("applicant", "Merrill", 7, 0)
	response, body = h.do("POST", "/v1/players/applicant/guilds/"+guildID+"/join", applicant, "", "")
	expectStatus(t, response, body, http.StatusOK)
	response, body = h.do("GET", "/v1/players/applicant/guilds/"+guildID, applicant, "", "")
	expectStatus(t, response, body, http.StatusOK)
	if !strings.Contains(body, `"applied":true`) || !strings.Contains(body, `"pending_requests":[]`) {
		t.Fatalf("observed = %s", body)
	}
	response, body = h.do("GET", "/v1/players/applicant/guilds/guild-missing", applicant, "", "")
	expectStatus(t, response, body, http.StatusNotFound)
}
