package battle

import (
	"cmp"
	"encoding/json"
	"fmt"
	"slices"
)

const (
	pvpLevelSalt        = 0x51ED27
	pvpBotSalt          = 0x7A3C11
	pvpSquadSalt        = 0x2F90D3
	pvpDriftSalt        = 0x1C8A77
	pvpEventSalt        = 0x4E11A9
	pvpEventSkillJitter = 25
	PvpMaxLevel         = 100
	SecondsPerDay       = 86400
	mixGolden           = 0x9E3779B1
	mixFirst            = 0x85EBCA77
	mixSecond           = 0xC2B2AE3D
	defaultOpponentSeed = 1
	percentScale        = 100
	maxPvpBots          = 256
	maxPvpSquads        = 1024
	maxPvpEvents        = 16
	maxPvpGuildEvents   = 8
)

func pvpMix(a, b uint32) uint32 {
	h := a ^ (b * mixGolden)
	h ^= h >> 15
	h *= mixFirst
	h ^= h >> 13
	h *= mixSecond
	return h ^ (h >> 16)
}

func pvpRoll(seed, salt uint32, count int) int {
	if count <= 0 {
		return 0
	}
	return int(pvpMix(seed, salt) % uint32(count))
}

type pvpSquad struct {
	strength   int
	eventsOnly bool
	elite      bool
	cells      []Unit
	runes      []string
}

type pvpEvent struct {
	changer *Changer
	rules   *squadRules
	pools   [][]int
}

type pvpTables struct {
	windowBelow, windowAbove, botDailyDrift, levelSpread int
	candidates                                           int
	eventDuration, eventBannerWindow, topBotBanners      int64
	guildEveryCycles                                     int64
	statBudget                                           bool
	bots                                                 []int
	squads                                               []pvpSquad
	ladder                                               []int
	events, guildEvents                                  []pvpEvent
}

func (t *pvpTables) weakestFirst(squads []int) {
	slices.SortStableFunc(squads, func(squad, other int) int {
		return cmp.Or(
			cmp.Compare(eliteRank(t.squads[squad]), eliteRank(t.squads[other])),
			cmp.Compare(t.squads[squad].strength, t.squads[other].strength),
		)
	})
}

func (t *pvpTables) poolEvents(events []pvpEvent, characters map[string]*character) error {
	d := &Data{characters: characters}
	for squad := range t.squads {
		heroes, err := d.fieldedHeroes(t.squads[squad].cells)
		if err != nil {
			return fmt.Errorf("pvp rules: squad %d: %w", squad, err)
		}
		for e := range events {
			event := &events[e]
			for side := 0; event.rules != nil && side < event.rules.poolCount(); side++ {
				if event.rules.fieldsAgainst(heroes, side) {
					event.pools[side] = append(event.pools[side], squad)
				}
			}
		}
	}
	for e := range events {
		for side, pool := range events[e].pools {
			if len(pool) == 0 {
				return fmt.Errorf("pvp rules: event %d side %d has no squad to field", e, side)
			}
			t.weakestFirst(pool)
		}
	}
	return nil
}

func loadPvpTables(data []byte, characters map[string]*character) (*pvpTables, error) {
	type rawEvent struct {
		Effects    []rawChangerEffect `json:"effects"`
		SquadRules *rawSquadRules     `json:"squad_rules"`
	}
	var root struct {
		Matchmaking struct {
			WindowBelow   int `json:"window_below"`
			WindowAbove   int `json:"window_above"`
			BotDailyDrift int `json:"bot_daily_drift"`
			LevelSpread   int `json:"level_spread"`
		} `json:"matchmaking"`
		Squads struct {
			Candidates int `json:"candidates"`
		} `json:"squads"`
		Events struct {
			DurationSeconds         int64      `json:"duration_seconds"`
			MatchmakingBannerWindow int64      `json:"matchmaking_banner_window"`
			TopBotBanners           int64      `json:"top_bot_banners"`
			GuildEveryCycles        int64      `json:"guild_every_cycles"`
			Catalog                 []rawEvent `json:"catalog"`
			GuildCatalog            []rawEvent `json:"guild_catalog"`
		} `json:"events"`
		Battle struct {
			StatBudget bool `json:"stat_budget"`
		} `json:"battle"`
		Bots []struct {
			Trophies int `json:"trophies"`
		} `json:"bots"`
		SquadPool []struct {
			Strength   int        `json:"strength"`
			EventsOnly bool       `json:"events_only"`
			Elite      bool       `json:"elite"`
			Cells      []*rawCell `json:"cells"`
			Runes      []string   `json:"runes"`
		} `json:"squad_pool"`
	}
	if err := json.Unmarshal(data, &root); err != nil {
		return nil, fmt.Errorf("pvp rules: %w", err)
	}
	t := &pvpTables{
		windowBelow: root.Matchmaking.WindowBelow, windowAbove: root.Matchmaking.WindowAbove,
		botDailyDrift: root.Matchmaking.BotDailyDrift, levelSpread: root.Matchmaking.LevelSpread,
		candidates:    root.Squads.Candidates,
		eventDuration: root.Events.DurationSeconds, eventBannerWindow: root.Events.MatchmakingBannerWindow,
		topBotBanners: root.Events.TopBotBanners, guildEveryCycles: root.Events.GuildEveryCycles,
		statBudget: root.Battle.StatBudget,
	}
	if len(root.Bots) == 0 || len(root.Bots) > maxPvpBots || len(root.SquadPool) == 0 ||
		len(root.SquadPool) > maxPvpSquads || len(root.Events.Catalog) == 0 ||
		len(root.Events.Catalog) > maxPvpEvents || len(root.Events.GuildCatalog) > maxPvpGuildEvents ||
		t.eventDuration <= 0 {
		return nil, fmt.Errorf("pvp rules: incomplete tables")
	}
	if len(root.Events.GuildCatalog) > 0 && t.guildEveryCycles <= 0 {
		return nil, fmt.Errorf("pvp rules: no guild event cadence")
	}
	for _, bot := range root.Bots {
		t.bots = append(t.bots, bot.Trophies)
	}
	for i, raw := range root.SquadPool {
		if len(raw.Cells) > SlotCount {
			return nil, fmt.Errorf("pvp rules: squad %d has too many cells", i)
		}
		if len(raw.Runes) > RuneSlotCount {
			return nil, fmt.Errorf("pvp rules: squad %d has %d runes", i, len(raw.Runes))
		}
		squad := pvpSquad{
			strength: raw.Strength, eventsOnly: raw.EventsOnly, elite: raw.Elite,
			runes: append([]string{}, raw.Runes...),
		}
		for slot, cell := range raw.Cells {
			if cell == nil {
				continue
			}
			gear, err := wornSlots(cell.Gear)
			if err != nil {
				return nil, fmt.Errorf("pvp rules: squad %d slot %d: %w", i, slot, err)
			}
			squad.cells = append(squad.cells, Unit{
				Slot: slot, ID: cell.ID, Tier: ParseTier(cell.Tier).String(), Level: max(cellLevel(cell.Level), 1),
				Ascended: cell.Ascended && gear != nil, Gear: gear,
			})
		}
		t.squads = append(t.squads, squad)
		if !squad.eventsOnly {
			t.ladder = append(t.ladder, i)
		}
	}
	if len(t.ladder) == 0 {
		return nil, fmt.Errorf("pvp rules: no ladder squads")
	}
	t.weakestFirst(t.ladder)
	for _, list := range []struct {
		raw []rawEvent
		out *[]pvpEvent
	}{{root.Events.Catalog, &t.events}, {root.Events.GuildCatalog, &t.guildEvents}} {
		for _, raw := range list.raw {
			changer, err := parseChanger(raw.Effects)
			if err != nil {
				return nil, fmt.Errorf("pvp rules: %w", err)
			}
			rules, err := parseSquadRules(raw.SquadRules)
			if err != nil {
				return nil, fmt.Errorf("pvp rules: %w", err)
			}
			event := pvpEvent{changer: changer, rules: rules}
			if rules != nil {
				event.pools = make([][]int, rules.poolCount())
			}
			*list.out = append(*list.out, event)
		}
		if err := t.poolEvents(*list.out, characters); err != nil {
			return nil, err
		}
	}
	return t, nil
}

type PvpOpponent struct {
	Bot      int    `json:"bot"`
	Squad    int    `json:"squad"`
	Level    int    `json:"level"`
	Trophies int    `json:"trophies"`
	Seed     uint32 `json:"seed"`
}

func absInt(value int) int {
	if value < 0 {
		return -value
	}
	return value
}

func (t *pvpTables) botTrophies(bot int, day int64) int {
	drift := t.botDailyDrift
	seed := pvpMix(uint32(bot), uint32(day))
	return max(t.bots[bot]+pvpRoll(seed, pvpDriftSalt, drift*2+1)-drift, 0)
}

func (t *pvpTables) pickBot(trophies int, day int64, seed uint32) int {
	inWindow, closest, closestDistance := 0, 0, -1
	for bot := range t.bots {
		delta := t.botTrophies(bot, day) - trophies
		if delta >= -t.windowBelow && delta <= t.windowAbove {
			inWindow++
		}
		if closestDistance < 0 || absInt(delta) < closestDistance {
			closestDistance, closest = absInt(delta), bot
		}
	}
	if inWindow == 0 {
		return closest
	}
	pick := pvpRoll(seed, pvpBotSalt, inWindow)
	for bot := range t.bots {
		delta := t.botTrophies(bot, day) - trophies
		if delta >= -t.windowBelow && delta <= t.windowAbove {
			if pick == 0 {
				return bot
			}
			pick--
		}
	}
	return closest
}

func (t *pvpTables) strengthRank(pool []int, bot int) int {
	if len(t.bots) == 0 {
		return 0
	}
	topTrophies := t.bots[len(t.bots)-1]
	if topTrophies <= 0 {
		return 0
	}
	return int(int64(t.bots[bot]) * int64(len(pool)-1) / int64(topTrophies))
}

func (t *pvpTables) botStrengthRank(bot int) int {
	return t.strengthRank(t.ladder, bot)
}

func (t *pvpTables) pickSquadFrom(pool []int, bot int, seed uint32) int {
	weakest := min(max(t.strengthRank(pool, bot)-t.candidates/2, 0), max(len(pool)-t.candidates, 0))
	span := min(t.candidates, len(pool))
	return pool[weakest+pvpRoll(seed, pvpSquadSalt, span)]
}

func (t *pvpTables) pickSquad(bot int, seed uint32) int {
	return t.pickSquadFrom(t.ladder, bot, seed)
}

func (t *pvpTables) eventFor(cycle int64) *pvpEvent {
	if len(t.guildEvents) > 0 && cycle >= 0 && cycle%t.guildEveryCycles == 0 {
		return &t.guildEvents[(cycle/t.guildEveryCycles)%int64(len(t.guildEvents))]
	}
	return &t.events[cycle%int64(len(t.events))]
}

func (t *pvpTables) eventPool(cycle int64, side int) []int {
	event := t.eventFor(cycle)
	if event.rules == nil {
		return t.ladder
	}
	if side < 0 || side >= len(event.pools) {
		side = 0
	}
	return event.pools[side]
}

func eliteRank(squad pvpSquad) int {
	if squad.elite {
		return 1
	}
	return 0
}

func wornSlots(worn []string) ([]string, error) {
	if len(worn) > gearSlotCapacity {
		return nil, fmt.Errorf("%d gear slots", len(worn))
	}
	if !slices.ContainsFunc(worn, func(id string) bool { return id != "" }) {
		return nil, nil
	}
	slots := make([]string, gearSlotCapacity)
	copy(slots, worn)
	return slots, nil
}

func (d *Data) checkPvpLoadouts() error {
	for i, squad := range d.pvp.squads {
		for _, id := range squad.runes {
			if _, ok := d.runesByID[id]; !ok {
				return fmt.Errorf("pvp rules: squad %d has unknown rune %s", i, id)
			}
		}
		for _, cell := range squad.cells {
			for _, id := range cell.Gear {
				if _, ok := d.gear[id]; id != "" && !ok {
					return fmt.Errorf("pvp rules: squad %d wears unknown gear %s", i, id)
				}
			}
		}
	}
	return nil
}

func (d *Data) PvpStatBudget() bool { return d.pvp.statBudget }

func (d *Data) PvpBotCount() int   { return len(d.pvp.bots) }
func (d *Data) PvpSquadCount() int { return len(d.pvp.squads) }

func (d *Data) PvpBotSquad(bot int, seed uint32) int { return d.pvp.pickSquad(bot, seed) }

func (d *Data) pvpOpponentIn(pool []int, bot, level int, day int64, seed uint32) PvpOpponent {
	t := d.pvp
	if seed == 0 {
		seed = defaultOpponentSeed
	}
	o := PvpOpponent{Bot: bot, Seed: seed, Trophies: t.botTrophies(bot, day)}
	o.Level = min(max(level+pvpRoll(seed, pvpLevelSalt, t.levelSpread*2+1)-t.levelSpread, 1), PvpMaxLevel)
	o.Squad = t.pickSquadFrom(pool, bot, seed)
	return o
}

func (d *Data) PvpOpponentFromBot(bot, level int, day int64, seed uint32) PvpOpponent {
	return d.pvpOpponentIn(d.pvp.ladder, bot, level, day, seed)
}

func (d *Data) PvpEventOpponentFromBot(bot, level int, now int64, side int, seed uint32) PvpOpponent {
	pool := d.pvp.eventPool(d.PvpEventCycle(now), side)
	return d.pvpOpponentIn(pool, bot, level, now/SecondsPerDay, seed)
}

func (d *Data) PvpLadderOpponent(level, trophies int, day int64, seed uint32) PvpOpponent {
	pickSeed := seed
	if pickSeed == 0 {
		pickSeed = defaultOpponentSeed
	}
	return d.PvpOpponentFromBot(d.pvp.pickBot(trophies, day, pickSeed), level, day, seed)
}

func (t *pvpTables) botBanners(bot int, cycle, elapsed int64) int {
	bots := int64(len(t.bots))
	standing := int64(bot+1) * int64(bot+1)
	full := t.topBotBanners * standing / (bots * bots)
	seed := pvpMix(uint32(bot), uint32(cycle))
	full = full * int64(100-pvpEventSkillJitter+pvpRoll(seed, pvpEventSalt, pvpEventSkillJitter*2+1)) / percentScale
	return int(full * elapsed / t.eventDuration)
}

func (t *pvpTables) pickEventBot(banners int, cycle, elapsed int64, seed uint32) int {
	inWindow, closest, closestDistance := 0, 0, -1
	for bot := range t.bots {
		distance := absInt(t.botBanners(bot, cycle, elapsed) - banners)
		if int64(distance) <= t.eventBannerWindow {
			inWindow++
		}
		if closestDistance < 0 || distance < closestDistance {
			closestDistance, closest = distance, bot
		}
	}
	if inWindow == 0 {
		return closest
	}
	pick := pvpRoll(seed, pvpBotSalt, inWindow)
	for bot := range t.bots {
		if int64(absInt(t.botBanners(bot, cycle, elapsed)-banners)) <= t.eventBannerWindow {
			if pick == 0 {
				return bot
			}
			pick--
		}
	}
	return closest
}

func (d *Data) PvpEventCycle(now int64) int64 {
	return now / d.pvp.eventDuration
}

func (d *Data) PvpEventOpponent(level, banners int, now int64, side int, seed uint32) PvpOpponent {
	pickSeed := seed
	if pickSeed == 0 {
		pickSeed = defaultOpponentSeed
	}
	bot := d.pvp.pickEventBot(banners, d.PvpEventCycle(now), now%d.pvp.eventDuration, pickSeed)
	return d.PvpEventOpponentFromBot(bot, level, now, side, seed)
}

func (d *Data) PvpRoster(o PvpOpponent) (Side, error) {
	if o.Squad < 0 || o.Squad >= len(d.pvp.squads) {
		return Side{}, fmt.Errorf("no pvp squad %d", o.Squad)
	}
	squad := d.pvp.squads[o.Squad]
	roster := Side{Units: make([]Unit, len(squad.cells)), Runes: append([]string{}, squad.runes...)}
	for i, unit := range squad.cells {
		unit.Gear = slices.Clone(unit.Gear)
		roster.Units[i] = unit
	}
	for _, unit := range roster.Units {
		if _, ok := d.characters[unit.ID]; !ok {
			return Side{}, fmt.Errorf("pvp unit %s is not in the catalog", unit.ID)
		}
	}
	return roster, nil
}

func (d *Data) PvpEventChanger(cycle int64) *Changer {
	return d.pvp.eventFor(cycle).changer
}

func (d *Data) PvpEventSides(cycle int64) int {
	if rules := d.pvp.eventFor(cycle).rules; rules != nil {
		return len(rules.sides)
	}
	return 0
}

func (d *Data) CheckPvpEventSquad(cycle int64, side int, squad Side) error {
	rules := d.pvp.eventFor(cycle).rules
	if rules == nil {
		return nil
	}
	heroes, err := d.fieldedHeroes(squad.Units)
	if err != nil {
		return err
	}
	if _, v := rules.judge(heroes, side); v != nil {
		return v
	}
	return nil
}
