package economy

import (
	"encoding/json"

	"github.com/jealous-sound/hoda-custom-backend/internal/battle"
	"github.com/jealous-sound/hoda-custom-backend/internal/catalog"
	"github.com/jealous-sound/hoda-custom-backend/internal/profile"
)

type nexusState struct {
	section document
	run     document
	events  document
	nodes   []document
}

func (p *player) loadNexus() (*nexusState, error) {
	section, err := p.doc.section("nexus")
	if err != nil {
		return nil, refuse(BadProfile, "%v", err)
	}
	run, err := section.section("run")
	if err != nil {
		return nil, refuse(BadProfile, "%v", err)
	}
	events, err := section.section("events")
	if err != nil {
		return nil, refuse(BadProfile, "%v", err)
	}
	state := &nexusState{section: section, run: run, events: events}
	items, _ := run.array("nodes")
	for len(items) < catalog.NexusMaxNodes {
		items = append(items, map[string]any{
			"wins": json.Number("0"), "losses": json.Number("0"), "previous_wins": json.Number("0"),
		})
	}
	run["nodes"] = items
	for _, item := range items {
		object, ok := item.(map[string]any)
		if !ok {
			return nil, refuse(BadProfile, "nexus run node is not an object")
		}
		state.nodes = append(state.nodes, document(object))
	}
	return state, nil
}

func (n *nexusState) int(doc document, name string) int64 {
	value, _ := doc.optionalInt(name)
	return int64(value)
}

func (n *nexusState) arrayValues(name string, size int) []int64 {
	items, _ := n.events.array(name)
	values, err := intArray(items, name)
	if err != nil {
		values = nil
	}
	for len(values) < size {
		values = append(values, 0)
	}
	return values
}

func (n *nexusState) escrowUnits() []int64 {
	items, _ := n.run.array("escrow_units")
	values, err := intArray(items, "escrow_units")
	if err != nil {
		values = nil
	}
	for len(values) <= int(catalog.RarityMythic) {
		values = append(values, 0)
	}
	return values
}

func (p *player) activeDifficulty(n *nexusState) (int, *catalog.NexusEvent, *catalog.NexusDifficulty, error) {
	eventIndex := int(n.int(n.run, "event_index"))
	difficultyIndex := int(n.int(n.run, "difficulty_index"))
	if eventIndex < 0 || eventIndex >= len(p.catalog.Nexus) {
		return 0, nil, nil, refuse(Unavailable, "no nexus event %d", eventIndex)
	}
	event := &p.catalog.Nexus[eventIndex]
	if difficultyIndex < 0 || difficultyIndex >= len(event.Difficulties) {
		return 0, nil, nil, refuse(Unavailable, "no nexus difficulty %d", difficultyIndex)
	}
	return eventIndex, event, &event.Difficulties[difficultyIndex], nil
}

func (p *player) nexusStep(n *nexusState, request BattleStartRequest) (int, *catalog.NexusEvent,
	*catalog.NexusDifficulty, error,
) {
	eventIndex, event, difficulty, err := p.activeDifficulty(n)
	if err != nil {
		return 0, nil, nil, err
	}
	if request.Revive && n.int(n.section, "potions") < 1 {
		return 0, nil, nil, refuse(CantAfford, "a revive needs a potion")
	}
	if n.run.boolField("reset") && !request.Revive {
		return 0, nil, nil, refuse(Unavailable, "the nexus run needs a revive")
	}
	step := request.Step
	current := int(n.int(n.run, "current_node_step"))
	last := len(difficulty.Nodes)
	if step < 1 || step > last || step != current || (step == last && n.int(n.nodes[step-1], "wins") > 0) {
		return 0, nil, nil, refuse(Unavailable, "nexus step %d is not the next battle", step)
	}
	remaining := n.arrayValues("remaining_runs", catalog.NexusMaxEvents)
	if !n.run.boolField("committed") && event.MaxRuns > 0 && remaining[eventIndex] <= 0 {
		return 0, nil, nil, refuse(Unavailable, "no nexus runs left for %s", event.ID)
	}
	return eventIndex, event, difficulty, nil
}

func (p *player) startNexus(request BattleStartRequest) (battleStart, error) {
	n, err := p.loadNexus()
	if err != nil {
		return battleStart{}, err
	}
	eventIndex, event, difficulty, err := p.nexusStep(n, request)
	if err != nil {
		return battleStart{}, err
	}
	enemy, changer, err := nexusEnemy(p.catalog, event, difficulty, request.Step)
	if err != nil {
		return battleStart{}, err
	}
	pending := profile.PendingBattle{
		Event: eventIndex, Difficulty: int(n.int(n.run, "difficulty_index")), Step: request.Step,
	}
	return battleStart{pending: pending, enemy: enemy, changer: changer}, nil
}

func (p *player) commitNexusStep(request BattleStartRequest, start *battleStart) error {
	n, err := p.loadNexus()
	if err != nil {
		return err
	}
	eventIndex, event, difficulty, err := p.nexusStep(n, request)
	if err != nil {
		return err
	}
	if request.Revive {
		n.section.setInt("potions", n.int(n.section, "potions")-1)
		n.run["reset"] = false
	}
	if err := p.account.spendEnergy(int64(difficulty.EnergyCost)); err != nil {
		return err
	}
	if !n.run.boolField("committed") {
		n.run["committed"] = true
		remaining := n.arrayValues("remaining_runs", catalog.NexusMaxEvents)
		if event.MaxRuns > 0 && remaining[eventIndex] > 0 {
			remaining[eventIndex]--
			n.events["remaining_runs"] = numberArray(remaining)
		}
	}
	return nil
}

func (p *player) settleNexus(pending profile.PendingBattle, outcome battle.Outcome) (BattleResult, error) {
	n, err := p.loadNexus()
	if err != nil {
		return BattleResult{}, err
	}
	eventIndex, _, difficulty, err := p.activeDifficulty(n)
	current := int(n.int(n.run, "current_node_step"))
	result := BattleResult{Escrowed: true, RunStep: int64(current)}
	if err != nil || eventIndex != pending.Event || int(n.int(n.run, "difficulty_index")) != pending.Difficulty ||
		current != pending.Step || pending.Step < 1 || pending.Step > len(difficulty.Nodes) {
		return result, nil
	}
	progress := n.nodes[pending.Step-1]
	if !outcome.Won {
		progress.setInt("losses", saturatingAdd(n.int(progress, "losses"), 1))
		n.run["reset"] = difficulty.PotionCost > 0
		return result, nil
	}
	node := difficulty.Nodes[pending.Step-1]
	progress.setInt("wins", saturatingAdd(n.int(progress, "wins"), 1))
	result.NodeWins = n.int(progress, "wins")
	goldPct, xpPct := p.runeBonus()
	result.Gold = applyPct(saturatingAdd(int64(max(node.Gold, 0)), creditedCritGold(outcome.Crits)), goldPct)
	result.XP = applyPct(int64(max(node.XP, 0)), xpPct)
	if err := p.maybeAwardVoucherForWin(); err != nil {
		return BattleResult{}, err
	}
	n.run.setInt("escrow_gold", saturatingAdd(n.int(n.run, "escrow_gold"), result.Gold))
	n.run.setInt("escrow_xp", saturatingAdd(n.int(n.run, "escrow_xp"), result.XP))
	n.run.setInt("escrow_potions", saturatingAdd(n.int(n.run, "escrow_potions"), int64(node.PotionReward)))
	if len(node.RewardRarity) > 0 {
		rarity := node.RewardRarity[p.rng.below(len(node.RewardRarity))]
		if rarity >= int(catalog.RarityCommon) && rarity <= int(catalog.RarityMythic) {
			units := n.escrowUnits()
			units[rarity] = saturatingAdd(units[rarity], 1)
			n.run["escrow_units"] = numberArray(units)
		}
	}
	p.account.awardBattleXP(result.XP)
	if err := p.awardSquadXP(result.XP); err != nil {
		return BattleResult{}, err
	}
	if current < len(difficulty.Nodes) {
		current++
		n.run.setInt("current_node_step", int64(current))
	}
	result.RunStep = int64(current)
	return result, nil
}

func (n *nexusState) resetRun() {
	n.run.setInt("current_node_step", runInitialStep)
	n.run["committed"] = false
	n.run["reset"] = false
	for _, field := range []string{"escrow_gold", "escrow_xp", "escrow_potions"} {
		n.run.setInt(field, 0)
	}
	n.run["escrow_units"] = zeros(int(catalog.RarityMythic) + 1)
	for _, node := range n.nodes {
		wins, previous := n.int(node, "wins"), n.int(node, "previous_wins")
		if wins > previous {
			node.setInt("previous_wins", wins)
		}
		node.setInt("wins", 0)
		node.setInt("losses", 0)
	}
}

func (p *player) ResetNexusRun() (BattleResult, error) {
	n, err := p.loadNexus()
	if err != nil {
		return BattleResult{}, err
	}
	n.resetRun()
	return BattleResult{}, nil
}

func (p *player) AwardNexusRun() (BattleResult, error) {
	n, err := p.loadNexus()
	if err != nil {
		return BattleResult{}, err
	}
	if !n.run.boolField("committed") {
		return BattleResult{}, refuse(Unavailable, "no nexus run to award")
	}
	result := BattleResult{Gold: n.int(n.run, "escrow_gold")}
	p.addGold(result.Gold)
	if err := p.addPotions(n.int(n.run, "escrow_potions")); err != nil {
		return BattleResult{}, err
	}
	var heroes []string
	for rarity, count := range n.escrowUnits() {
		pool := p.catalog.RarityPool(catalog.Rarity(rarity))
		for range count {
			if len(pool) == 0 || len(heroes) >= catalog.NexusMaxNodes {
				break
			}
			heroes = append(heroes, p.catalog.Heroes[pool[p.rng.below(len(pool))]].ID)
		}
	}
	if len(heroes) > p.rosterRoom() {
		heroes = heroes[:p.rosterRoom()]
	}
	if err := p.grantHeroes(heroes); err != nil {
		return BattleResult{}, err
	}
	eventIndex := int(n.int(n.run, "event_index"))
	difficultyIndex := int(n.int(n.run, "difficulty_index"))
	if eventIndex >= 0 && eventIndex < catalog.NexusMaxEvents && difficultyIndex >= 0 &&
		difficultyIndex < catalog.NexusMaxDifficulties {
		rows, _ := n.events.array("completions")
		for len(rows) < catalog.NexusMaxEvents {
			rows = append(rows, zeros(catalog.NexusMaxDifficulties))
		}
		row, _ := rows[eventIndex].([]any)
		values, err := intArray(row, "completions")
		if err != nil {
			values = nil
		}
		for len(values) < catalog.NexusMaxDifficulties {
			values = append(values, 0)
		}
		values[difficultyIndex] = saturatingAdd(values[difficultyIndex], 1)
		rows[eventIndex] = numberArray(values)
		n.events["completions"] = rows
	}
	n.resetRun()
	result.Granted = p.granted
	return result, nil
}

type NexusUnlockRequest struct {
	Event int `json:"event"`
}

func (p *player) UnlockNexusEvent(request NexusUnlockRequest) (BattleResult, error) {
	if request.Event < 0 || request.Event >= len(p.catalog.Nexus) {
		return BattleResult{}, refuse(Unavailable, "no nexus event %d", request.Event)
	}
	event := p.catalog.Nexus[request.Event]
	n, err := p.loadNexus()
	if err != nil {
		return BattleResult{}, err
	}
	if event.UnlockKey != 0 {
		key := event.UnlockKey - catalog.NexusKeyFirstID
		items, _ := n.section.array("keys")
		keys, err := intArray(items, "keys")
		if err != nil || key < 0 || key >= len(keys) || keys[key] < int64(event.UnlockKeyQty) {
			return BattleResult{}, refuse(CantAfford, "%s needs %d keys", event.ID, event.UnlockKeyQty)
		}
		keys[key] -= int64(event.UnlockKeyQty)
		n.section["keys"] = numberArray(keys)
	}
	unlocked := n.arrayValues("unlocked_at", catalog.NexusMaxEvents)
	unlocked[request.Event] = p.now.Unix()
	n.events["unlocked_at"] = numberArray(unlocked)
	remaining := n.arrayValues("remaining_runs", catalog.NexusMaxEvents)
	remaining[request.Event] = int64(event.MaxRuns)
	n.events["remaining_runs"] = numberArray(remaining)
	return BattleResult{}, nil
}
