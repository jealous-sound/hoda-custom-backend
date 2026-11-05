package catalog

import (
	"embed"
	"encoding/json"
	"fmt"
	"strings"
)

//go:embed data/*.json
var embedded embed.FS

type Rarity int

const (
	RarityCommon Rarity = iota + 1
	RarityUncommon
	RarityRare
	RarityEpic
	RarityLegendary
	RarityMythic
)

var rarityNames = map[string]Rarity{
	"Common":    RarityCommon,
	"Uncommon":  RarityUncommon,
	"Rare":      RarityRare,
	"Epic":      RarityEpic,
	"Legendary": RarityLegendary,
	"Mythic":    RarityMythic,
}

func ParseRarity(name string) Rarity {
	if rarity, ok := rarityNames[name]; ok {
		return rarity
	}
	return RarityCommon
}

type PackKind string

const (
	KindHero PackKind = "hero"
	KindRune PackKind = "rune"
	KindGear PackKind = "gear"
)

type Currency string

const (
	CurrencyGold    Currency = "gold"
	CurrencyGems    Currency = "gems"
	CurrencyVoucher Currency = "voucher"
)

const (
	StoreCounterCount   = 4
	VoucherCapacity     = 16
	heroWeightTotal     = 10000
	maxHeroesPerPack    = 10
	runePackSlotLimit   = 3
	gearPackItemLimit   = 5
	neverSoldHeroMark   = "BANNER"
	defaultPoolExcluded = "WISP"
	overchargedRuneMark = "-6-"
	specialRuneMark     = "Special"
)

type Offer struct {
	Currency Currency
	Price    int
	Voucher  int
}

type RuneSlot struct {
	Families  []string
	MinRarity int
	MaxRarity int
}

type Schedule struct {
	StartMonthDay int
	EndMonthDay   int
}

type Pack struct {
	ID           string
	Kind         PackKind
	Paid         *Offer
	Voucher      int
	TutorialOnly bool
	Schedule     *Schedule
	FlashIndex   int

	OpenedCounter       int
	HeroCount           int
	MinimumRarity       Rarity
	GuaranteedLegendary int
	GuaranteedEpic      int
	GuaranteedRare      int
	FixedHeroes         []string
	PoolTag             string
	UniqueHeroes        bool
	LegendaryPity       int
	Weights             [RarityLegendary + 1]int
	RuneChancePct       int
	RuneCount           int
	RuneMinRarity       int
	RuneMaxRarity       int
	GearChancePct       int
	GearMinRarity       int
	GearMaxRarity       int

	Slots []RuneSlot

	GearCount     int
	TomeChancePct int
}

type Hero struct {
	ID          string
	Rarity      Rarity
	Obtainable  bool
	InitialTier string
}

type Rune struct {
	ID              string
	Family          string
	Rarity          int
	DurationMinutes int
	CraftTo         string
	GoldPct         int
	XPPct           int
}

func (r Rune) Rollable() bool {
	return r.DurationMinutes > 0 && !strings.Contains(r.ID, overchargedRuneMark) &&
		!strings.Contains(r.ID, specialRuneMark)
}

type Gear struct {
	ID     string
	Rarity int
}

type ScriptedPack struct {
	Heroes []string
	Rune   string
}

type Catalog struct {
	Packs      []Pack
	Vouchers   []string
	FlashDays  int
	FlashCount int
	Heroes     []Hero
	Runes      []Rune
	Gear       []Gear
	Scripted   map[string]ScriptedPack

	Campaign         []CampaignMap
	Nexus            []NexusEvent
	Pvp              PvpRules
	Missions         []Mission
	Tutorial         TutorialRewards
	GearSellGold     []int
	GearPrestigeGems int

	packIndex    map[string]int
	missionIndex map[string]int
	gearIndex    map[string]int
	heroIndex    map[string]int
	runeIndex    map[string]int
	rarityPool   [RarityLegendary + 1][]int
}

func (c *Catalog) Pack(id string) (*Pack, bool) {
	index, ok := c.packIndex[id]
	if !ok {
		return nil, false
	}
	return &c.Packs[index], true
}

func (c *Catalog) Hero(id string) (Hero, bool) {
	index, ok := c.heroIndex[id]
	if !ok {
		return Hero{}, false
	}
	return c.Heroes[index], true
}

func (c *Catalog) HeroIndex(id string) (int, bool) {
	index, ok := c.heroIndex[id]
	return index, ok
}

func (c *Catalog) RuneKnown(id string) bool {
	_, ok := c.runeIndex[id]
	return ok
}

func (c *Catalog) RarityPool(rarity Rarity) []int {
	if rarity < RarityCommon || rarity > RarityLegendary {
		return nil
	}
	return c.rarityPool[rarity]
}

func (c *Catalog) TaggedPool(tag string, rarity Rarity) []int {
	var pool []int
	for index, hero := range c.Heroes {
		if heroSellable(hero) && strings.Contains(hero.ID, tag) && hero.Rarity == rarity {
			pool = append(pool, index)
		}
	}
	return pool
}

func heroSellable(hero Hero) bool {
	return hero.Obtainable && !strings.Contains(hero.ID, neverSoldHeroMark) &&
		hero.Rarity >= RarityCommon && hero.Rarity <= RarityLegendary
}

func heroInDefaultPool(hero Hero) bool {
	return heroSellable(hero) && !strings.Contains(hero.ID, defaultPoolExcluded)
}

var defaultCatalog *Catalog

func Default() (*Catalog, error) {
	if defaultCatalog != nil {
		return defaultCatalog, nil
	}
	loaded, err := Load(func(name string) ([]byte, error) { return embedded.ReadFile("data/" + name) })
	if err != nil {
		return nil, err
	}
	defaultCatalog = loaded
	return loaded, nil
}

type rawOffer struct {
	Currency string `json:"currency"`
	Price    int    `json:"price"`
	Voucher  string `json:"voucher"`
}

type rawPack struct {
	ID           string     `json:"id"`
	Kind         string     `json:"kind"`
	Offers       []rawOffer `json:"offers"`
	TutorialOnly bool       `json:"tutorial_only"`
	Schedule     *struct {
		Start string `json:"start"`
		End   string `json:"end"`
	} `json:"schedule"`
	Rotation *struct {
		Index int `json:"index"`
	} `json:"rotation"`
	OpenedCounter       int            `json:"opened_counter"`
	HeroCount           int            `json:"hero_count"`
	MinimumRarity       string         `json:"minimum_rarity"`
	GuaranteedLegendary int            `json:"guaranteed_legendary"`
	GuaranteedEpic      int            `json:"guaranteed_epic"`
	GuaranteedRare      int            `json:"guaranteed_rare"`
	FixedHeroes         []string       `json:"fixed_heroes"`
	PoolTag             string         `json:"pool_tag"`
	UniqueHeroes        bool           `json:"unique_heroes"`
	LegendaryPity       int            `json:"legendary_pity"`
	Weights             map[string]int `json:"weights"`
	RuneChancePct       int            `json:"rune_chance_pct"`
	RuneCount           int            `json:"rune_count"`
	RuneMinRarity       int            `json:"rune_min_rarity"`
	RuneMaxRarity       int            `json:"rune_max_rarity"`
	GearChancePct       int            `json:"gear_chance_pct"`
	GearMinRarity       int            `json:"gear_min_rarity"`
	GearMaxRarity       int            `json:"gear_max_rarity"`
	Slots               []struct {
		Families  []string `json:"families"`
		MinRarity int      `json:"min_rarity"`
		MaxRarity int      `json:"max_rarity"`
	} `json:"slots"`
	Count         int `json:"count"`
	MinRarity     int `json:"min_rarity"`
	MaxRarity     int `json:"max_rarity"`
	TomeChancePct int `json:"tome_chance_pct"`
}

type rawStore struct {
	Version   int `json:"version"`
	FlashDays int `json:"flash_days"`
	Vouchers  []struct {
		ID string `json:"id"`
	} `json:"vouchers"`
	Packs []rawPack `json:"packs"`
}

type rawHeroPool struct {
	Heroes []struct {
		ID          string `json:"id"`
		Rarity      string `json:"rarity"`
		Obtainable  bool   `json:"obtainable"`
		InitialTier string `json:"initial_tier"`
	} `json:"heroes"`
}

type rawRunes struct {
	Runes []struct {
		ID              string `json:"id"`
		Family          string `json:"family"`
		Rarity          int    `json:"rarity"`
		DurationMinutes int    `json:"duration_minutes"`
	} `json:"runes"`
}

type rawGear struct {
	Items []struct {
		ID     string `json:"id"`
		Rarity int    `json:"rarity"`
	} `json:"items"`
}

type rawTutorial struct {
	Packs map[string]struct {
		Heroes []string `json:"heroes"`
		Rune   string   `json:"rune"`
	} `json:"packs"`
}

const supportedStoreVersion = 3

func Load(read func(name string) ([]byte, error)) (*Catalog, error) {
	var store rawStore
	var heroes rawHeroPool
	var runes rawRunes
	var gear rawGear
	var tutorial rawTutorial
	for name, target := range map[string]any{
		"store_packs.json":      &store,
		"hero_pool.json":        &heroes,
		"runes.json":            &runes,
		"gear.json":             &gear,
		"tutorial_rewards.json": &tutorial,
	} {
		data, err := read(name)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", name, err)
		}
		if err := json.Unmarshal(data, target); err != nil {
			return nil, fmt.Errorf("decode %s: %w", name, err)
		}
	}
	if store.Version != supportedStoreVersion {
		return nil, fmt.Errorf("store_packs.json version %d, want %d", store.Version, supportedStoreVersion)
	}
	c := &Catalog{
		FlashDays: store.FlashDays,
		Scripted:  map[string]ScriptedPack{},
		packIndex: map[string]int{},
		heroIndex: map[string]int{},
		runeIndex: map[string]int{},
	}
	for _, hero := range heroes.Heroes {
		c.heroIndex[hero.ID] = len(c.Heroes)
		c.Heroes = append(c.Heroes, Hero{
			ID: hero.ID, Rarity: ParseRarity(hero.Rarity), Obtainable: hero.Obtainable, InitialTier: hero.InitialTier,
		})
	}
	for index, hero := range c.Heroes {
		if heroInDefaultPool(hero) {
			c.rarityPool[hero.Rarity] = append(c.rarityPool[hero.Rarity], index)
		}
	}
	for _, rune := range runes.Runes {
		c.runeIndex[rune.ID] = len(c.Runes)
		c.Runes = append(c.Runes, Rune{
			ID: rune.ID, Family: rune.Family, Rarity: rune.Rarity, DurationMinutes: rune.DurationMinutes,
		})
	}
	for _, item := range gear.Items {
		c.Gear = append(c.Gear, Gear{ID: item.ID, Rarity: item.Rarity})
	}
	if len(store.Vouchers) > VoucherCapacity {
		return nil, fmt.Errorf("%d vouchers, capacity is %d", len(store.Vouchers), VoucherCapacity)
	}
	voucherIndex := map[string]int{}
	for index, voucher := range store.Vouchers {
		voucherIndex[voucher.ID] = index
		c.Vouchers = append(c.Vouchers, voucher.ID)
	}
	for _, raw := range store.Packs {
		pack, err := c.parsePack(raw, voucherIndex)
		if err != nil {
			return nil, fmt.Errorf("pack %s: %w", raw.ID, err)
		}
		if pack.FlashIndex >= 0 {
			c.FlashCount++
		}
		c.packIndex[pack.ID] = len(c.Packs)
		c.Packs = append(c.Packs, pack)
	}
	for id, scripted := range tutorial.Packs {
		for _, hero := range scripted.Heroes {
			if _, ok := c.heroIndex[hero]; !ok {
				return nil, fmt.Errorf("tutorial pack %s names unknown hero %s", id, hero)
			}
		}
		if scripted.Rune != "" && !c.RuneKnown(scripted.Rune) {
			return nil, fmt.Errorf("tutorial pack %s names unknown rune %s", id, scripted.Rune)
		}
		c.Scripted[id] = ScriptedPack{Heroes: scripted.Heroes, Rune: scripted.Rune}
	}
	for _, rarity := range []Rarity{RarityCommon, RarityUncommon, RarityRare, RarityEpic, RarityLegendary} {
		if len(c.rarityPool[rarity]) == 0 {
			return nil, fmt.Errorf("no obtainable heroes of rarity %d", rarity)
		}
	}
	if err := c.loadRules(read); err != nil {
		return nil, err
	}
	return c, nil
}

func parseMonthDay(text string) (int, error) {
	var month, day int
	if _, err := fmt.Sscanf(text, "%d-%d", &month, &day); err != nil {
		return 0, fmt.Errorf("bad month-day %q", text)
	}
	return month*100 + day, nil
}

func (c *Catalog) parsePack(raw rawPack, voucherIndex map[string]int) (Pack, error) {
	pack := Pack{
		ID:           raw.ID,
		Kind:         PackKind(raw.Kind),
		Voucher:      -1,
		TutorialOnly: raw.TutorialOnly,
		FlashIndex:   -1,
	}
	if pack.Kind == "" {
		pack.Kind = KindHero
	}
	for _, offer := range raw.Offers {
		if Currency(offer.Currency) == CurrencyVoucher {
			index, ok := voucherIndex[offer.Voucher]
			if !ok {
				return pack, fmt.Errorf("unknown voucher %s", offer.Voucher)
			}
			pack.Voucher = index
			continue
		}
		currency := Currency(offer.Currency)
		if currency != CurrencyGems {
			currency = CurrencyGold
		}
		pack.Paid = &Offer{Currency: currency, Price: offer.Price, Voucher: -1}
	}
	if pack.Paid == nil && pack.Voucher < 0 {
		return pack, fmt.Errorf("no offer")
	}
	if raw.Schedule != nil {
		start, err := parseMonthDay(raw.Schedule.Start)
		if err != nil {
			return pack, err
		}
		end, err := parseMonthDay(raw.Schedule.End)
		if err != nil {
			return pack, err
		}
		pack.Schedule = &Schedule{StartMonthDay: start, EndMonthDay: end}
	}
	if raw.Rotation != nil {
		pack.FlashIndex = raw.Rotation.Index
	}
	switch pack.Kind {
	case KindHero:
		return pack, c.parseHeroContents(&pack, raw)
	case KindRune:
		for _, slot := range raw.Slots {
			pack.Slots = append(pack.Slots, RuneSlot{
				Families: slot.Families, MinRarity: slot.MinRarity, MaxRarity: slot.MaxRarity,
			})
		}
		if len(pack.Slots) == 0 || len(pack.Slots) > runePackSlotLimit {
			return pack, fmt.Errorf("rune pack needs 1-%d slots", runePackSlotLimit)
		}
		return pack, nil
	case KindGear:
		pack.GearCount = raw.Count
		pack.GearMinRarity = raw.MinRarity
		pack.GearMaxRarity = raw.MaxRarity
		pack.TomeChancePct = raw.TomeChancePct
		if pack.GearCount <= 0 || pack.GearCount > gearPackItemLimit || pack.GearMinRarity > pack.GearMaxRarity {
			return pack, fmt.Errorf("invalid gear pack contents")
		}
		return pack, nil
	default:
		return pack, fmt.Errorf("unknown kind %s", raw.Kind)
	}
}

func (c *Catalog) parseHeroContents(pack *Pack, raw rawPack) error {
	pack.OpenedCounter = raw.OpenedCounter
	pack.HeroCount = raw.HeroCount
	pack.MinimumRarity = ParseRarity(raw.MinimumRarity)
	pack.GuaranteedLegendary = raw.GuaranteedLegendary
	pack.GuaranteedEpic = raw.GuaranteedEpic
	pack.GuaranteedRare = raw.GuaranteedRare
	pack.FixedHeroes = raw.FixedHeroes
	pack.PoolTag = raw.PoolTag
	pack.UniqueHeroes = raw.UniqueHeroes
	pack.LegendaryPity = raw.LegendaryPity
	pack.RuneChancePct = raw.RuneChancePct
	pack.RuneCount = raw.RuneCount
	pack.RuneMinRarity = raw.RuneMinRarity
	pack.RuneMaxRarity = raw.RuneMaxRarity
	pack.GearChancePct = raw.GearChancePct
	pack.GearMinRarity = raw.GearMinRarity
	pack.GearMaxRarity = raw.GearMaxRarity
	total := 0
	for name, rarity := range rarityNames {
		if rarity > RarityLegendary {
			continue
		}
		pack.Weights[rarity] = raw.Weights[name]
		total += raw.Weights[name]
	}
	guaranteed := len(pack.FixedHeroes) + pack.GuaranteedLegendary + pack.GuaranteedEpic + pack.GuaranteedRare
	if pack.HeroCount <= 0 || pack.HeroCount > maxHeroesPerPack || guaranteed > pack.HeroCount ||
		pack.OpenedCounter < 0 || pack.OpenedCounter >= StoreCounterCount || total != heroWeightTotal {
		return fmt.Errorf("invalid hero pack contents")
	}
	for _, id := range pack.FixedHeroes {
		if _, ok := c.heroIndex[id]; !ok {
			return fmt.Errorf("fixed hero %s is not in the hero pool", id)
		}
	}
	return nil
}
