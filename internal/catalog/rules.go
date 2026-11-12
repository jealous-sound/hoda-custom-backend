package catalog

import (
	"encoding/json"
	"fmt"
	"slices"
)

const (
	CampaignMaxMaps         = 16
	CampaignMaxNodes        = 12
	NexusMaxEvents          = 40
	NexusMaxDifficulties    = 6
	NexusMaxNodes           = 8
	NexusKeyCount           = 4
	NexusKeyFirstID         = 2001
	offlineMasteryGems      = 1
	gearRarityCount         = 6
	runeGoldBuff            = "gold"
	runeExperienceBuff      = "xp"
	supportedNewPlayerShape = 2
)

var offlineWinsToMaster = []int{3, 5, 7, 10, 12}

type CampaignNode struct {
	EnergyCost    int
	Gold          int
	XP            int
	Gems          int
	FirstWinGems  int
	MasteryGems   int
	Grind         bool
	WinsToMaster  int
	ChallengeGold int
	ChallengeXP   int
	ChallengeGems int
	GrindGold     int
	GrindXP       int
	GrindGems     int
}

type CampaignMap struct {
	ID    string
	Nodes []CampaignNode
}

func (m CampaignMap) BossNode() int {
	boss := len(m.Nodes) - 1
	if boss >= 0 && m.Nodes[boss].Grind {
		boss--
	}
	return boss
}

func (m CampaignMap) GrindNode() int {
	last := len(m.Nodes) - 1
	if last >= 0 && m.Nodes[last].Grind {
		return last
	}
	return -1
}

type NexusNode struct {
	Gold         int
	XP           int
	PotionReward int
	RewardRarity []int
}

type NexusDifficulty struct {
	ID         string
	EnergyCost int
	PotionCost int
	Nodes      []NexusNode
}

type NexusEvent struct {
	ID           string
	MaxRuns      int
	UnlockKey    int
	UnlockKeyQty int
	Difficulties []NexusDifficulty
}

type PvpRules struct {
	Trophies struct {
		BaseGain      int `json:"base_gain"`
		BaseLoss      int `json:"base_loss"`
		PointsPerStep int `json:"points_per_step"`
		MinGain       int `json:"min_gain"`
		MaxGain       int `json:"max_gain"`
		MinLoss       int `json:"min_loss"`
		MaxLoss       int `json:"max_loss"`
	} `json:"trophies"`
	Matchmaking struct {
		WindowBelow   int `json:"window_below"`
		WindowAbove   int `json:"window_above"`
		DailyNexts    int `json:"daily_nexts"`
		BotDailyDrift int `json:"bot_daily_drift"`
		LevelSpread   int `json:"level_spread"`
	} `json:"matchmaking"`
	Squads struct {
		Candidates int `json:"candidates"`
	} `json:"squads"`
	Defense struct {
		AttackIntervalSeconds int64 `json:"attack_interval_seconds"`
		MaxAttacksPerLogin    int64 `json:"max_attacks_per_login"`
		WinPct                int   `json:"win_pct"`
		LossSharePct          int   `json:"loss_share_pct"`
	} `json:"defense"`
	Rewards struct {
		WinGoldBase     int `json:"win_gold_base"`
		WinGoldPerLevel int `json:"win_gold_per_level"`
		LossGoldPct     int `json:"loss_gold_pct"`
		WinXPBase       int `json:"win_xp_base"`
		WinXPPerLevel   int `json:"win_xp_per_level"`
		LossXPPct       int `json:"loss_xp_pct"`
	} `json:"rewards"`
	Events PvpEvents `json:"events"`
	Bots   []struct {
		Name     string `json:"name"`
		Trophies int    `json:"trophies"`
	} `json:"bots"`
	SquadPool []struct{} `json:"squad_pool"`
}

type PvpEvents struct {
	DurationSeconds           int64             `json:"duration_seconds"`
	WinBannersStart           int               `json:"win_banners_start"`
	WinBannersPeakBonus       int               `json:"win_banners_peak_bonus"`
	WinBannersStepsToPeak     int               `json:"win_banners_steps_to_peak"`
	WinBannersDeclineSlowdown int               `json:"win_banners_decline_slowdown"`
	WinBannersFloorPct        int               `json:"win_banners_floor_pct"`
	LossBanners               int               `json:"loss_banners"`
	TopBotBanners             int64             `json:"top_bot_banners"`
	LadderBanners             int               `json:"ladder_banners"`
	Catalog                   []PvpEventDetails `json:"catalog"`
	GuildEveryCycles          int64             `json:"guild_every_cycles"`
	GuildCatalog              []PvpEventDetails `json:"guild_catalog"`
}

func (e *PvpEvents) IsGuildCycle(cycle int64) bool {
	return len(e.GuildCatalog) > 0 && e.GuildEveryCycles > 0 && cycle%e.GuildEveryCycles == 0
}

func (e *PvpEvents) ForCycle(cycle int64) *PvpEventDetails {
	if e.IsGuildCycle(cycle) {
		return &e.GuildCatalog[int((cycle/e.GuildEveryCycles)%int64(len(e.GuildCatalog)))]
	}
	return &e.Catalog[int(cycle%int64(len(e.Catalog)))]
}

func (e *PvpEvents) CycleStart(cycle int64) int64 {
	return cycle * e.DurationSeconds
}

type PvpPrize struct {
	Kind  string `json:"kind"`
	ID    string `json:"id"`
	Index int    `json:"index"`
	Count int    `json:"count"`
}

type PvpReward struct {
	Gold   int        `json:"gold"`
	Gems   int        `json:"gems"`
	Prizes []PvpPrize `json:"prizes"`
}

type PvpMilestone struct {
	Banners int `json:"banners"`
	PvpReward
}

type PvpRankReward struct {
	First int `json:"first"`
	Last  int `json:"last"`
	PvpReward
}

type PvpEventDetails struct {
	Milestones          []PvpMilestone  `json:"milestones"`
	RankRewards         []PvpRankReward `json:"rank_rewards"`
	GuildMinimumBanners int             `json:"guild_minimum_banners"`
	GuildRankRewards    []PvpRankReward `json:"guild_rank_rewards"`
}

func (d *PvpEventDetails) GuildReward(rank int) (PvpReward, bool) {
	for _, tier := range d.GuildRankRewards {
		if rank >= tier.First && rank <= tier.Last {
			return tier.PvpReward, true
		}
	}
	return PvpReward{}, false
}

type Mission struct {
	ID       string `json:"id"`
	Stat     string `json:"stat"`
	Target   int64  `json:"target"`
	Gold     int    `json:"gold"`
	Gems     int    `json:"gems"`
	Map      int    `json:"map"`
	Rarity   Rarity `json:"rarity"`
	HeroTier int    `json:"hero_tier"`
	Faction  int    `json:"faction"`
}

type TutorialRewards struct {
	Giant         string
	GemPackGems   int
	GiftHeroes    []string
	GiftRunes     []string
	StarterHeroes []string
}

type rawCampaignMap struct {
	ID    string `json:"id"`
	Nodes []struct {
		EnergyCost    int  `json:"energy_cost"`
		Gold          int  `json:"gold"`
		XP            int  `json:"xp"`
		Gems          int  `json:"gems"`
		Grind         bool `json:"grind"`
		WinsToMaster  int  `json:"wins_to_master"`
		MasteryGems   *int `json:"mastery_gems"`
		ChallengeGold int  `json:"challenge_gold"`
		ChallengeXP   int  `json:"challenge_xp"`
		ChallengeGems int  `json:"challenge_gems"`
		GrindGold     int  `json:"grind_gold"`
		GrindXP       int  `json:"grind_xp"`
		GrindGems     int  `json:"grind_gems"`
	} `json:"nodes"`
}

type rawNexusEvent struct {
	ID           string `json:"id"`
	MaxRuns      int    `json:"max_runs"`
	UnlockKey    int    `json:"unlock_key"`
	UnlockKeyQty int    `json:"unlock_key_qty"`
	Difficulties []struct {
		ID         string `json:"id"`
		EnergyCost int    `json:"energy_cost"`
		PotionCost int    `json:"potion_cost"`
		Nodes      []struct {
			Gold         int   `json:"gold"`
			XP           int   `json:"xp"`
			PotionReward int   `json:"potion_reward"`
			RewardRarity []int `json:"reward_rarity"`
		} `json:"nodes"`
	} `json:"difficulties"`
}

type rawTutorialRewards struct {
	Giant struct {
		Hero string `json:"hero"`
	} `json:"giant"`
	HiddenGemPack struct {
		Gems int `json:"gems"`
	} `json:"hidden_gem_pack"`
	PartingGift struct {
		Heroes []string `json:"heroes"`
		Runes  []string `json:"runes"`
	} `json:"parting_gift"`
}

type rawNewPlayer struct {
	Version    int `json:"version"`
	Characters []struct {
		ID string `json:"id"`
	} `json:"characters"`
}

type rawGearValues struct {
	RarityGoldValues    []int `json:"rarity_gold_values"`
	PrestigeSlotGemCost int   `json:"prestige_slot_gem_cost"`
}

type rawRuneBuffs struct {
	Runes []struct {
		ID        string `json:"id"`
		BuffType  string `json:"buff_type"`
		BuffValue int    `json:"buff_value"`
		CraftTo   string `json:"craft_to"`
	} `json:"runes"`
}

func decodeFile(read func(name string) ([]byte, error), name string, target any) error {
	data, err := read(name)
	if err != nil {
		return fmt.Errorf("read %s: %w", name, err)
	}
	if err := json.Unmarshal(data, target); err != nil {
		return fmt.Errorf("decode %s: %w", name, err)
	}
	return nil
}

func (c *Catalog) loadRules(read func(name string) ([]byte, error)) error {
	var campaign []rawCampaignMap
	var nexus []rawNexusEvent
	var missions struct {
		Missions []Mission `json:"missions"`
	}
	var tutorial rawTutorialRewards
	var newPlayer rawNewPlayer
	var gear rawGearValues
	var runes rawRuneBuffs
	for name, target := range map[string]any{
		"campaign_maps.json":    &campaign,
		"nexus_maps.json":       &nexus,
		"pvp_rules.json":        &c.Pvp,
		"missions.json":         &missions,
		"tutorial_rewards.json": &tutorial,
		"new_player.json":       &newPlayer,
		"gear.json":             &gear,
		"runes.json":            &runes,
	} {
		if err := decodeFile(read, name, target); err != nil {
			return err
		}
	}
	if err := c.parseCampaign(campaign); err != nil {
		return err
	}
	if err := c.parseNexus(nexus); err != nil {
		return err
	}
	if err := c.checkPvp(); err != nil {
		return err
	}
	c.Missions = missions.Missions
	c.missionIndex = map[string]int{}
	for index, mission := range c.Missions {
		c.missionIndex[mission.ID] = index
	}
	if newPlayer.Version != supportedNewPlayerShape {
		return fmt.Errorf("new_player.json version %d, want %d", newPlayer.Version, supportedNewPlayerShape)
	}
	c.Tutorial = TutorialRewards{
		Giant:       tutorial.Giant.Hero,
		GemPackGems: tutorial.HiddenGemPack.Gems,
		GiftHeroes:  tutorial.PartingGift.Heroes,
		GiftRunes:   tutorial.PartingGift.Runes,
	}
	for _, character := range newPlayer.Characters {
		c.Tutorial.StarterHeroes = append(c.Tutorial.StarterHeroes, character.ID)
	}
	for _, hero := range append(append([]string{c.Tutorial.Giant}, c.Tutorial.GiftHeroes...), c.Tutorial.StarterHeroes...) {
		if _, ok := c.heroIndex[hero]; !ok {
			return fmt.Errorf("tutorial rewards name unknown hero %s", hero)
		}
	}
	for _, rune := range c.Tutorial.GiftRunes {
		if !c.RuneKnown(rune) {
			return fmt.Errorf("tutorial rewards name unknown rune %s", rune)
		}
	}
	if len(gear.RarityGoldValues) != gearRarityCount {
		return fmt.Errorf("gear.json has %d rarity gold values, want %d", len(gear.RarityGoldValues), gearRarityCount)
	}
	c.GearSellGold = gear.RarityGoldValues
	c.GearPrestigeGems = gear.PrestigeSlotGemCost
	c.gearIndex = map[string]int{}
	for index, item := range c.Gear {
		c.gearIndex[item.ID] = index
	}
	for _, rune := range runes.Runes {
		index := c.runeIndex[rune.ID]
		c.Runes[index].CraftTo = rune.CraftTo
		switch rune.BuffType {
		case runeGoldBuff:
			c.Runes[index].GoldPct = rune.BuffValue
		case runeExperienceBuff:
			c.Runes[index].XPPct = rune.BuffValue
		}
	}
	for _, rune := range c.Runes {
		if rune.CraftTo != "" && !c.RuneKnown(rune.CraftTo) {
			return fmt.Errorf("rune %s crafts to unknown %s", rune.ID, rune.CraftTo)
		}
	}
	return nil
}

func (c *Catalog) parseCampaign(maps []rawCampaignMap) error {
	if len(maps) == 0 || len(maps) > CampaignMaxMaps {
		return fmt.Errorf("campaign has %d maps, want 1-%d", len(maps), CampaignMaxMaps)
	}
	for _, raw := range maps {
		m := CampaignMap{ID: raw.ID}
		if len(raw.Nodes) == 0 || len(raw.Nodes) > CampaignMaxNodes {
			return fmt.Errorf("campaign %s has %d nodes", raw.ID, len(raw.Nodes))
		}
		for index, rawNode := range raw.Nodes {
			node := CampaignNode{
				EnergyCost: rawNode.EnergyCost, Gold: rawNode.Gold, XP: rawNode.XP, FirstWinGems: rawNode.Gems,
				Grind: rawNode.Grind, WinsToMaster: rawNode.WinsToMaster,
				ChallengeGold: rawNode.ChallengeGold, ChallengeXP: rawNode.ChallengeXP,
				ChallengeGems: rawNode.ChallengeGems, GrindGold: rawNode.GrindGold, GrindXP: rawNode.GrindXP,
				GrindGems: rawNode.GrindGems,
			}
			if node.Grind {
				if index != len(raw.Nodes)-1 || index == 0 {
					return fmt.Errorf("campaign %s grind node must follow a boss as the final node", raw.ID)
				}
				node.WinsToMaster = 0
			} else {
				if node.WinsToMaster <= 0 {
					node.WinsToMaster = offlineWinsToMaster[index%len(offlineWinsToMaster)]
				}
				node.MasteryGems = offlineMasteryGems
				if rawNode.MasteryGems != nil {
					node.MasteryGems = max(*rawNode.MasteryGems, 0)
				}
			}
			m.Nodes = append(m.Nodes, node)
		}
		c.Campaign = append(c.Campaign, m)
	}
	return nil
}

func (c *Catalog) parseNexus(events []rawNexusEvent) error {
	if len(events) == 0 || len(events) > NexusMaxEvents {
		return fmt.Errorf("nexus has %d events, want 1-%d", len(events), NexusMaxEvents)
	}
	for _, raw := range events {
		event := NexusEvent{ID: raw.ID, MaxRuns: raw.MaxRuns, UnlockKey: raw.UnlockKey, UnlockKeyQty: raw.UnlockKeyQty}
		if len(raw.Difficulties) == 0 || len(raw.Difficulties) > NexusMaxDifficulties {
			return fmt.Errorf("nexus %s has %d difficulties", raw.ID, len(raw.Difficulties))
		}
		for _, rawDifficulty := range raw.Difficulties {
			difficulty := NexusDifficulty{
				ID: rawDifficulty.ID, EnergyCost: rawDifficulty.EnergyCost, PotionCost: rawDifficulty.PotionCost,
			}
			if len(rawDifficulty.Nodes) == 0 || len(rawDifficulty.Nodes) > NexusMaxNodes {
				return fmt.Errorf("nexus %s has a difficulty with %d nodes", raw.ID, len(rawDifficulty.Nodes))
			}
			for _, rawNode := range rawDifficulty.Nodes {
				difficulty.Nodes = append(difficulty.Nodes, NexusNode{
					Gold: rawNode.Gold, XP: rawNode.XP, PotionReward: rawNode.PotionReward,
					RewardRarity: rawNode.RewardRarity,
				})
			}
			event.Difficulties = append(event.Difficulties, difficulty)
		}
		c.Nexus = append(c.Nexus, event)
	}
	return nil
}

func (c *Catalog) checkPvp() error {
	events := c.Pvp.Events
	if len(c.Pvp.Bots) == 0 || len(c.Pvp.SquadPool) == 0 || c.Pvp.Squads.Candidates <= 0 ||
		c.Pvp.Defense.AttackIntervalSeconds <= 0 ||
		len(events.Catalog) == 0 || events.DurationSeconds <= 0 ||
		events.WinBannersStepsToPeak <= 0 || events.WinBannersDeclineSlowdown <= 0 || events.LadderBanners <= 0 ||
		c.Pvp.Trophies.PointsPerStep <= 0 || c.Pvp.Trophies.MaxGain <= c.Pvp.Trophies.MinGain {
		return fmt.Errorf("pvp_rules.json is incomplete")
	}
	if len(events.GuildCatalog) > 0 && events.GuildEveryCycles <= 0 {
		return fmt.Errorf("pvp_rules.json has guild events without a cadence")
	}
	for _, event := range append(slices.Clone(events.Catalog), events.GuildCatalog...) {
		groups := [][]PvpReward{
			milestoneRewards(event.Milestones), rankRewards(event.RankRewards), rankRewards(event.GuildRankRewards),
		}
		for _, rewards := range groups {
			for _, reward := range rewards {
				for _, prize := range reward.Prizes {
					if err := c.checkPrize(prize); err != nil {
						return err
					}
				}
			}
		}
	}
	return nil
}

func milestoneRewards(milestones []PvpMilestone) []PvpReward {
	rewards := make([]PvpReward, len(milestones))
	for i, milestone := range milestones {
		rewards[i] = milestone.PvpReward
	}
	return rewards
}

func rankRewards(ranks []PvpRankReward) []PvpReward {
	rewards := make([]PvpReward, len(ranks))
	for i, rank := range ranks {
		rewards[i] = rank.PvpReward
	}
	return rewards
}

func (c *Catalog) checkPrize(prize PvpPrize) error {
	switch prize.Kind {
	case PrizeUnit:
		if _, ok := c.heroIndex[prize.ID]; !ok {
			return fmt.Errorf("pvp prize names unknown hero %s", prize.ID)
		}
	case PrizeGear:
		if _, ok := c.GearIndex(prize.ID); !ok {
			return fmt.Errorf("pvp prize names unknown gear %s", prize.ID)
		}
	case PrizeVoucher:
		if prize.Index < 0 || prize.Index >= VoucherCapacity {
			return fmt.Errorf("pvp voucher prize %s has index %d", prize.ID, prize.Index)
		}
	case PrizeTome:
	default:
		return fmt.Errorf("pvp prize of unknown kind %s", prize.Kind)
	}
	return nil
}

const (
	PrizeUnit    = "unit"
	PrizeGear    = "gear"
	PrizeTome    = "tome"
	PrizeVoucher = "voucher"
)

func (c *Catalog) GearIndex(id string) (int, bool) {
	if c.gearIndex == nil {
		for index, item := range c.Gear {
			if item.ID == id {
				return index, true
			}
		}
		return 0, false
	}
	index, ok := c.gearIndex[id]
	return index, ok
}

func (c *Catalog) RuneByID(id string) (Rune, bool) {
	index, ok := c.runeIndex[id]
	if !ok {
		return Rune{}, false
	}
	return c.Runes[index], true
}

func (c *Catalog) MissionIndex(id string) (int, bool) {
	index, ok := c.missionIndex[id]
	return index, ok
}

func (c *Catalog) GearSellValue(item Gear) int {
	if item.Rarity < 1 || item.Rarity > len(c.GearSellGold) {
		return 0
	}
	return c.GearSellGold[item.Rarity-1]
}
