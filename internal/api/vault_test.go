package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/jealous-sound/hoda-custom-backend/internal/catalog"
	"github.com/jealous-sound/hoda-custom-backend/internal/economy"
	"github.com/jealous-sound/hoda-custom-backend/internal/guild"
)

const (
	saverWallet    = 20000
	silverSavings  = 6200
	tierField      = `"vault_tier"`
	depositSilver  = `{"request_id":"vault-deposit-1","action":"vault_deposit","gems":6200}`
	storedProfiles = 1
)

func withGems(gems int64) func(doc map[string]any) {
	return func(doc map[string]any) { doc["account"].(map[string]any)["gems"] = gems }
}

func withVault(locked int64) func(doc map[string]any) {
	return func(doc map[string]any) { doc["vault"] = map[string]any{"locked": locked} }
}

func TestADepositShowsItsTierOnTheLadderAndInTheAdminTotals(t *testing.T) {
	h := newHarnessWith(t, Config{OpenUnregistered: true, AdminToken: testAdminToken})
	viewer := h.storedPlayer("viewer", "Hawke", 400, 3)
	saver := h.storedAs("saver", editedProfile(t, rankedProfile("Varric", 500, 1), withGems(saverWallet)))

	deposited := h.act("saver", saver, storedProfiles, depositSilver)
	if deposited.Profile.Account.Gems != saverWallet-silverSavings {
		t.Fatalf("after the deposit the wallet holds %d", deposited.Profile.Account.Gems)
	}
	rows := h.ladder("viewer", viewer)
	if len(rows) != 1 || rows[0].VaultTier != int(catalog.VaultTierSilver) {
		t.Fatalf("ladder = %+v, want the saver at the silver tier", rows)
	}

	forged := editedProfile(t, rankedProfile("Varric", 500, 1), withVault(15000))
	response, body := h.do("PUT", "/v1/players/saver/profile", saver, "2", forged)
	expectStatus(t, response, body, http.StatusOK)
	h.pass(ladderMaxAge)
	if rows := h.ladder("viewer", viewer); len(rows) != 1 || rows[0].VaultTier != int(catalog.VaultTierSilver) {
		t.Fatalf("after a forged upload the ladder lists %+v", rows)
	}

	h.pass(storedPlayersMaxAge)
	totals := h.overview().Gems
	if totals.InVaults != silverSavings || totals.VaultHolders != 1 || totals.InWallets < saverWallet-silverSavings {
		t.Fatalf("gem totals = %+v", totals)
	}
	if listed := h.listedPlayers()["saver"]; listed.VaultGems != silverSavings {
		t.Fatalf("the saver is listed with %d vault gems", listed.VaultGems)
	}
}

func TestChatLinesAndDuelOpponentsCarryTheVaultTier(t *testing.T) {
	h := newHarness(t)
	gold := h.storedAs("gold", editedProfile(t, guildProfile("Varric", 9, 10), withVault(15000)))
	plain := h.storedAs("plain", guildProfile("Merrill", 9, 10))

	response, _, body := h.world("gold", gold, `{"text":"the vault is full","vault_tier":1}`)
	expectStatus(t, response, body, http.StatusOK)
	response, _, body = h.world("plain", plain, `{"text":"mine is empty","vault_tier":3}`)
	expectStatus(t, response, body, http.StatusOK)
	var lines struct {
		Messages []guild.Message `json:"messages"`
	}
	if err := json.Unmarshal([]byte(body), &lines); err != nil {
		t.Fatal(err)
	}
	if len(lines.Messages) != 2 || lines.Messages[0].VaultTier != int(catalog.VaultTierGold) ||
		lines.Messages[1].VaultTier != 0 || strings.Count(body, tierField) != 1 {
		t.Fatalf("world chat = %s, want the gold tier on the first line only", body)
	}

	h.storedAs("rival", editedProfile(t, rankedProfile("Isabela", rivalTrophies, 1), withVault(1000)))
	response, body = h.do("GET", "/v1/players/plain/pvp/ladder/rival", plain, "", "")
	expectStatus(t, response, body, http.StatusOK)
	var opponent economy.DuelOpponent
	if err := json.Unmarshal([]byte(body), &opponent); err != nil {
		t.Fatal(err)
	}
	if opponent.VaultTier != int(catalog.VaultTierBronze) {
		t.Fatalf("duel opponent = %s, want the bronze tier", body)
	}
}
