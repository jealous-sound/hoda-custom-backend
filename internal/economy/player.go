package economy

import (
	"crypto/rand"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"time"

	"github.com/jealous-sound/hoda-custom-backend/internal/catalog"
	"github.com/jealous-sound/hoda-custom-backend/internal/profile"
)

const (
	voucherWinDropPct  = 5
	critGoldPerCrit    = 10
	maxCreditedCrits   = 100
	percentScale       = 100.0
	runeSlotsPerSquad  = 3
	runeSecondsPerMin  = 60
	heroesGrantedLimit = 64
)

type player struct {
	catalog  *catalog.Catalog
	doc      document
	account  *account
	server   *profile.ServerState
	now      time.Time
	rng      rng
	granted  Granted
	duels    Duels
	rivals   EventStandings
	counters missionCounters
	seeds    SeedSource
	fighters []int64
}

func openPlayer(c *catalog.Catalog, raw json.RawMessage, server *profile.ServerState, now time.Time) (*player, error) {
	doc, err := parseDocument(raw)
	if err != nil {
		return nil, refuse(BadProfile, "%v", err)
	}
	if err := regenerate(doc, regenSince(server), now); err != nil {
		return nil, err
	}
	a, err := loadAccount(doc)
	if err != nil {
		return nil, err
	}
	p := &player{
		catalog: c, doc: doc, account: a, server: server, now: now, duels: noDuels{}, rivals: noRivals{}, seeds: RandomSeeds,
		granted: Granted{Heroes: []GrantedHero{}, Runes: []string{}, Gear: []string{}},
	}
	p.rng.state = server.StoreRNG
	for p.rng.state == 0 {
		var bytes [4]byte
		if _, err := rand.Read(bytes[:]); err != nil {
			return nil, fmt.Errorf("seed store rng: %w", err)
		}
		p.rng.state = binary.LittleEndian.Uint32(bytes[:])
	}
	return p, nil
}

func (p *player) close() (json.RawMessage, error) {
	p.account.store()
	if err := p.syncMissions(); err != nil {
		return nil, err
	}
	if err := p.countQuests(); err != nil {
		return nil, err
	}
	encoded, err := p.doc.encode()
	if err != nil {
		return nil, err
	}
	p.server.StoreRNG = p.rng.state
	p.server.RegenAt = p.now.Unix()
	return encoded, nil
}

func (p *player) addGold(amount int64) {
	p.account.Gold = saturatingAdd(p.account.Gold, amount)
	p.count(statGoldEarned, amount)
}

func (p *player) addGems(amount int64) {
	p.account.Gems = saturatingAdd(p.account.Gems, amount)
}

func (p *player) vouchers() (document, []int64, error) {
	store, err := p.doc.section("store")
	if err != nil {
		return nil, nil, refuse(BadProfile, "%v", err)
	}
	items, err := store.array("vouchers")
	if err != nil {
		return nil, nil, refuse(BadProfile, "%v", err)
	}
	counts, err := intArray(items, "vouchers")
	if err != nil {
		return nil, nil, refuse(BadProfile, "%v", err)
	}
	for len(counts) < catalog.VoucherCapacity {
		counts = append(counts, 0)
	}
	return store, counts, nil
}

func (p *player) addVoucher(index, count int) error {
	store, counts, err := p.vouchers()
	if err != nil {
		return err
	}
	if index < 0 || index >= len(counts) {
		return refuse(Unavailable, "no voucher %d", index)
	}
	counts[index] = saturatingAdd(counts[index], int64(count))
	store["vouchers"] = numberArray(counts)
	return nil
}

func (p *player) maybeAwardVoucherForWin() error {
	kinds := len(p.catalog.Vouchers)
	if kinds == 0 || !p.rng.chance(voucherWinDropPct) {
		return nil
	}
	index := p.rng.below(kinds)
	p.granted.Vouchers = append(p.granted.Vouchers, index)
	return p.addVoucher(index, 1)
}

func creditedCritGold(crits int) int64 {
	return int64(min(max(crits, 0), maxCreditedCrits) * critGoldPerCrit)
}

func applyPct(value int64, pct int) int64 {
	if value <= 0 || pct <= 0 {
		return value
	}
	scaled := math.Round(float64(value) * (percentScale + float64(pct)) / percentScale)
	if scaled >= math.MaxInt32 {
		return math.MaxInt32
	}
	return int64(scaled)
}

func (p *player) runeBonus() (goldPct, xpPct int) {
	runes, _ := p.doc.optionalSection("runes")
	items, _ := runes.array("items")
	active := map[int64]catalog.Rune{}
	for _, entry := range items {
		object, ok := entry.(map[string]any)
		if !ok {
			continue
		}
		uuid, err := numberValue(object["uuid"], "uuid")
		expiry, expiryErr := numberValue(object["expire_at"], "expire_at")
		def, _ := object["def"].(string)
		rune, known := p.catalog.RuneByID(def)
		if err != nil || expiryErr != nil || !known || expiry == 0 || expiry < p.now.Unix() {
			continue
		}
		active[uuid] = rune
	}
	squad, _ := p.doc.intField("current_squad")
	squads, _ := runes.array("squads")
	if squad < 0 || squad >= len(squads) {
		return 0, 0
	}
	object, _ := squads[squad].(map[string]any)
	slots, _ := object["slots"].([]any)
	for i, slot := range slots {
		if i >= runeSlotsPerSquad {
			break
		}
		uuid, err := numberValue(slot, "slot")
		if rune, ok := active[uuid]; err == nil && ok {
			goldPct += rune.GoldPct
			xpPct += rune.XPPct
		}
	}
	return goldPct, xpPct
}

func (p *player) grantHeroes(ids []string) error {
	roster, err := newRosterWriter(p.doc)
	if err != nil {
		return err
	}
	if !roster.hasRoom(len(ids)) {
		return refuse(InventoryFull, "roster has no room for %d heroes", len(ids))
	}
	for _, id := range ids {
		hero, ok := p.catalog.Hero(id)
		if !ok {
			return refuse(Unavailable, "no hero %s", id)
		}
		instance := roster.add(hero)
		p.granted.Heroes = append(p.granted.Heroes, GrantedHero{ID: hero.ID, InstanceID: instance})
	}
	roster.store()
	return nil
}

func (p *player) rosterRoom() int {
	roster, err := newRosterWriter(p.doc)
	if err != nil {
		return 0
	}
	return max(RosterCapacity-roster.starterCount-len(roster.additions), 0)
}

func (p *player) grantRunes(ids []string) error {
	writer, err := newRuneWriter(p.doc, p.now)
	if err != nil {
		return err
	}
	for _, id := range ids {
		if !p.catalog.RuneKnown(id) {
			return refuse(Unavailable, "no rune %s", id)
		}
		if writer.grant(id) {
			p.granted.Runes = append(p.granted.Runes, id)
		}
	}
	writer.store()
	return nil
}

func (p *player) grantGear(id string, count int) error {
	if _, ok := p.catalog.GearIndex(id); !ok {
		return refuse(Unavailable, "no gear %s", id)
	}
	for range count {
		if err := grantGear(p.doc, id, false); err != nil {
			return err
		}
		p.granted.Gear = append(p.granted.Gear, id)
	}
	return nil
}

func (p *player) grantTomes(count int) error {
	for range count {
		if err := grantGear(p.doc, "", true); err != nil {
			return err
		}
		p.granted.Tomes++
	}
	return nil
}

func (p *player) addPotions(count int64) error {
	nexus, err := p.doc.section("nexus")
	if err != nil {
		return refuse(BadProfile, "%v", err)
	}
	potions, err := nexus.int64Field("potions")
	if err != nil {
		return refuse(BadProfile, "%v", err)
	}
	maximum, _ := nexus.optionalInt("potion_max")
	potions = saturatingAdd(potions, count)
	if maximum > 0 {
		potions = min(potions, int64(maximum))
	}
	nexus.setInt("potions", potions)
	return nil
}
