package economy

import (
	"crypto/rand"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"slices"
	"time"

	"github.com/jealous-sound/hoda-custom-backend/internal/catalog"
	"github.com/jealous-sound/hoda-custom-backend/internal/profile"
)

const (
	RosterCapacity        = 1024
	RuneInventoryCapacity = 512
	RuneInventoryLifetime = 30 * 24 * time.Hour
	RefillGemCost         = 3
	voucherOfferPrice     = 1
	firstRuneUUID         = 1
	firstInstanceID       = 1
	newHeroLevel          = 1
	summonedMissionStat   = "summoned"
)

type RefusalCode string

const (
	CantAfford    RefusalCode = "cant_afford"
	InventoryFull RefusalCode = "inventory_full"
	Unavailable   RefusalCode = "unavailable"
	CounterLimit  RefusalCode = "counter_limit"
	OfferChanged  RefusalCode = "offer_changed"
	AlreadyFull   RefusalCode = "already_full"
	BadProfile    RefusalCode = "bad_profile"
)

type Refusal struct {
	Code    RefusalCode
	Message string
}

func (r *Refusal) Error() string {
	return fmt.Sprintf("%s: %s", r.Code, r.Message)
}

func refuse(code RefusalCode, format string, args ...any) *Refusal {
	return &Refusal{Code: code, Message: fmt.Sprintf(format, args...)}
}

type PurchaseRequest struct {
	Pack     string           `json:"pack"`
	Currency catalog.Currency `json:"currency"`
	Price    int              `json:"price"`
	Scripted bool             `json:"scripted"`
}

type GrantedHero struct {
	ID         string `json:"id"`
	InstanceID int64  `json:"instance_id"`
}

type Granted struct {
	Heroes   []GrantedHero `json:"heroes"`
	Runes    []string      `json:"runes"`
	Gear     []string      `json:"gear"`
	Tome     bool          `json:"tome"`
	Tomes    int           `json:"tomes,omitempty"`
	Vouchers []int         `json:"vouchers,omitempty"`
	Gold     int64         `json:"gold,omitempty"`
	Gems     int64         `json:"gems,omitempty"`
}

type purchase struct {
	catalog *catalog.Catalog
	pack    *catalog.Pack
	doc     document
	server  *profile.ServerState
	now     time.Time
	rng     rng
	granted Granted
}

func Purchase(
	c *catalog.Catalog, raw json.RawMessage, server *profile.ServerState, request PurchaseRequest, now time.Time,
) (json.RawMessage, Granted, error) {
	pack, ok := c.Pack(request.Pack)
	if !ok {
		return nil, Granted{}, refuse(Unavailable, "no pack %q", request.Pack)
	}
	doc, err := parseDocument(raw)
	if err != nil {
		return nil, Granted{}, refuse(BadProfile, "%v", err)
	}
	p := &purchase{
		catalog: c, pack: pack, doc: doc, server: server, now: now,
		granted: Granted{Heroes: []GrantedHero{}, Runes: []string{}, Gear: []string{}},
	}
	if err := p.seed(); err != nil {
		return nil, Granted{}, err
	}
	if err := p.run(request); err != nil {
		return nil, Granted{}, err
	}
	if err := syncMissions(c, doc, server, now); err != nil {
		return nil, Granted{}, err
	}
	encoded, err := doc.encode()
	if err != nil {
		return nil, Granted{}, err
	}
	server.StoreRNG = p.rng.state
	return encoded, p.granted, nil
}

func (p *purchase) seed() error {
	p.rng.state = p.server.StoreRNG
	for p.rng.state == 0 {
		var bytes [4]byte
		if _, err := rand.Read(bytes[:]); err != nil {
			return fmt.Errorf("seed store rng: %w", err)
		}
		p.rng.state = binary.LittleEndian.Uint32(bytes[:])
	}
	return nil
}

func (p *purchase) run(request PurchaseRequest) error {
	scripted, err := p.scriptedContents(request.Scripted)
	if err != nil {
		return err
	}
	if scripted == nil && !p.catalog.PackOnSale(p.pack, p.now) {
		return refuse(Unavailable, "pack %s is not on sale", p.pack.ID)
	}
	wallet, err := p.loadWallet()
	if err != nil {
		return err
	}
	offer := wallet.activeOffer(p.pack)
	if offer.Currency != request.Currency || offer.Price != request.Price {
		return refuse(OfferChanged, "pack %s costs %d %s", p.pack.ID, offer.Price, offer.Currency)
	}
	switch p.pack.Kind {
	case catalog.KindRune:
		err = p.buyRunePack(wallet, offer)
	case catalog.KindGear:
		err = p.buyGearPack(wallet, offer)
	default:
		err = p.buyHeroPack(wallet, offer, scripted)
	}
	if err != nil {
		return err
	}
	return wallet.store(p.doc)
}

func (p *purchase) scriptedContents(wanted bool) (*catalog.ScriptedPack, error) {
	contents, known := p.catalog.Scripted[p.pack.ID]
	tutorialDone, err := tutorialFinished(p.doc)
	if err != nil {
		return nil, err
	}
	eligible := wanted && known && !tutorialDone && !slices.Contains(p.server.ScriptedPacks, p.pack.ID) &&
		p.pack.Kind == catalog.KindHero && len(contents.Heroes) == p.pack.HeroCount
	if eligible {
		return &contents, nil
	}
	if p.pack.TutorialOnly {
		return nil, refuse(Unavailable, "pack %s is only sold during the tutorial", p.pack.ID)
	}
	return nil, nil
}

func tutorialFinished(doc document) (bool, error) {
	if _, present := doc["tutorial"]; !present {
		return true, nil
	}
	tutorial, err := doc.section("tutorial")
	if err != nil {
		return false, refuse(BadProfile, "%v", err)
	}
	return tutorial.boolField("complete") && tutorial.boolField("consume_complete"), nil
}

type wallet struct {
	gold, gems     int64
	vouchers       []int64
	openedTotal    int64
	openedByPack   []int64
	sinceLegendary int64
}

func (p *purchase) loadWallet() (*wallet, error) {
	account, err := p.doc.section("account")
	if err != nil {
		return nil, refuse(BadProfile, "%v", err)
	}
	store, err := p.doc.section("store")
	if err != nil {
		return nil, refuse(BadProfile, "%v", err)
	}
	w := &wallet{}
	fields := []struct {
		section document
		name    string
		target  *int64
	}{
		{account, "gold", &w.gold},
		{account, "gems", &w.gems},
		{store, "opened_total", &w.openedTotal},
		{store, "champion_since_legendary", &w.sinceLegendary},
	}
	for _, field := range fields {
		value, err := field.section.int64Field(field.name)
		if err != nil {
			return nil, refuse(BadProfile, "%v", err)
		}
		*field.target = value
	}
	vouchers, err := store.array("vouchers")
	if err == nil {
		w.vouchers, err = intArray(vouchers, "vouchers")
	}
	if err != nil {
		return nil, refuse(BadProfile, "%v", err)
	}
	for len(w.vouchers) < len(p.catalog.Vouchers) {
		w.vouchers = append(w.vouchers, 0)
	}
	opened, err := store.array("opened_by_pack")
	if err == nil {
		w.openedByPack, err = intArray(opened, "opened_by_pack")
	}
	if err != nil || len(w.openedByPack) != catalog.StoreCounterCount {
		return nil, refuse(BadProfile, "store.opened_by_pack must hold %d counters", catalog.StoreCounterCount)
	}
	return w, nil
}

func (w *wallet) activeOffer(pack *catalog.Pack) catalog.Offer {
	if pack.Voucher >= 0 && (w.vouchers[pack.Voucher] > 0 || pack.Paid == nil) {
		return catalog.Offer{Currency: catalog.CurrencyVoucher, Price: voucherOfferPrice, Voucher: pack.Voucher}
	}
	return *pack.Paid
}

func (w *wallet) balance(offer catalog.Offer) *int64 {
	switch offer.Currency {
	case catalog.CurrencyVoucher:
		return &w.vouchers[offer.Voucher]
	case catalog.CurrencyGems:
		return &w.gems
	default:
		return &w.gold
	}
}

func (w *wallet) affordable(offer catalog.Offer) bool {
	return *w.balance(offer) >= int64(offer.Price)
}

func (w *wallet) pay(offer catalog.Offer) {
	*w.balance(offer) -= int64(offer.Price)
}

func (w *wallet) store(doc document) error {
	account, err := doc.section("account")
	if err != nil {
		return err
	}
	store, err := doc.section("store")
	if err != nil {
		return err
	}
	account.setInt("gold", w.gold)
	account.setInt("gems", w.gems)
	store["vouchers"] = numberArray(w.vouchers)
	store.setInt("opened_total", w.openedTotal)
	store["opened_by_pack"] = numberArray(w.openedByPack)
	store.setInt("champion_since_legendary", w.sinceLegendary)
	return nil
}

func (p *purchase) buyHeroPack(w *wallet, offer catalog.Offer, scripted *catalog.ScriptedPack) error {
	roster, err := newRosterWriter(p.doc)
	if err != nil {
		return err
	}
	if !roster.hasRoom(p.pack.HeroCount) {
		return refuse(InventoryFull, "roster has no room for %d heroes", p.pack.HeroCount)
	}
	if !w.affordable(offer) {
		return refuse(CantAfford, "pack %s costs %d %s", p.pack.ID, offer.Price, offer.Currency)
	}
	counter := p.pack.OpenedCounter
	if w.openedTotal >= math.MaxInt32 || w.openedByPack[counter] >= math.MaxInt32 {
		return refuse(CounterLimit, "pack counters are at their limit")
	}
	roll := heroRoll{}
	if scripted == nil {
		var ok bool
		roll, ok = rollHeroes(p.catalog, p.pack, int(w.sinceLegendary), &p.rng)
		if !ok {
			return refuse(Unavailable, "pack %s has no heroes to roll", p.pack.ID)
		}
	} else {
		for _, id := range scripted.Heroes {
			index, _ := p.catalog.HeroIndex(id)
			roll.heroes = append(roll.heroes, index)
			roll.rarities = append(roll.rarities, p.catalog.Heroes[index].Rarity)
		}
	}
	if p.pack.HeroCount > 1 {
		sortByRarity(roll.heroes, roll.rarities)
	}
	for _, index := range roll.heroes {
		hero := p.catalog.Heroes[index]
		p.granted.Heroes = append(p.granted.Heroes, GrantedHero{ID: hero.ID, InstanceID: roster.add(hero)})
	}
	roster.store()
	w.pay(offer)

	runes, err := newRuneWriter(p.doc, p.now)
	if err != nil {
		return err
	}
	if scripted != nil {
		if scripted.Rune != "" && runes.grant(scripted.Rune) {
			p.granted.Runes = append(p.granted.Runes, scripted.Rune)
		}
		p.server.ScriptedPacks = append(p.server.ScriptedPacks, p.pack.ID)
	} else {
		bonus := catalog.RuneSlot{MinRarity: p.pack.RuneMinRarity, MaxRarity: p.pack.RuneMaxRarity}
		for i := 0; i < p.pack.RuneCount; i++ {
			if !p.rng.chance(p.pack.RuneChancePct) {
				continue
			}
			if index := rollRune(p.catalog, bonus, &p.rng); index != noRoll && runes.grant(p.catalog.Runes[index].ID) {
				p.granted.Runes = append(p.granted.Runes, p.catalog.Runes[index].ID)
			}
		}
		if p.rng.chance(p.pack.GearChancePct) {
			if index := rollGear(p.catalog, p.pack.GearMinRarity, p.pack.GearMaxRarity, &p.rng); index != noRoll {
				if err := grantGear(p.doc, p.catalog.Gear[index].ID, false); err != nil {
					return err
				}
				p.granted.Gear = append(p.granted.Gear, p.catalog.Gear[index].ID)
			}
		}
	}
	runes.store()

	w.openedTotal++
	w.openedByPack[counter]++
	if roll.tracksPity {
		w.sinceLegendary = int64(roll.nextPity)
	}
	return addMissionCounter(p.doc, summonedMissionStat, int64(p.pack.HeroCount))
}

func (p *purchase) buyRunePack(w *wallet, offer catalog.Offer) error {
	runes, err := newRuneWriter(p.doc, p.now)
	if err != nil {
		return err
	}
	if !runes.hasRoom(len(p.pack.Slots)) {
		return refuse(InventoryFull, "rune inventory has no room for %d runes", len(p.pack.Slots))
	}
	if !w.affordable(offer) {
		return refuse(CantAfford, "pack %s costs %d %s", p.pack.ID, offer.Price, offer.Currency)
	}
	for _, slot := range p.pack.Slots {
		index := rollRune(p.catalog, slot, &p.rng)
		if index == noRoll || !runes.grant(p.catalog.Runes[index].ID) {
			return refuse(Unavailable, "pack %s has no rune to roll", p.pack.ID)
		}
		p.granted.Runes = append(p.granted.Runes, p.catalog.Runes[index].ID)
	}
	runes.store()
	w.pay(offer)
	return nil
}

func (p *purchase) buyGearPack(w *wallet, offer catalog.Offer) error {
	if !w.affordable(offer) {
		return refuse(CantAfford, "pack %s costs %d %s", p.pack.ID, offer.Price, offer.Currency)
	}
	w.pay(offer)
	for i := 0; i < p.pack.GearCount; i++ {
		index := rollGear(p.catalog, p.pack.GearMinRarity, p.pack.GearMaxRarity, &p.rng)
		if index == noRoll {
			return refuse(Unavailable, "pack %s has no gear to roll", p.pack.ID)
		}
		if err := grantGear(p.doc, p.catalog.Gear[index].ID, false); err != nil {
			return err
		}
		p.granted.Gear = append(p.granted.Gear, p.catalog.Gear[index].ID)
	}
	if rollTome(p.pack.TomeChancePct, &p.rng) {
		p.granted.Tome = true
		return grantGear(p.doc, "", true)
	}
	return nil
}
