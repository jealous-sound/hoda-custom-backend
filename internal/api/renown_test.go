package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/jealous-sound/hoda-custom-backend/internal/battle"
	"github.com/jealous-sound/hoda-custom-backend/internal/catalog"
	"github.com/jealous-sound/hoda-custom-backend/internal/economy"
)

const (
	firstRankXP         = 540
	levelCapXP          = 50490
	sixthRankLevel      = 60
	sixthRankXP         = 18290
	sixthRankLevelEndXP = 18900
	secondCapRankXP     = 61040
	xpInsideFirstRank   = 60000
)

type renownProfile struct {
	Account struct {
		Level         int   `json:"level"`
		Experience    int64 `json:"experience"`
		ExperienceMin int64 `json:"experience_min"`
		ExperienceMax int64 `json:"experience_max"`
		Energy        int64 `json:"energy"`
		EnergyMax     int64 `json:"energy_max"`
		StaminaMax    int64 `json:"stamina_max"`
	} `json:"account"`
	Renown struct {
		Perks map[string]int `json:"perks"`
	} `json:"renown"`
}

func editedProfile(t *testing.T, profile string, edit func(doc map[string]any)) string {
	t.Helper()
	var doc map[string]any
	if err := json.Unmarshal([]byte(profile), &doc); err != nil {
		t.Fatal(err)
	}
	edit(doc)
	edited, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	return string(edited)
}

func withPerks(perks map[string]any) func(doc map[string]any) {
	return func(doc map[string]any) { doc["renown"] = map[string]any{"perks": perks} }
}

func withExperience(level int, experience, low, high int64) func(doc map[string]any) {
	return func(doc map[string]any) {
		account := doc["account"].(map[string]any)
		account["level"], account["experience"] = level, experience
		account["experience_min"], account["experience_max"] = low, high
	}
}

func (h *harness) storedAs(player, profile string) string {
	h.t.Helper()
	token := h.open(player)
	if _, err := h.profiles.Put(player, 0, json.RawMessage(profile)); err != nil {
		h.t.Fatal(err)
	}
	return token
}

func (h *harness) storedRenown(player, token string) renownProfile {
	h.t.Helper()
	response, body := h.do("GET", "/v1/players/"+player+"/profile", token, "", "")
	expectStatus(h.t, response, body, http.StatusOK)
	var stored renownProfile
	if err := json.Unmarshal([]byte(body), &stored); err != nil {
		h.t.Fatal(err)
	}
	return stored
}

func upgradeRenown(id, perk string) string {
	return fmt.Sprintf(`{"request_id":%q,"action":%q,"perk":%q}`, id, economy.ActionRenownUpgrade, perk)
}

func TestRenownUpgradeIsAnActionThatReturnsTheProfile(t *testing.T) {
	h := newHarness(t)
	const level = 10
	token := h.storedAs("local", editedProfile(t, guildProfile("Hawke", level, 10),
		withExperience(level, firstRankXP, 440, 540)))
	rules := h.catalog().Renown

	response, body := h.do("POST", "/v1/players/local/actions", token, "1",
		upgradeRenown("renown-1", catalog.RenownEnergyMax))
	expectStatus(t, response, body, http.StatusOK)
	var reply struct {
		Revision int64         `json:"revision"`
		Profile  renownProfile `json:"profile"`
	}
	if err := json.Unmarshal([]byte(body), &reply); err != nil {
		t.Fatal(err)
	}
	wantEnergyMax := int64(economy.StartEnergyMax + level - 1 + rules.PerkValue(catalog.RenownEnergyMax, 1))
	upgraded := reply.Profile
	if reply.Revision != 2 || len(upgraded.Renown.Perks) != 1 || upgraded.Renown.Perks[catalog.RenownEnergyMax] != 1 ||
		upgraded.Account.EnergyMax != wantEnergyMax || upgraded.Account.Energy != 5 ||
		upgraded.Account.StaminaMax != economy.StartStaminaMax {
		t.Fatalf("reply = %s, want energy max %d from one perk rank", body, wantEnergyMax)
	}
	if stored := h.storedRenown("local", token); stored.Renown.Perks[catalog.RenownEnergyMax] != 1 ||
		stored.Account.EnergyMax != wantEnergyMax {
		t.Fatalf("stored profile = %+v", stored)
	}

	refused := map[string]string{
		upgradeRenown("renown-2", catalog.RenownGoldPct): `"code":"cant_afford"`,
		upgradeRenown("renown-3", "luck_pct"):            `"code":"unavailable"`,
	}
	for request, code := range refused {
		response, body = h.do("POST", "/v1/players/local/actions", token, "2", request)
		expectStatus(t, response, body, http.StatusUnprocessableEntity)
		if !strings.Contains(body, code) || response.Header.Get(RevisionHeader) != "2" {
			t.Errorf("%s was answered %s", request, body)
		}
	}

	maxed := h.storedAs("maxed", editedProfile(t, editedProfile(t, guildProfile("Varric", economy.MaxPlayerLevel, 10),
		withExperience(economy.MaxPlayerLevel, levelCapXP, levelCapXP, secondCapRankXP)),
		withPerks(map[string]any{catalog.RenownPowerPct: rules.PerkMaxRank})))
	response, body = h.do("POST", "/v1/players/maxed/actions", maxed, "1",
		upgradeRenown("renown-4", catalog.RenownPowerPct))
	expectStatus(t, response, body, http.StatusUnprocessableEntity)
	if !strings.Contains(body, `"code":"already_full"`) {
		t.Errorf("a full perk was answered %s", body)
	}
}

func renownedDefender(t *testing.T, profile string) (string, *battle.SideRenown) {
	t.Helper()
	const powerRank, critRank = 2, 1
	rules := unlevelledCatalog(t).Renown
	perks := withPerks(map[string]any{
		catalog.RenownPowerPct: powerRank, catalog.RenownCritPermille: critRank, catalog.RenownGoldPct: 3,
	})
	earned := withExperience(sixthRankLevel, sixthRankXP, sixthRankXP, sixthRankLevelEndXP)
	return editedProfile(t, editedProfile(t, profile, earned), perks), battle.NewSideRenown(
		rules.PerkValue(catalog.RenownPowerPct, powerRank), 0, rules.PerkValue(catalog.RenownCritPermille, critRank),
	)
}

func (h *harness) expectDuelWithRenown(token string, revision int64, route, defender string, want *battle.SideRenown) {
	h.t.Helper()
	response, body := h.do("GET", route, token, "", "")
	expectStatus(h.t, response, body, http.StatusOK)
	var payload struct {
		Renown *battle.SideRenown `json:"renown"`
	}
	if err := json.Unmarshal([]byte(body), &payload); err != nil {
		h.t.Fatal(err)
	}
	onTheWire := fmt.Sprintf(`"renown":{"power_pct":%d,"health_pct":%d,"crit_permille":%d}`, want.PowerPct,
		want.HealthPct, want.CritPermille)
	if payload.Renown == nil || *payload.Renown != *want || !strings.Contains(body, onTheWire) {
		h.t.Fatalf("%s answered %s, want %s at the top level", route, body, onTheWire)
	}
	enemy := duelSide(h.t, body)
	if enemy.Renown == nil || *enemy.Renown != *want {
		h.t.Fatalf("the payload read as a side fields renown %+v, want %+v", enemy.Renown, want)
	}

	stripped := enemy
	stripped.Renown = nil
	start, _ := json.Marshal(map[string]any{
		"request_id": "stripped-" + defender, "action": economy.ActionBattleStart, "mode": economy.BattlePvp,
		"duel": defender, "squad": h.storedSquad("local"), "enemy": stripped,
	})
	response, body = h.do("POST", "/v1/players/local/actions", token, fmt.Sprint(revision), string(start))
	expectStatus(h.t, response, body, http.StatusUnprocessableEntity)
	if !strings.Contains(body, `"code":"opponent_changed"`) {
		h.t.Fatalf("a defender stripped of its renown was answered %s", body)
	}
	fought := h.battle("local", token, revision, "renowned-"+defender, economy.BattleStartRequest{
		Mode: economy.BattlePvp, Duel: defender, Enemy: enemy,
	})
	if fought.Granted.Gold <= 0 {
		h.t.Fatalf("the duel against %s paid %+v", defender, fought.Granted)
	}
}

func TestAGuildDuelPayloadCarriesTheDefendersRenown(t *testing.T) {
	h := newHarness(t)
	leader, ally, _ := h.guildWithAlly(t)
	route := "/v1/players/local/guild/members/ally/duel"

	if _, err := h.profiles.Put("ally", 1, json.RawMessage(duelistProfile)); err != nil {
		t.Fatal(err)
	}
	h.guilds(t, "ally", ally)
	response, body := h.do("GET", route, leader, "", "")
	expectStatus(t, response, body, http.StatusOK)
	if strings.Contains(body, "renown") {
		t.Fatalf("a defender without perks is sent as %s", body)
	}

	renowned, want := renownedDefender(t, duelistProfile)
	if _, err := h.profiles.Put("ally", 2, json.RawMessage(renowned)); err != nil {
		t.Fatal(err)
	}
	h.expectDuelWithRenown(leader, 2, route, "ally", want)
}

func TestALadderRivalPayloadCarriesTheDefendersRenown(t *testing.T) {
	h := newHarness(t)
	challenger := h.guildPlayer("local", "Hawke", 9, 12000)
	renowned, want := renownedDefender(t, rankedProfile("Isabela", rivalTrophies, 4))
	h.storedAs("rival", renowned)
	h.storedPlayer("plain", "Merrill", rivalTrophies, 2)

	h.expectDuelWithRenown(challenger, 1, "/v1/players/local/pvp/ladder/rival", "rival", want)
	response, body := h.do("GET", "/v1/players/local/pvp/ladder/plain", challenger, "", "")
	expectStatus(t, response, body, http.StatusOK)
	if strings.Contains(body, "renown") {
		t.Fatalf("a rival without perks is sent as %s", body)
	}
}

func TestAnUploadIsNeverRefusedForExperienceBoundsOrRenown(t *testing.T) {
	h := newHarness(t)
	earned := map[string]any{catalog.RenownEnergyMax: 1}
	capped := guildProfile("Hawke", economy.MaxPlayerLevel, 10)
	earnedEnergyMax := int64(economy.StartEnergyMax + economy.MaxPlayerLevel - 1 +
		h.catalog().Renown.PerkValue(catalog.RenownEnergyMax, 1))
	stored := editedProfile(t, capped, func(doc map[string]any) {
		withExperience(economy.MaxPlayerLevel, xpInsideFirstRank, levelCapXP, secondCapRankXP)(doc)
		withPerks(earned)(doc)
		doc["account"].(map[string]any)["energy_max"] = earnedEnergyMax
	})
	token := h.storedAs("local", stored)
	fromAClientWithoutTheTable := withExperience(economy.MaxPlayerLevel, xpInsideFirstRank, levelCapXP, levelCapXP)

	uploads := []struct {
		name   string
		renown func(doc map[string]any)
	}{
		{"the section it loaded", withPerks(earned)},
		{"forged perks", withPerks(map[string]any{catalog.RenownPowerPct: 5, catalog.RenownEnergyMax: 5})},
		{"no section", func(doc map[string]any) { delete(doc, "renown") }},
		{"a section of null", func(doc map[string]any) { doc["renown"] = nil }},
		{"a section of text", func(doc map[string]any) { doc["renown"] = "max" }},
		{"listed perks", func(doc map[string]any) { doc["renown"] = map[string]any{"perks": []any{5}} }},
		{"named ranks", withPerks(map[string]any{catalog.RenownPowerPct: "five"})},
	}
	revision := 1
	for _, upload := range uploads {
		body := editedProfile(t, capped, func(doc map[string]any) {
			fromAClientWithoutTheTable(doc)
			upload.renown(doc)
			account := doc["account"].(map[string]any)
			account["energy_max"], account["stamina_max"] = 999, 99
		})
		response, reply := h.do("PUT", "/v1/players/local/profile", token, fmt.Sprint(revision), body)
		expectStatus(t, response, reply, http.StatusOK)
		revision++
		kept := h.storedRenown("local", token)
		if kept.Account.Experience != xpInsideFirstRank || kept.Account.ExperienceMin != levelCapXP ||
			kept.Account.ExperienceMax != secondCapRankXP || kept.Account.EnergyMax != earnedEnergyMax ||
			kept.Account.StaminaMax != economy.StartStaminaMax || len(kept.Renown.Perks) != 1 ||
			kept.Renown.Perks[catalog.RenownEnergyMax] != 1 {
			t.Errorf("after an upload with %s the server keeps %+v", upload.name, kept)
		}
	}
}

func TestAProfileFromBeforeRenownGetsItsBoundsFromTheNextAction(t *testing.T) {
	h := newHarness(t)
	beforeRenown := editedProfile(t, guildProfile("Hawke", economy.MaxPlayerLevel, 10),
		withExperience(economy.MaxPlayerLevel, levelCapXP, levelCapXP, levelCapXP))
	token := h.storedAs("local", beforeRenown)

	response, body := h.do("PUT", "/v1/players/local/profile", token, "1", beforeRenown)
	expectStatus(t, response, body, http.StatusOK)
	if uploaded := h.storedRenown("local", token); uploaded.Account.Experience != levelCapXP ||
		len(uploaded.Renown.Perks) != 0 {
		t.Fatalf("after its upload the profile is %+v", uploaded)
	}

	response, body = h.do("POST", "/v1/players/local/actions", token, "2",
		upgradeRenown("renown-1", catalog.RenownStaminaMax))
	expectStatus(t, response, body, http.StatusOK)
	wantStaminaMax := int64(economy.StartStaminaMax + h.catalog().Renown.PerkValue(catalog.RenownStaminaMax, 1))
	if ranked := h.storedRenown("local", token); ranked.Account.ExperienceMin != levelCapXP ||
		ranked.Account.ExperienceMax != secondCapRankXP || ranked.Account.StaminaMax != wantStaminaMax ||
		ranked.Renown.Perks[catalog.RenownStaminaMax] != 1 {
		t.Fatalf("after its first upgrade the profile is %+v", ranked)
	}
}
