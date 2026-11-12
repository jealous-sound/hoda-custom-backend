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
	pvpRuneSalt         = 0x64B8E1
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
	maxPvpSquads        = 640
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
	strength int
	cells    []Unit
}

type pvpTables struct {
	windowBelow, windowAbove, botDailyDrift, levelSpread int
	candidates                                           int
	firstRuneLevel, levelsPerRune, levelsPerRarity       int
	maxRuneRarity                                        int
	eventDuration, eventBannerWindow, topBotBanners      int64
	guildEveryCycles                                     int64
	statBudget                                           bool
	bots                                                 []int
	squads                                               []pvpSquad
	squadsWeakestFirst                                   []int
	eventChangers, guildChangers                         []*Changer
}

func loadPvpTables(data []byte) (*pvpTables, error) {
	type rawEvent struct {
		Effects []rawChangerEffect `json:"effects"`
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
		Runes struct {
			FirstRuneLevel  int `json:"first_rune_level"`
			LevelsPerRune   int `json:"levels_per_rune"`
			LevelsPerRarity int `json:"levels_per_rarity"`
			MaxRarity       int `json:"max_rarity"`
		} `json:"runes"`
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
			Strength int        `json:"strength"`
			Cells    []*rawCell `json:"cells"`
		} `json:"squad_pool"`
	}
	if err := json.Unmarshal(data, &root); err != nil {
		return nil, fmt.Errorf("pvp rules: %w", err)
	}
	t := &pvpTables{
		windowBelow: root.Matchmaking.WindowBelow, windowAbove: root.Matchmaking.WindowAbove,
		botDailyDrift: root.Matchmaking.BotDailyDrift, levelSpread: root.Matchmaking.LevelSpread,
		candidates: root.Squads.Candidates, firstRuneLevel: root.Runes.FirstRuneLevel,
		levelsPerRune: root.Runes.LevelsPerRune, levelsPerRarity: root.Runes.LevelsPerRarity,
		maxRuneRarity: root.Runes.MaxRarity,
		eventDuration: root.Events.DurationSeconds, eventBannerWindow: root.Events.MatchmakingBannerWindow,
		topBotBanners: root.Events.TopBotBanners, guildEveryCycles: root.Events.GuildEveryCycles,
		statBudget: root.Battle.StatBudget,
	}
	if len(root.Bots) == 0 || len(root.Bots) > maxPvpBots || len(root.SquadPool) == 0 ||
		len(root.SquadPool) > maxPvpSquads || len(root.Events.Catalog) == 0 ||
		len(root.Events.Catalog) > maxPvpEvents || len(root.Events.GuildCatalog) > maxPvpGuildEvents ||
		t.eventDuration <= 0 || t.levelsPerRune == 0 || t.levelsPerRarity == 0 {
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
		squad := pvpSquad{strength: raw.Strength}
		for slot, cell := range raw.Cells {
			if cell != nil {
				squad.cells = append(squad.cells, Unit{
					Slot: slot, ID: cell.ID, Tier: ParseTier(cell.Tier).String(), Level: max(cellLevel(cell.Level), 1),
				})
			}
		}
		t.squads = append(t.squads, squad)
		t.squadsWeakestFirst = append(t.squadsWeakestFirst, i)
	}
	slices.SortStableFunc(t.squadsWeakestFirst, func(squad, other int) int {
		return cmp.Compare(t.squads[squad].strength, t.squads[other].strength)
	})
	for _, list := range []struct {
		raw []rawEvent
		out *[]*Changer
	}{{root.Events.Catalog, &t.eventChangers}, {root.Events.GuildCatalog, &t.guildChangers}} {
		for _, event := range list.raw {
			changer, err := parseChanger(event.Effects)
			if err != nil {
				return nil, fmt.Errorf("pvp rules: %w", err)
			}
			*list.out = append(*list.out, changer)
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

func (t *pvpTables) botStrengthRank(bot int) int {
	if len(t.bots) < 2 {
		return 0
	}
	return int(int64(bot) * int64(len(t.squads)-1) / int64(len(t.bots)-1))
}

func (t *pvpTables) pickSquad(bot int, seed uint32) int {
	weakest := min(max(t.botStrengthRank(bot)-t.candidates/2, 0), max(len(t.squads)-t.candidates, 0))
	span := min(t.candidates, len(t.squads))
	return t.squadsWeakestFirst[weakest+pvpRoll(seed, pvpSquadSalt, span)]
}

func (d *Data) pvpRunes(level int, seed uint32) []string {
	t := d.pvp
	runes := []string{}
	if level < t.firstRuneLevel {
		return runes
	}
	wanted := min(1+(level-t.firstRuneLevel)/t.levelsPerRune, RuneSlotCount)
	rarityCap := min(1+level/t.levelsPerRarity, t.maxRuneRarity)
	var pool []*runeDef
	for _, def := range d.runes {
		if def.rarity <= rarityCap && def.rollable() {
			pool = append(pool, def)
		}
	}
	for i := 0; i < wanted && len(pool) > 0; i++ {
		pick := pvpRoll(seed, pvpRuneSalt+uint32(i), len(pool))
		runes = append(runes, pool[pick].id)
		pool[pick] = pool[len(pool)-1]
		pool = pool[:len(pool)-1]
	}
	return runes
}

func (d *Data) PvpStatBudget() bool { return d.pvp.statBudget }

func (d *Data) PvpBotCount() int   { return len(d.pvp.bots) }
func (d *Data) PvpSquadCount() int { return len(d.pvp.squads) }

func (d *Data) PvpBotSquad(bot int, seed uint32) int { return d.pvp.pickSquad(bot, seed) }

func (d *Data) PvpOpponentFromBot(bot, level int, day int64, seed uint32) PvpOpponent {
	t := d.pvp
	if seed == 0 {
		seed = defaultOpponentSeed
	}
	o := PvpOpponent{Bot: bot, Seed: seed, Trophies: t.botTrophies(bot, day)}
	o.Level = min(max(level+pvpRoll(seed, pvpLevelSalt, t.levelSpread*2+1)-t.levelSpread, 1), PvpMaxLevel)
	o.Squad = t.pickSquad(bot, seed)
	return o
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

func (d *Data) PvpEventOpponent(level, banners int, now int64, seed uint32) PvpOpponent {
	pickSeed := seed
	if pickSeed == 0 {
		pickSeed = defaultOpponentSeed
	}
	bot := d.pvp.pickEventBot(banners, d.PvpEventCycle(now), now%d.pvp.eventDuration, pickSeed)
	return d.PvpOpponentFromBot(bot, level, now/SecondsPerDay, seed)
}

func (d *Data) PvpRoster(o PvpOpponent) (Side, error) {
	if o.Squad < 0 || o.Squad >= len(d.pvp.squads) {
		return Side{}, fmt.Errorf("no pvp squad %d", o.Squad)
	}
	roster := Side{Units: append([]Unit{}, d.pvp.squads[o.Squad].cells...), Runes: d.pvpRunes(o.Level, o.Seed)}
	for _, unit := range roster.Units {
		if _, ok := d.characters[unit.ID]; !ok {
			return Side{}, fmt.Errorf("pvp unit %s is not in the catalog", unit.ID)
		}
	}
	return roster, nil
}

func (d *Data) PvpEventChanger(cycle int64) *Changer {
	t := d.pvp
	if len(t.guildChangers) > 0 && cycle >= 0 && cycle%t.guildEveryCycles == 0 {
		return t.guildChangers[(cycle/t.guildEveryCycles)%int64(len(t.guildChangers))]
	}
	return t.eventChangers[cycle%int64(len(t.eventChangers))]
}
