package shop

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/jealous-sound/hoda-custom-backend/internal/catalog"
	"github.com/jealous-sound/hoda-custom-backend/internal/profile"
)

var springDay = time.Date(2026, time.March, 10, 12, 0, 0, 0, time.UTC)

type fixture struct {
	gold, gems     int
	vouchers       []int
	sinceLegendary int
	openedByPack   [catalog.StoreCounterCount]int
	tutorial       map[string]any
	additions      []any
	runeItems      []any
}

func (f fixture) profile(t *testing.T) json.RawMessage {
	t.Helper()
	opened := 0
	for _, count := range f.openedByPack {
		opened += count
	}
	vouchers := f.vouchers
	if vouchers == nil {
		vouchers = make([]int, catalog.VoucherCapacity)
	}
	additions := f.additions
	if additions == nil {
		additions = []any{
			map[string]any{"id": "UNIT_ELVENMAGE", "instance_id": 7, "tier": "I", "level": 3, "xp": 40,
				"ascended": false, "consume_xp": 0},
		}
	}
	runeItems := f.runeItems
	if runeItems == nil {
		runeItems = []any{}
	}
	doc := map[string]any{
		"version": 9,
		"account": map[string]any{"level": 5, "gold": f.gold, "gems": f.gems, "energy": 4, "energy_max": 30,
			"energy_regen_timer": 123.5, "stamina": 8, "stamina_max": 8, "stamina_regen_timer": 0},
		"store": map[string]any{"rng_state": 99, "opened_total": opened, "opened_by_pack": f.openedByPack[:],
			"champion_since_legendary": f.sinceLegendary, "vouchers": vouchers},
		"roster": map[string]any{"starter_count": 0, "next_instance_id": 8, "starters": []any{},
			"additions": additions},
		"squads":   []any{},
		"runes":    map[string]any{"next_uuid": 5, "seen": 0, "items": runeItems, "squads": []any{}},
		"missions": map[string]any{"counters": map[string]any{"summoned": 2}},
	}
	if f.tutorial != nil {
		doc["tutorial"] = f.tutorial
	}
	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func mustCatalog(t *testing.T) *catalog.Catalog {
	t.Helper()
	c, err := catalog.Default()
	if err != nil {
		t.Fatal(err)
	}
	return c
}

type decoded struct {
	Account struct {
		Gold, Gems, Energy int
		EnergyRegenTimer   float64 `json:"energy_regen_timer"`
	}
	Store struct {
		OpenedTotal    int   `json:"opened_total"`
		OpenedByPack   []int `json:"opened_by_pack"`
		SinceLegendary int   `json:"champion_since_legendary"`
		Vouchers       []int
		RNGState       int `json:"rng_state"`
	}
	Roster struct {
		NextInstanceID int `json:"next_instance_id"`
		Additions      []struct {
			ID         string
			InstanceID int `json:"instance_id"`
			Tier       string
			Level      int
		}
	}
	Runes struct {
		NextUUID int `json:"next_uuid"`
		Items    []struct {
			UUID              int    `json:"uuid"`
			Def               string `json:"def"`
			InventoryExpireAt int64  `json:"inventory_expire_at"`
		}
	}
	Gear struct {
		Inventory map[string]int
		Tomes     int
	}
	Missions struct {
		Counters map[string]int
	}
}

func decode(t *testing.T, raw json.RawMessage) decoded {
	t.Helper()
	var d decoded
	if err := json.Unmarshal(raw, &d); err != nil {
		t.Fatal(err)
	}
	return d
}

func buy(t *testing.T, raw json.RawMessage, server *profile.ServerState, request PurchaseRequest) (decoded, Granted) {
	t.Helper()
	next, granted, err := Purchase(mustCatalog(t), raw, server, request, springDay)
	if err != nil {
		t.Fatalf("buy %s: %v", request.Pack, err)
	}
	return decode(t, next), granted
}

func refusalCode(err error) RefusalCode {
	var refusal *Refusal
	if errors.As(err, &refusal) {
		return refusal.Code
	}
	return ""
}

func TestHeroPackChargesAndAddsHeroes(t *testing.T) {
	server := &profile.ServerState{StoreRNG: 12345}
	raw := fixture{gold: 1000}.profile(t)
	after, granted := buy(t, raw, server, PurchaseRequest{Pack: "recruit", Currency: catalog.CurrencyGold, Price: 350})
	if after.Account.Gold != 650 {
		t.Errorf("gold = %d, want 650", after.Account.Gold)
	}
	if len(granted.Heroes) != 1 || granted.Heroes[0].InstanceID != 8 {
		t.Fatalf("granted = %+v", granted.Heroes)
	}
	added := after.Roster.Additions[len(after.Roster.Additions)-1]
	if added.ID != granted.Heroes[0].ID || added.InstanceID != 8 || added.Level != 1 || added.Tier == "" {
		t.Errorf("roster addition = %+v", added)
	}
	if after.Roster.NextInstanceID != 9 {
		t.Errorf("next_instance_id = %d, want 9", after.Roster.NextInstanceID)
	}
	if after.Store.OpenedTotal != 1 || after.Store.OpenedByPack[0] != 1 {
		t.Errorf("opened = %d %v", after.Store.OpenedTotal, after.Store.OpenedByPack)
	}
	if after.Missions.Counters["summoned"] != 3 {
		t.Errorf("summoned = %d, want 3", after.Missions.Counters["summoned"])
	}
	if server.StoreRNG == 12345 {
		t.Error("store rng did not advance")
	}
	if after.Store.RNGState != 99 || after.Account.EnergyRegenTimer != 123.5 {
		t.Error("purchase touched fields it does not own")
	}
}

func TestVoucherIsSpentBeforeCurrency(t *testing.T) {
	c := mustCatalog(t)
	recruit, _ := c.Pack("recruit")
	vouchers := make([]int, catalog.VoucherCapacity)
	vouchers[recruit.Voucher] = 2
	raw := fixture{gold: 1000, vouchers: vouchers}.profile(t)
	_, _, err := Purchase(c, raw, &profile.ServerState{}, PurchaseRequest{
		Pack: "recruit", Currency: catalog.CurrencyGold, Price: 350,
	}, springDay)
	if refusalCode(err) != OfferChanged {
		t.Fatalf("paying gold while holding a voucher: %v", err)
	}
	after, _ := buy(t, raw, &profile.ServerState{}, PurchaseRequest{
		Pack: "recruit", Currency: catalog.CurrencyVoucher, Price: 1,
	})
	if after.Account.Gold != 1000 || after.Store.Vouchers[recruit.Voucher] != 1 {
		t.Errorf("gold %d, vouchers %v", after.Account.Gold, after.Store.Vouchers)
	}
}

func TestRefusalsLeaveNothingBehind(t *testing.T) {
	c := mustCatalog(t)
	server := &profile.ServerState{StoreRNG: 777}
	cases := []struct {
		name    string
		raw     json.RawMessage
		request PurchaseRequest
		want    RefusalCode
	}{
		{"poor", fixture{gold: 100}.profile(t),
			PurchaseRequest{Pack: "recruit", Currency: catalog.CurrencyGold, Price: 350}, CantAfford},
		{"stale price", fixture{gold: 1000}.profile(t),
			PurchaseRequest{Pack: "recruit", Currency: catalog.CurrencyGold, Price: 1}, OfferChanged},
		{"off season", fixture{gems: 1000}.profile(t),
			PurchaseRequest{Pack: "harvest", Currency: catalog.CurrencyVoucher, Price: 1}, Unavailable},
		{"unknown", fixture{gems: 1000}.profile(t),
			PurchaseRequest{Pack: "nope", Currency: catalog.CurrencyGems, Price: 1}, Unavailable},
		{"tutorial only", fixture{gold: 1000}.profile(t),
			PurchaseRequest{Pack: "tutorial_consume", Currency: catalog.CurrencyGold, Price: 0}, Unavailable},
	}
	for _, tc := range cases {
		next, _, err := Purchase(c, tc.raw, server, tc.request, springDay)
		if refusalCode(err) != tc.want || next != nil {
			t.Errorf("%s: got %v, want %s", tc.name, err, tc.want)
		}
	}
	if server.StoreRNG != 777 {
		t.Error("a refused purchase advanced the store rng")
	}
}

func TestFullRosterIsRefused(t *testing.T) {
	additions := make([]any, RosterCapacity)
	for i := range additions {
		additions[i] = map[string]any{"id": "UNIT_ELVENMAGE", "instance_id": i + 1}
	}
	raw := fixture{gold: 1000, additions: additions}.profile(t)
	_, _, err := Purchase(mustCatalog(t), raw, &profile.ServerState{}, PurchaseRequest{
		Pack: "recruit", Currency: catalog.CurrencyGold, Price: 350,
	}, springDay)
	if refusalCode(err) != InventoryFull {
		t.Fatalf("got %v", err)
	}
}

func TestChampionPityGuaranteesALegendary(t *testing.T) {
	c := mustCatalog(t)
	champion, _ := c.Pack("champion")
	for seed := uint32(1); seed <= 20; seed++ {
		raw := fixture{gems: 100, sinceLegendary: champion.LegendaryPity - 1,
			openedByPack: [4]int{0, 0, champion.LegendaryPity - 1, 0}}.profile(t)
		after, granted := buy(t, raw, &profile.ServerState{StoreRNG: seed}, PurchaseRequest{
			Pack: "champion", Currency: catalog.CurrencyGems, Price: 30,
		})
		hero, _ := c.Hero(granted.Heroes[0].ID)
		if hero.Rarity != catalog.RarityLegendary || after.Store.SinceLegendary != 0 {
			t.Fatalf("seed %d: %s is %d, since legendary %d", seed, hero.ID, hero.Rarity, after.Store.SinceLegendary)
		}
	}
}

func TestHeroPacksRespectTheirMinimumRarity(t *testing.T) {
	c := mustCatalog(t)
	soldier, _ := c.Pack("soldier")
	server := &profile.ServerState{StoreRNG: 4242}
	raw := fixture{gold: 1_000_000}.profile(t)
	for i := 0; i < 200; i++ {
		var granted Granted
		var err error
		raw, granted, err = Purchase(c, raw, server, PurchaseRequest{
			Pack: "soldier", Currency: catalog.CurrencyGold, Price: soldier.Paid.Price,
		}, springDay)
		if err != nil {
			t.Fatal(err)
		}
		for _, h := range granted.Heroes {
			hero, _ := c.Hero(h.ID)
			if hero.Rarity < soldier.MinimumRarity || hero.Rarity > catalog.RarityLegendary || !hero.Obtainable {
				t.Fatalf("soldier pack gave %s of rarity %d", hero.ID, hero.Rarity)
			}
		}
	}
	if got := decode(t, raw).Store.OpenedByPack[soldier.OpenedCounter]; got != 200 {
		t.Errorf("soldier counter = %d, want 200", got)
	}
}

func TestUniquePacksDoNotRepeatHeroes(t *testing.T) {
	c := mustCatalog(t)
	wisp, _ := c.Pack("free_wisp")
	newYear := time.Date(2027, time.January, 3, 0, 0, 0, 0, time.UTC)
	for seed := uint32(1); seed <= 30; seed++ {
		_, granted, err := Purchase(c, fixture{}.profile(t), &profile.ServerState{StoreRNG: seed}, PurchaseRequest{
			Pack: "free_wisp", Currency: wisp.Paid.Currency, Price: wisp.Paid.Price,
		}, newYear)
		if err != nil {
			t.Fatal(err)
		}
		seen := map[string]bool{}
		for _, h := range granted.Heroes {
			if seen[h.ID] {
				t.Fatalf("seed %d repeated %s", seed, h.ID)
			}
			seen[h.ID] = true
		}
	}
}

func TestRunePackGrantsOneRunePerSlot(t *testing.T) {
	c := mustCatalog(t)
	pack, _ := c.Pack("speed_kills")
	after, granted := buy(t, fixture{gems: 100}.profile(t), &profile.ServerState{StoreRNG: 9}, PurchaseRequest{
		Pack: "speed_kills", Currency: catalog.CurrencyGems, Price: pack.Paid.Price,
	})
	if len(granted.Runes) != len(pack.Slots) || len(after.Runes.Items) != len(pack.Slots) {
		t.Fatalf("granted %v, inventory %d", granted.Runes, len(after.Runes.Items))
	}
	if after.Runes.Items[0].UUID != 5 || after.Runes.NextUUID != 5+len(pack.Slots) {
		t.Errorf("uuids start at %d, next %d", after.Runes.Items[0].UUID, after.Runes.NextUUID)
	}
	if want := springDay.Add(RuneInventoryLifetime).Unix(); after.Runes.Items[0].InventoryExpireAt != want {
		t.Errorf("inventory_expire_at = %d, want %d", after.Runes.Items[0].InventoryExpireAt, want)
	}
	if after.Account.Gems != 100-pack.Paid.Price {
		t.Errorf("gems = %d", after.Account.Gems)
	}
}

func TestGearPackFillsTheInventory(t *testing.T) {
	c := mustCatalog(t)
	pack, _ := c.Pack("peasant")
	after, granted := buy(t, fixture{gold: 5000}.profile(t), &profile.ServerState{StoreRNG: 3}, PurchaseRequest{
		Pack: "peasant", Currency: catalog.CurrencyGold, Price: pack.Paid.Price,
	})
	if len(granted.Gear) != pack.GearCount || after.Gear.Inventory[granted.Gear[0]] != 1 {
		t.Fatalf("granted %v, inventory %v", granted.Gear, after.Gear.Inventory)
	}
	if after.Account.Gold != 5000-pack.Paid.Price {
		t.Errorf("gold = %d", after.Account.Gold)
	}
}

func TestTutorialPacksAreScriptedOnce(t *testing.T) {
	c := mustCatalog(t)
	scripted := c.Scripted["soldier"]
	server := &profile.ServerState{StoreRNG: 5}
	tutorial := map[string]any{"complete": false, "consume_complete": false, "restore_step": 3}
	raw := fixture{gold: 10_000, tutorial: tutorial}.profile(t)
	request := PurchaseRequest{Pack: "soldier", Currency: catalog.CurrencyGold, Price: 1500, Scripted: true}
	raw, granted, err := Purchase(c, raw, server, request, springDay)
	if err != nil {
		t.Fatal(err)
	}
	if granted.Heroes[0].ID != scripted.Heroes[0] {
		t.Errorf("scripted soldier gave %s, want %s", granted.Heroes[0].ID, scripted.Heroes[0])
	}
	rolledOthers := false
	for seed := uint32(1); seed <= 10; seed++ {
		server.StoreRNG = seed
		_, again, err := Purchase(c, raw, server, request, springDay)
		if err != nil {
			t.Fatal(err)
		}
		rolledOthers = rolledOthers || again.Heroes[0].ID != scripted.Heroes[0]
	}
	if !rolledOthers {
		t.Error("the scripted soldier pack kept its script after it was delivered")
	}
	if len(server.ScriptedPacks) != 1 || server.ScriptedPacks[0] != "soldier" {
		t.Errorf("scripted packs = %v", server.ScriptedPacks)
	}
}

func TestTutorialConsumePackNeedsTheTutorial(t *testing.T) {
	c := mustCatalog(t)
	request := PurchaseRequest{Pack: "tutorial_consume", Currency: catalog.CurrencyGold, Price: 0, Scripted: true}
	running := map[string]any{"complete": true, "consume_complete": false, "restore_step": 0}
	after, granted := buy(t, fixture{tutorial: running}.profile(t), &profile.ServerState{}, request)
	if len(granted.Heroes) != 2 || len(after.Roster.Additions) != 3 {
		t.Fatalf("granted %v", granted.Heroes)
	}
	done := map[string]any{"complete": true, "consume_complete": true, "restore_step": 0}
	_, _, err := Purchase(c, fixture{tutorial: done}.profile(t), &profile.ServerState{}, request, springDay)
	if refusalCode(err) != Unavailable {
		t.Errorf("finished tutorial: %v", err)
	}
}

func TestRefillTopsUpForGems(t *testing.T) {
	next, err := Refill(fixture{gems: 10}.profile(t), RefillEnergy)
	if err != nil {
		t.Fatal(err)
	}
	after := decode(t, next)
	if after.Account.Energy != 30 || after.Account.Gems != 7 || after.Account.EnergyRegenTimer != 0 {
		t.Errorf("account = %+v", after.Account)
	}
	if _, err := Refill(fixture{gems: 2}.profile(t), RefillEnergy); refusalCode(err) != CantAfford {
		t.Errorf("poor refill: %v", err)
	}
	if _, err := Refill(fixture{gems: 10}.profile(t), RefillStamina); refusalCode(err) != AlreadyFull {
		t.Errorf("full stamina refill: %v", err)
	}
}
