package economy

import (
	"github.com/jealous-sound/hoda-custom-backend/internal/catalog"
)

const (
	BattleCampaign = "campaign"
	BattlePvp      = "pvp"
	BattleNexus    = "nexus"
	pvpStaminaCost = 1
)

type BattleRequest struct {
	Mode     string `json:"mode"`
	Won      bool   `json:"won"`
	CritGold int    `json:"crit_gold"`
	Map      int    `json:"map"`
	Node     int    `json:"node"`
	Seed     uint32 `json:"seed"`
	Event    bool   `json:"event"`
	Banners  int    `json:"banners"`
	Step     int    `json:"step"`
	Revive   bool   `json:"revive"`
}

type BattleResult struct {
	Won       bool    `json:"won"`
	Gold      int64   `json:"gold"`
	Gems      int64   `json:"gems"`
	XP        int64   `json:"xp"`
	FirstWin  bool    `json:"first_win,omitempty"`
	Mastered  bool    `json:"mastered,omitempty"`
	Banners   int     `json:"banners,omitempty"`
	Escrowed  bool    `json:"escrowed,omitempty"`
	LevelsUp  int     `json:"levels_up,omitempty"`
	Granted   Granted `json:"granted"`
	NodeWins  int64   `json:"node_wins,omitempty"`
	RunStep   int64   `json:"run_step,omitempty"`
	EventRank int     `json:"event_rank,omitempty"`
}

func (p *player) Battle(request BattleRequest) (BattleResult, error) {
	levelBefore := p.account.Level
	var result BattleResult
	var err error
	switch request.Mode {
	case BattleCampaign:
		result, err = p.campaignBattle(request)
	case BattlePvp:
		result, err = p.pvpBattle(request)
	case BattleNexus:
		result, err = p.nexusBattle(request)
	default:
		return BattleResult{}, refuse(Unavailable, "no battle mode %q", request.Mode)
	}
	if err != nil {
		return BattleResult{}, err
	}
	result.Won = request.Won
	result.LevelsUp = p.account.Level - levelBefore
	result.Granted = p.granted
	return result, nil
}

func (p *player) payBattle(result *BattleResult, gold, xp int64, critGold int, won bool) error {
	goldPct, xpPct := p.runeBonus()
	gold = applyPct(saturatingAdd(gold, creditedCritGold(critGold)), goldPct)
	xp = applyPct(xp, xpPct)
	result.Gold = saturatingAdd(result.Gold, gold)
	result.XP = xp
	if won {
		if err := p.maybeAwardVoucherForWin(); err != nil {
			return err
		}
	}
	p.addGold(gold)
	p.addGems(result.Gems)
	p.account.awardBattleXP(xp)
	return nil
}

type campaignProgress struct {
	section     document
	completions [][]int64
	losses      []int64
}

func (p *player) loadCampaign() (*campaignProgress, error) {
	section, err := p.doc.section("campaign")
	if err != nil {
		return nil, refuse(BadProfile, "%v", err)
	}
	progress := &campaignProgress{section: section}
	rows, _ := section.array("completions")
	for m := range catalog.CampaignMaxMaps {
		row := make([]int64, catalog.CampaignMaxNodes)
		if m < len(rows) {
			if items, ok := rows[m].([]any); ok {
				values, err := intArray(items, "completions")
				if err != nil {
					return nil, refuse(BadProfile, "%v", err)
				}
				copy(row, values)
			}
		}
		progress.completions = append(progress.completions, row)
	}
	progress.losses = make([]int64, catalog.CampaignMaxMaps*catalog.CampaignMaxNodes)
	if items, _ := section.array("node_losses"); items != nil {
		values, err := intArray(items, "node_losses")
		if err != nil {
			return nil, refuse(BadProfile, "%v", err)
		}
		copy(progress.losses, values)
	}
	return progress, nil
}

func (c *campaignProgress) store() {
	rows := make([]any, len(c.completions))
	for i, row := range c.completions {
		rows[i] = numberArray(row)
	}
	c.section["completions"] = rows
	c.section["node_losses"] = numberArray(c.losses)
}

func (c *campaignProgress) frontier(m catalog.CampaignMap, index int) int {
	boss := m.BossNode()
	for node := 0; node <= boss; node++ {
		if c.completions[index][node] <= 0 {
			return node
		}
	}
	if grind := m.GrindNode(); grind >= 0 {
		return grind
	}
	return boss
}

func (c *campaignProgress) mapUnlocked(maps []catalog.CampaignMap, index int) bool {
	if index <= 0 {
		return index == 0
	}
	if index >= len(maps) {
		return false
	}
	for previous := range index {
		boss := maps[previous].BossNode()
		if boss < 0 || c.completions[previous][boss] <= 0 {
			return false
		}
	}
	return true
}

func masteryWin(node catalog.CampaignNode, wins int64) bool {
	return wins > 0 && !node.Grind && node.WinsToMaster > 0 && wins == int64(node.WinsToMaster-1)
}

func (p *player) campaignBattle(request BattleRequest) (BattleResult, error) {
	maps := p.catalog.Campaign
	if request.Map < 0 || request.Map >= len(maps) || request.Node < 0 ||
		request.Node >= len(maps[request.Map].Nodes) {
		return BattleResult{}, refuse(Unavailable, "no campaign node %d/%d", request.Map, request.Node)
	}
	progress, err := p.loadCampaign()
	if err != nil {
		return BattleResult{}, err
	}
	m := maps[request.Map]
	if !progress.mapUnlocked(maps, request.Map) || request.Node > progress.frontier(m, request.Map) {
		return BattleResult{}, refuse(Unavailable, "campaign node %d/%d is locked", request.Map, request.Node)
	}
	node := m.Nodes[request.Node]
	if finished, _ := tutorialFinished(p.doc); !finished && p.account.Energy < int64(node.EnergyCost) {
		p.account.Energy = p.account.EnergyMax
	}
	if err := p.account.spendEnergy(int64(node.EnergyCost)); err != nil {
		return BattleResult{}, err
	}
	lossIndex := request.Map*catalog.CampaignMaxNodes + request.Node
	result := BattleResult{}
	if !request.Won {
		progress.losses[lossIndex] = saturatingAdd(progress.losses[lossIndex], 1)
		progress.store()
		return result, nil
	}
	progress.losses[lossIndex] = 0
	wins := progress.completions[request.Map][request.Node]
	result.FirstWin = wins == 0
	result.Mastered = masteryWin(node, wins)
	progress.completions[request.Map][request.Node] = saturatingAdd(wins, 1)
	result.NodeWins = progress.completions[request.Map][request.Node]
	progress.store()

	gold, xp, gems := int64(node.Gold), int64(node.XP), int64(0)
	switch {
	case node.Grind && result.FirstWin:
		gold, xp, gems = gold+int64(node.ChallengeGold), xp+int64(node.ChallengeXP), int64(node.ChallengeGems)
	case node.Grind:
		gold, xp, gems = gold+int64(node.GrindGold), xp+int64(node.GrindXP), int64(node.GrindGems)
	case result.FirstWin:
		gems = int64(node.FirstWinGems)
	case result.Mastered:
		gems = int64(node.MasteryGems)
	}
	result.Gems = max(gems, 0)
	return result, p.payBattle(&result, max(gold, 0), max(xp, 0), request.CritGold, true)
}
