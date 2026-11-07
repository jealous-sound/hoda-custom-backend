package economy

import (
	"maps"
	"math"
	"slices"
	"time"

	"github.com/jealous-sound/hoda-custom-backend/internal/battle"
	"github.com/jealous-sound/hoda-custom-backend/internal/catalog"
	"github.com/jealous-sound/hoda-custom-backend/internal/profile"
)

const (
	statFirstWin          = "first_win"
	statFirstBoss         = "first_boss"
	statNodeMastered      = "node_mastered"
	statMapMastered       = "map_mastered"
	statMapsUnlocked      = "maps_unlocked"
	statPlayerLevel       = "player_level"
	statHeroLevel         = "hero_level"
	statTierIVLevel       = "tier_iv_level"
	statCollected         = "collected"
	statSquadFaction      = "squad_faction"
	statOriginsSquad      = "origins_squad"
	statPvpBattles        = "pvp_battles"
	statPvpWins           = "pvp_wins"
	statPvpWinStreak      = "pvp_win_streak"
	statPvpTrophiesWon    = "pvp_trophies_won"
	statPvpTrophiesHeld   = "pvp_trophies_held"
	statGoldEarned        = "gold_earned"
	statCrits             = "crits"
	statBattleDamage      = "battle_damage"
	statChallenges        = "challenge_attempts"
	statBattleDays        = "battle_days"
	statFactionHits       = "faction_hits"
	statRunesUsed         = "runes_used"
	statEventBanners      = "event_banners"
	statEventBuffedSquads = "event_buffed_squads"
	tierIV                = 3
)

var (
	missionSquadFactions = []catalog.Faction{
		catalog.FactionRed, catalog.FactionBlue, catalog.FactionWhite, catalog.FactionBlack,
	}
	originsNames = []string{"Morrigan", "Leliana", "Alistair", "Oghren"}
)

type missionCounters struct {
	added  map[string]int64
	raised map[string]int64
}

func (m *missionCounters) add(stat string, amount int64) {
	if amount <= 0 {
		return
	}
	if m.added == nil {
		m.added = map[string]int64{}
	}
	m.added[stat] = saturatingAdd(m.added[stat], amount)
}

func (m *missionCounters) raise(stat string, value int64) {
	if m.raised == nil {
		m.raised = map[string]int64{}
	}
	m.raised[stat] = max(m.raised[stat], value)
}

func (m *missionCounters) flush(doc document) error {
	counters, err := missionCounterSection(doc)
	if err != nil {
		return err
	}
	for _, stat := range slices.Sorted(maps.Keys(m.added)) {
		value, _ := counters.int64Field(stat)
		counters.setInt(stat, saturatingAdd(max(value, 0), m.added[stat]))
	}
	for _, stat := range slices.Sorted(maps.Keys(m.raised)) {
		value, _ := counters.int64Field(stat)
		counters.setInt(stat, max(value, m.raised[stat]))
	}
	m.added, m.raised = nil, nil
	return nil
}

func missionCounterSection(doc document) (document, error) {
	missions, err := doc.section("missions")
	if err != nil {
		return nil, refuse(BadProfile, "%v", err)
	}
	counters, err := missions.section("counters")
	if err != nil {
		return nil, refuse(BadProfile, "%v", err)
	}
	return counters, nil
}

func (p *player) count(stat string, amount int64) {
	p.counters.add(stat, amount)
}

func (p *player) recordBattleFacts(outcome battle.Outcome) {
	p.count(statCrits, int64(min(max(outcome.Crits, 0), maxCreditedCrits)))
	if outcome.Damage > 0 {
		p.counters.raise(statBattleDamage, min(outcome.Damage, math.MaxInt32))
	}
	if outcome.FactionHit {
		p.count(statFactionHits, 1)
	}
	if day := pvpDay(p.now.Unix()); day != p.server.BattleDay {
		p.server.BattleDay = day
		p.count(statBattleDays, 1)
	}
}

type collection struct {
	rarity catalog.Rarity
	tier   int
}

type missionView struct {
	campaign  *campaignProgress
	maps      []catalog.CampaignMap
	level     int
	heroes    []*hero
	squad     []*hero
	collected map[collection]int64
	pvp       pvpStats
	counters  document
	now       int64
}

func (p *player) missionView() (*missionView, error) {
	progress, err := p.loadCampaign()
	if err != nil {
		return nil, err
	}
	r, err := loadRoster(p.catalog, p.doc)
	if err != nil {
		return nil, err
	}
	view := &missionView{
		campaign: progress, maps: p.catalog.Campaign, level: p.account.Level, heroes: r.heroes,
		collected: map[collection]int64{}, pvp: readPvpStats(p.doc), now: p.now.Unix(),
	}
	seen := map[collection]map[string]bool{}
	for _, h := range r.heroes {
		kind := collection{rarity: h.rarity(), tier: h.tier}
		if seen[kind] == nil {
			seen[kind] = map[string]bool{}
		}
		if !seen[kind][h.def.ID] {
			seen[kind][h.def.ID] = true
			view.collected[kind]++
		}
	}
	for _, instance := range currentSquad(p.doc) {
		if h := r.find(instance); h != nil {
			view.squad = append(view.squad, h)
		}
	}
	missions, _ := p.doc.optionalSection("missions")
	view.counters, _ = missions.optionalSection("counters")
	return view, nil
}

func (v *missionView) highestLevel(keep func(*hero) bool) int64 {
	highest := int64(0)
	for _, h := range v.heroes {
		if keep(h) {
			highest = max(highest, h.level)
		}
	}
	return highest
}

func (v *missionView) squadOfFaction(index int) int64 {
	if index < 0 || index >= len(missionSquadFactions) || len(v.squad) != squadSlotCount {
		return 0
	}
	for _, h := range v.squad {
		if h.def.Factions&missionSquadFactions[index] == 0 {
			return 0
		}
	}
	return 1
}

func (v *missionView) originsMembers() int64 {
	members := int64(0)
	for _, h := range v.squad {
		if originsMember(h.def.Name) {
			members++
		}
	}
	return members
}

func (v *missionView) value(mission catalog.Mission) int64 {
	completions := v.campaign.completions
	switch mission.Stat {
	case statFirstWin:
		return min(completions[0][0], 1)
	case statFirstBoss:
		if boss := v.maps[0].BossNode(); boss >= 0 && completions[0][boss] > 0 {
			return 1
		}
		return 0
	case statNodeMastered:
		return campaignMastered(v.maps, v.campaign, -1)
	case statMapMastered:
		if mission.Map < 0 || mission.Map >= len(v.maps) {
			return 0
		}
		return campaignMastered(v.maps, v.campaign, mission.Map)
	case statMapsUnlocked:
		unlocked := int64(0)
		for index := range v.maps {
			if v.campaign.mapUnlocked(v.maps, index) {
				unlocked = int64(index + 1)
			}
		}
		return unlocked
	case statPlayerLevel:
		return int64(v.level)
	case statHeroLevel:
		return v.highestLevel(func(*hero) bool { return true })
	case statTierIVLevel:
		return v.highestLevel(func(h *hero) bool { return h.tier == tierIV && h.rarity() == mission.Rarity })
	case statCollected:
		return v.collected[collection{rarity: mission.Rarity, tier: mission.HeroTier}]
	case statSquadFaction:
		return v.squadOfFaction(mission.Faction)
	case statOriginsSquad:
		return v.originsMembers()
	case statPvpBattles:
		return int64(v.pvp.Wins + v.pvp.Losses)
	case statPvpWins:
		return int64(v.pvp.Wins)
	case statPvpWinStreak:
		return int64(v.pvp.BestWinStreak)
	case statPvpTrophiesWon:
		return int64(v.pvp.TrophiesWon)
	case statPvpTrophiesHeld:
		return v.pvp.held(v.now)
	}
	counted, _ := numberValue(v.counters[mission.Stat], mission.Stat)
	return max(counted, 0)
}

func campaignMastered(maps []catalog.CampaignMap, progress *campaignProgress, onlyMap int) int64 {
	for index, m := range maps {
		if onlyMap >= 0 && index != onlyMap {
			continue
		}
		mastered, counted := true, false
		for node, def := range m.Nodes {
			if def.Grind || def.WinsToMaster <= 0 {
				continue
			}
			done := progress.completions[index][node] >= int64(def.WinsToMaster)
			if onlyMap < 0 && done {
				return 1
			}
			mastered = mastered && done
			counted = true
		}
		if onlyMap >= 0 && counted && mastered {
			return 1
		}
	}
	return 0
}

func (p *player) syncMissions() error {
	if err := p.counters.flush(p.doc); err != nil {
		return err
	}
	view, err := p.missionView()
	if err != nil {
		return err
	}
	missions, err := p.doc.section("missions")
	if err != nil {
		return refuse(BadProfile, "%v", err)
	}
	claimed, _ := missions.array("claimed")
	if p.server.MissionProgress == nil {
		p.server.MissionProgress = map[string]int64{}
	}
	progress := map[string]any{}
	for _, mission := range p.catalog.Missions {
		reached := max(p.server.MissionProgress[mission.ID], view.value(mission))
		if slices.Contains(claimed, any(mission.ID)) {
			reached = max(reached, mission.Target)
		}
		if reached <= 0 {
			continue
		}
		p.server.MissionProgress[mission.ID] = reached
		progress[mission.ID] = numberArray([]int64{reached})[0]
	}
	missions["progress"] = progress
	return nil
}

func syncMissions(c *catalog.Catalog, doc document, server *profile.ServerState, now time.Time) error {
	a, err := loadAccount(doc)
	if err != nil {
		return err
	}
	p := &player{catalog: c, doc: doc, account: a, server: server, now: now}
	return p.syncMissions()
}

type MissionClaimRequest struct {
	Mission string `json:"mission"`
}

func (p *player) ClaimMission(request MissionClaimRequest) (BattleResult, error) {
	index, ok := p.catalog.MissionIndex(request.Mission)
	if !ok {
		return BattleResult{}, refuse(Unavailable, "no mission %q", request.Mission)
	}
	mission := p.catalog.Missions[index]
	missions, err := p.doc.section("missions")
	if err != nil {
		return BattleResult{}, refuse(BadProfile, "%v", err)
	}
	claimed, err := missions.array("claimed")
	if err != nil {
		return BattleResult{}, refuse(BadProfile, "%v", err)
	}
	if slices.Contains(claimed, any(mission.ID)) {
		return BattleResult{}, refuse(Unavailable, "mission %s is already claimed", mission.ID)
	}
	if err := p.counters.flush(p.doc); err != nil {
		return BattleResult{}, err
	}
	view, err := p.missionView()
	if err != nil {
		return BattleResult{}, err
	}
	if value := max(p.server.MissionProgress[mission.ID], view.value(mission)); value < mission.Target {
		return BattleResult{}, refuse(Unavailable, "mission %s is at %d of %d", mission.ID, value, mission.Target)
	}
	missions["claimed"] = append(claimed, mission.ID)
	result := BattleResult{Gold: int64(max(mission.Gold, 0)), Gems: int64(max(mission.Gems, 0))}
	p.account.Gold = saturatingAdd(p.account.Gold, result.Gold)
	p.addGems(result.Gems)
	return result, nil
}
