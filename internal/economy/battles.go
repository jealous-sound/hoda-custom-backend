package economy

import (
	"github.com/jealous-sound/hoda-custom-backend/internal/battle"
	"github.com/jealous-sound/hoda-custom-backend/internal/catalog"
	"github.com/jealous-sound/hoda-custom-backend/internal/profile"
)

const (
	BattleCampaign = "campaign"
	BattlePvp      = "pvp"
	BattleNexus    = "nexus"
	pvpStaminaCost = 1
)

type BattleStartRequest struct {
	Mode            string             `json:"mode"`
	Map             int                `json:"map"`
	Node            int                `json:"node"`
	Step            int                `json:"step"`
	Revive          bool               `json:"revive"`
	Event           bool               `json:"event"`
	Duel            string             `json:"duel"`
	Opponent        PvpOpponentRequest `json:"opponent"`
	Squad           battle.Side        `json:"squad"`
	RuneIDs         []int64            `json:"rune_ids"`
	Enemy           battle.Side        `json:"enemy"`
	Changer         *battle.Changer    `json:"changer"`
	TutorialOutcome string             `json:"tutorial_outcome"`
}

type BattleRequest struct {
	BattleID int64 `json:"battle_id"`
	Won      bool  `json:"won"`
	CritGold int   `json:"crit_gold"`
}

type BattleResult struct {
	Won       bool          `json:"won"`
	Gold      int64         `json:"gold"`
	Gems      int64         `json:"gems"`
	XP        int64         `json:"xp"`
	FirstWin  bool          `json:"first_win,omitempty"`
	Mastered  bool          `json:"mastered,omitempty"`
	Banners   int           `json:"banners,omitempty"`
	Escrowed  bool          `json:"escrowed,omitempty"`
	LevelsUp  int           `json:"levels_up,omitempty"`
	Granted   Granted       `json:"granted"`
	NodeWins  int64         `json:"node_wins,omitempty"`
	RunStep   int64         `json:"run_step,omitempty"`
	EventRank int           `json:"event_rank,omitempty"`
	Cycle     int64         `json:"event_cycle,omitempty"`
	BattleID  int64         `json:"battle_id,omitempty"`
	Seed      uint32        `json:"seed,omitempty"`
	Crits     int           `json:"crits,omitempty"`
	Settled   *BattleResult `json:"settled,omitempty"`
}

type battleStart struct {
	pending profile.PendingBattle
	enemy   battle.Side
	changer *battle.Changer
}

func (p *player) StartBattle(request BattleStartRequest) (BattleResult, error) {
	var result BattleResult
	if p.server.Battle != nil {
		settled, err := p.settlePending()
		if err != nil {
			return BattleResult{}, err
		}
		result.Settled = &settled
	}
	squad, err := p.battleSquad(request.Squad, request.RuneIDs)
	if err != nil {
		return BattleResult{}, err
	}
	var start battleStart
	switch request.Mode {
	case BattleCampaign:
		start, err = p.startCampaign(request)
	case BattlePvp:
		start, err = p.startPvp(request)
	case BattleNexus:
		start, err = p.startNexus(request)
	default:
		return BattleResult{}, refuse(Unavailable, "no battle mode %q", request.Mode)
	}
	if err != nil {
		return BattleResult{}, err
	}
	if err := p.checkEnemy(request, start.enemy, start.changer); err != nil {
		return BattleResult{}, err
	}
	replay := battle.Replay{Player: squad, Enemy: start.enemy, Changer: canonicalChanger(start.changer)}
	if _, err := p.catalog.Battle.Run(replay); err != nil {
		return BattleResult{}, refuse(Unavailable, "%v", err)
	}
	if replay.Seed, err = p.battleSeed(request, replay); err != nil {
		return BattleResult{}, err
	}
	if err := p.spendForBattle(request, &start); err != nil {
		return BattleResult{}, err
	}
	p.server.Battles++
	start.pending.ID = p.server.Battles
	start.pending.Mode = request.Mode
	start.pending.StartedAt = p.now.Unix()
	start.pending.Replay = replay
	p.server.Battle = &start.pending
	result.BattleID = start.pending.ID
	result.Seed = replay.Seed
	result.Granted = p.granted
	return result, nil
}

func (p *player) spendForBattle(request BattleStartRequest, start *battleStart) error {
	switch request.Mode {
	case BattleCampaign:
		node := p.catalog.Campaign[request.Map].Nodes[request.Node]
		if finished, _ := tutorialFinished(p.doc); !finished && p.account.Energy < int64(node.EnergyCost) {
			p.account.Energy = p.account.EnergyMax
		}
		return p.account.spendEnergy(int64(node.EnergyCost))
	case BattlePvp:
		return p.account.spendStamina(pvpStaminaCost)
	}
	return p.commitNexusStep(request, start)
}

func (p *player) FinishBattle(request BattleRequest) (BattleResult, error) {
	if request.BattleID == 0 {
		return BattleResult{}, refuse(Unavailable, "battles start with battle_start; update the client")
	}
	if p.server.Battle == nil || p.server.Battle.ID != request.BattleID {
		return BattleResult{}, refuse(Unavailable, "battle %d is not in progress", request.BattleID)
	}
	result, err := p.settlePending()
	if err != nil {
		return BattleResult{}, err
	}
	if request.Won != result.Won || request.CritGold != battle.CritGoldFor(result.Crits) {
		return result, &Refusal{
			Code:      OutcomeMismatch,
			Message:   "the replayed battle has a different outcome; the server's result was applied",
			Committed: true,
		}
	}
	return result, nil
}

func (p *player) settlePending() (BattleResult, error) {
	pending := *p.server.Battle
	p.server.Battle = nil
	levelBefore := p.account.Level
	outcome, err := p.catalog.Battle.Run(pending.Replay)
	if err != nil {
		outcome = battle.Outcome{}
	}
	var result BattleResult
	switch pending.Mode {
	case BattleCampaign:
		result, err = p.settleCampaign(pending, outcome)
	case BattlePvp:
		result, err = p.settlePvp(pending, outcome)
	case BattleNexus:
		result, err = p.settleNexus(pending, outcome)
	}
	if err != nil {
		return BattleResult{}, err
	}
	result.Won = outcome.Won
	result.Crits = outcome.Crits
	result.BattleID = pending.ID
	result.LevelsUp = p.account.Level - levelBefore
	result.Granted = p.granted
	return result, nil
}

func (p *player) payBattle(result *BattleResult, gold, xp int64, crits int, won bool) error {
	goldPct, xpPct := p.runeBonus()
	gold = applyPct(saturatingAdd(gold, creditedCritGold(crits)), goldPct)
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

func (p *player) startCampaign(request BattleStartRequest) (battleStart, error) {
	maps := p.catalog.Campaign
	if request.Map < 0 || request.Map >= len(maps) || request.Node < 0 ||
		request.Node >= len(maps[request.Map].Nodes) {
		return battleStart{}, refuse(Unavailable, "no campaign node %d/%d", request.Map, request.Node)
	}
	progress, err := p.loadCampaign()
	if err != nil {
		return battleStart{}, err
	}
	m := maps[request.Map]
	if !progress.mapUnlocked(maps, request.Map) || request.Node > progress.frontier(m, request.Map) {
		return battleStart{}, refuse(Unavailable, "campaign node %d/%d is locked", request.Map, request.Node)
	}
	enemy, err := campaignEnemy(p.catalog, m, request.Node)
	if err != nil {
		return battleStart{}, err
	}
	return battleStart{pending: profile.PendingBattle{Map: request.Map, Node: request.Node}, enemy: enemy}, nil
}

func (p *player) settleCampaign(pending profile.PendingBattle, outcome battle.Outcome) (BattleResult, error) {
	maps := p.catalog.Campaign
	if pending.Map < 0 || pending.Map >= len(maps) || pending.Node < 0 || pending.Node >= len(maps[pending.Map].Nodes) {
		return BattleResult{}, nil
	}
	progress, err := p.loadCampaign()
	if err != nil {
		return BattleResult{}, err
	}
	node := maps[pending.Map].Nodes[pending.Node]
	lossIndex := pending.Map*catalog.CampaignMaxNodes + pending.Node
	result := BattleResult{}
	if !outcome.Won {
		progress.losses[lossIndex] = saturatingAdd(progress.losses[lossIndex], 1)
		progress.store()
		return result, nil
	}
	progress.losses[lossIndex] = 0
	wins := progress.completions[pending.Map][pending.Node]
	result.FirstWin = wins == 0
	result.Mastered = masteryWin(node, wins)
	progress.completions[pending.Map][pending.Node] = saturatingAdd(wins, 1)
	result.NodeWins = progress.completions[pending.Map][pending.Node]
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
	return result, p.payBattle(&result, max(gold, 0), max(xp, 0), outcome.Crits, true)
}
