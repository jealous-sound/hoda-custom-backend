package battle

import (
	"errors"
	"math"
)

const (
	SlotCount                  = 5
	GiantSlot                  = 4
	unitCount                  = 2 * SlotCount
	maxTargets                 = unitCount
	maxActions                 = 4096
	initClassSpan              = 40
	runeTargetWeightPerLevel   = float32(0.5)
	runeShieldedHitMinDamage   = 1
	gearTargetWeight           = float32(2.0)
	gearStrongTargetWeight     = float32(3.0)
	gearFlankWeight            = float32(3.0)
	gearFlankReduction         = float32(0.5)
	gearDeathPasses            = 4
	gearActionReserve          = 64
	defenderAbsorbPct          = 25
	roundCap                   = 10
	defaultSeed                = uint32(0x9E3779B9)
	tauntWeight                = float32(2.0)
	elusiveWeight              = float32(0.5)
	markedWeight               = float32(4.0)
	backRowCritMultiplier      = float32(1.5)
	factionAdvantageDamageRate = float32(0.25)
	critRollScale              = float32(10.0)
	critRollSides              = 1000
	percentSides               = 100
	targetRollResolution       = 10000
	chanceGroupCount           = 16
)

const (
	initClassSlow = iota
	initClassMedium
	initClassFast
)

const (
	ailCursePower uint32 = 1 << iota
	ailStunned
	ailCurseHealth
	ailUncursePower
	ailUnstunned
	ailUncurseHealth
)

var ErrActionOverflow = errors.New("battle timeline exceeds the action cap")

var slotReferencePosition = [SlotCount]int{2, 0, 3, 1, -1}

func slotReferenceX(slot int) int {
	if position := slotReferencePosition[slot]; position >= 0 {
		return position / 2
	}
	return -1
}

func slotReferenceY(slot int) int {
	if position := slotReferencePosition[slot]; position >= 0 {
		return position % 2
	}
	return -1
}

func slotIsFront(slot int) bool { return slotReferenceX(slot) == 0 }
func slotIsBack(slot int) bool  { return slotReferenceX(slot) == 1 }

func mul(a, b float32) float32 { return float32(a * b) }

func lround(value float32) int { return int(math.Round(float64(value))) }

func roundf(value float32) float32 { return float32(math.Round(float64(value))) }

func percent(value int) float32 { return float32(value) / 100.0 }

type Target struct {
	Unit               int
	HealthDelta        int
	PowerDelta         int
	InitiativeDelta    int
	HealthFactionDelta int
	CritDelta          float32
	DamageBonusDelta   float32
}

type Action struct {
	Actor        int
	NewRound     bool
	Crit         bool
	ReplayAttack bool
	SameSwing    bool
	Ailments     uint32
	Targets      []Target
}

type gearState struct {
	damageResistPct, rawDamageResist             int
	stunResistPct, slowResistPct, drainResistPct int
	cursePowerResistPct, curseHealthResistPct    int
	defenderPct                                  int
	stealth, perceptive, haste, flankReduction   bool
	targetHighestPower, targetLowestHealth       bool
	targetBackRow, targetQuick, targetSlowest    bool
}

type unit struct {
	hero                     *hero
	recipe                   *abilityRecipe
	gear                     gearState
	present, alive           bool
	side, slot               int
	maxHealth, health        int
	power, startPower        int
	critPct, damageBonusPct  float32
	initClass, curInitiative int
	stunned                  bool
	cursePower, curseHealth  bool
	cursePowerTurns          int
	curseHealthTurns         int
	marked                   int
	faction                  uint32
	gameChangerApplied       bool
}

func (u *unit) giant() bool { return u.hero.def.giant }

type sim struct {
	units             [unitCount]unit
	actions           []Action
	rounds            int
	playerWon         bool
	formationFaction  [2]uint32
	formationBonusPct [2]float32
	runeBonus         [2]runeBonus
	rng               uint32
	gearPendingDeaths uint32
	gearLastDamage    int
	overflow          bool
	budget            *statBudget
}

func (s *sim) rand() uint32 {
	x := s.rng
	x ^= x << 13
	x ^= x >> 17
	x ^= x << 5
	s.rng = x
	return x
}

func (s *sim) rangeRoll(lo, hi int) int {
	return lo + int(s.rand()%uint32(hi-lo+1))
}

func (s *sim) chance(pct int) bool {
	return pct > 0 && s.rangeRoll(1, percentSides) <= pct
}

func hasAdvantage(attacker, target uint32) bool {
	ror4 := ((target >> 1) | (target << 3)) & 15
	return attacker&ror4 != 0
}

func formationRarityBonus(r rarity) float32 {
	switch r {
	case rarityCommon:
		return 0.01
	case rarityUncommon:
		return 0.02
	case rarityRare:
		return 0.03
	case rarityEpic:
		return 0.04
	case rarityLegendary:
		return 0.05
	}
	return 0.06
}

func (s *sim) unitAt(side, slot int) *unit { return &s.units[side*SlotCount+slot] }

func (s *sim) initUnit(side, slot int, h *hero) {
	u := s.unitAt(side, slot)
	*u = unit{}
	if h == nil {
		return
	}
	u.hero = h
	u.present, u.alive = true, true
	u.side, u.slot = side, slot
	u.power = h.statPower()
	u.maxHealth = h.statHealth()
	u.recipe = h.recipe
	if s.budget != nil {
		u.power, u.maxHealth = s.budget.apply(h, u.power, u.maxHealth)
		u.recipe = h.recipe.withPotencyCut(s.budget.potencyCutPct(h))
	}
	u.startPower = u.power
	u.critPct = float32(max(h.critChance, baseCritChance))
	switch h.def.speed {
	case speedQuick:
		u.initClass = initClassFast
	case speedSlow:
		u.initClass = initClassSlow
	default:
		u.initClass = initClassMedium
	}
	if !h.def.giant {
		u.faction = factionMask(h.def.faction)
	}
	if !h.def.giant && slotIsFront(slot) {
		u.maxHealth = (u.maxHealth*3 + 1) / 2
	}
	if !h.def.giant && slotIsBack(slot) {
		u.critPct = mul(u.critPct, backRowCritMultiplier)
	}
	u.health = u.maxHealth
}

func (s *sim) formationBonus(side int) {
	mask := maskAll
	bonus := float32(0)
	count := 0
	for slot := range SlotCount {
		u := s.unitAt(side, slot)
		if !u.present || u.giant() {
			continue
		}
		mask &= u.faction
		if mask == 0 {
			return
		}
		if s.budget != nil {
			bonus += s.budget.formationBonus()
		} else {
			bonus += formationRarityBonus(u.hero.rarity)
		}
		count++
	}
	if count < 4 || mask == 0 {
		return
	}
	s.formationFaction[side] = mask & (^mask + 1)
	s.formationBonusPct[side] = roundf(mul(bonus, 1000)) / 10
	for slot := range SlotCount {
		u := s.unitAt(side, slot)
		if !u.present || u.giant() {
			continue
		}
		u.power = lround(mul(float32(u.power), 1+bonus))
		u.startPower = u.power
	}
}

func filterMatchCount(f *filter, source, candidate *unit) int {
	if !source.present || !candidate.present {
		return 0
	}
	switch f.scope {
	case scopeSelf:
		if source != candidate {
			return 0
		}
	case scopeAllies:
		if source.side != candidate.side || (!f.includeSource && source == candidate) {
			return 0
		}
	default:
		if source.side == candidate.side {
			return 0
		}
	}
	return versusCount(f, candidate)
}

func versusCount(f *filter, candidate *unit) int {
	if !f.hasMatch {
		return 1
	}
	matches := 0
	for i := range f.terms {
		if termMatches(&f.terms[i], candidate.hero) {
			matches++
		}
	}
	if f.each {
		return matches
	}
	if matches > 0 {
		return 1
	}
	return 0
}

func wearerQualifies(effect *gearEffect, wearer *unit) bool {
	return filterMatchCount(&effect.wearer, wearer, wearer) > 0
}

func applyGearStat(g *gearState, stat gearStat, value int) {
	switch stat {
	case gearStatDamageResist:
		g.damageResistPct += value
	case gearStatRawDamageResist:
		g.rawDamageResist += value
	case gearStatStunResist:
		g.stunResistPct += value
	case gearStatSlowResist:
		g.slowResistPct += value
	case gearStatDrainResist:
		g.drainResistPct += value
	case gearStatCursePowerResist:
		g.cursePowerResistPct += value
	case gearStatCurseHealthResist:
		g.curseHealthResistPct += value
	case gearStatDefender:
		g.defenderPct = max(value, g.defenderPct)
	case gearStatStealth:
		g.stealth = true
	case gearStatPerceptive:
		g.perceptive = true
	case gearStatHaste:
		g.haste = true
	case gearStatFlankReduction:
		g.flankReduction = true
	case gearStatTargetHighestPower:
		g.targetHighestPower = true
	case gearStatTargetLowestHealth:
		g.targetLowestHealth = true
	case gearStatTargetBackRow:
		g.targetBackRow = true
	case gearStatTargetQuick:
		g.targetQuick = true
	case gearStatTargetSlowest:
		g.targetSlowest = true
	}
}

func (s *sim) gearAuras(healthPct, powerPct *[unitCount]float32) {
	for wearerIdx := range unitCount {
		wearer := &s.units[wearerIdx]
		if !wearer.present {
			continue
		}
		for _, def := range wearer.hero.gear {
			if def == nil {
				continue
			}
			for e := range def.effects {
				effect := &def.effects[e]
				if effect.on != onBattle || effect.versus.hasMatch || effect.stat == gearStatTargetMatch ||
					!wearerQualifies(effect, wearer) {
					continue
				}
				for recipientIdx := range unitCount {
					recipient := &s.units[recipientIdx]
					multiplier := filterMatchCount(&effect.recipient, wearer, recipient)
					if multiplier <= 0 {
						continue
					}
					value := effect.value * multiplier
					switch effect.stat {
					case gearStatPower:
						powerPct[recipientIdx] += float32(value)
					case gearStatHealth:
						healthPct[recipientIdx] += float32(value)
					case gearStatCrit:
						recipient.critPct += float32(value)
					case gearStatBonusDamage:
						recipient.damageBonusPct += float32(value)
					default:
						applyGearStat(&recipient.gear, effect.stat, value)
					}
				}
			}
		}
	}
}

func (s *sim) gearVersusStat(recipient, other *unit, stat gearStat) int {
	total := 0
	for wearerIdx := range unitCount {
		wearer := &s.units[wearerIdx]
		if !wearer.present {
			continue
		}
		for _, def := range wearer.hero.gear {
			if def == nil {
				continue
			}
			for e := range def.effects {
				effect := &def.effects[e]
				if effect.stat != stat || effect.on != onBattle || !effect.versus.hasMatch ||
					!wearerQualifies(effect, wearer) {
					continue
				}
				total += effect.value * filterMatchCount(&effect.recipient, wearer, recipient) *
					versusCount(&effect.versus, other)
			}
		}
	}
	return total
}

func gearPrefersMatchedTarget(attacker, target *unit) bool {
	for _, def := range attacker.hero.gear {
		if def == nil {
			continue
		}
		for e := range def.effects {
			effect := &def.effects[e]
			if effect.stat == gearStatTargetMatch && wearerQualifies(effect, attacker) &&
				versusCount(&effect.recipient, target) > 0 {
				return true
			}
		}
	}
	return false
}

func (s *sim) battleStartPassives() {
	var gearHealthPct, gearPowerPct, healthPct, powerPct [unitCount]float32
	s.gearAuras(&gearHealthPct, &gearPowerPct)
	if s.budget == nil {
		healthPct, powerPct = gearHealthPct, gearPowerPct
	}
	for sourceIdx := range unitCount {
		source := &s.units[sourceIdx]
		if !source.present {
			continue
		}
		for e := range source.hero.passives {
			effect := &source.hero.passives[e]
			counted := effect.kind == passiveStatPercentPerMatchingAlly
			if (!counted && effect.kind != passiveStatPercent) || effect.trigger != triggerBattleStart {
				continue
			}
			composition := 1
			if counted {
				composition = 0
				for candidateIdx := range unitCount {
					composition += filterMatchCount(&effect.count, source, &s.units[candidateIdx])
				}
				if composition <= 0 {
					continue
				}
			}
			for recipientIdx := range unitCount {
				multiplier := filterMatchCount(&effect.recipient, source, &s.units[recipientIdx])
				if multiplier <= 0 {
					continue
				}
				value := mul(mul(effect.valuePct, float32(multiplier)), float32(composition))
				if effect.stats&statHealth != 0 {
					healthPct[recipientIdx] += value
				}
				if effect.stats&statPower != 0 {
					powerPct[recipientIdx] += value
				}
			}
		}
	}
	for i := range unitCount {
		u := &s.units[i]
		if !u.present {
			continue
		}
		if s.budget != nil {
			healthPct[i] = s.budget.cappedAuraPct(healthPct[i]) + s.budget.cappedGearPct(gearHealthPct[i])
			powerPct[i] = s.budget.cappedAuraPct(powerPct[i]) + s.budget.cappedGearPct(gearPowerPct[i])
		}
		if healthPct[i] != 0 {
			u.maxHealth = max(lround(mul(float32(u.maxHealth), 1+healthPct[i]/100)), 1)
		}
		if powerPct[i] != 0 {
			u.power = max(lround(mul(float32(u.power), 1+powerPct[i]/100)), 0)
		}
		u.health = u.maxHealth
		u.startPower = u.power
	}
}

func (s *sim) incomingTargetMultiplier(target *unit) (float32, bool) {
	multiplier := float32(1)
	found := false
	for sourceIdx := range unitCount {
		source := &s.units[sourceIdx]
		if !source.present {
			continue
		}
		for e := range source.hero.passives {
			effect := &source.hero.passives[e]
			if effect.kind != passiveIncomingTargetWeight || effect.valueMultiplier <= 0 {
				continue
			}
			count := filterMatchCount(&effect.recipient, source, target)
			if count <= 0 {
				continue
			}
			found = true
			power := float32(math.Pow(float64(effect.valueMultiplier), float64(float32(count))))
			multiplier = mul(multiplier, power)
		}
	}
	return multiplier, found
}

func (s *sim) passiveLowestHealthTarget(source *unit, effect *passiveEffect, target *unit) bool {
	if filterMatchCount(&effect.recipient, source, target) <= 0 || target.maxHealth <= 0 {
		return false
	}
	for i := range unitCount {
		other := &s.units[i]
		if !other.alive || other.maxHealth <= 0 || filterMatchCount(&effect.recipient, source, other) <= 0 {
			continue
		}
		if int64(other.health)*int64(target.maxHealth) < int64(target.health)*int64(other.maxHealth) {
			return false
		}
	}
	return true
}

func (s *sim) passiveOutgoingTargetMultiplier(attacker, target *unit) float32 {
	multiplier := float32(1)
	for e := range attacker.hero.passives {
		effect := &attacker.hero.passives[e]
		if effect.kind != passiveOutgoingTargetPreference || effect.valueMultiplier <= 0 {
			continue
		}
		if effect.lowestHealth && s.passiveLowestHealthTarget(attacker, effect, target) {
			multiplier = mul(multiplier, effect.valueMultiplier)
		}
	}
	return multiplier
}

func (s *sim) passiveBonusDamagePct(attacker, target *unit) float32 {
	bonus := float32(0)
	for sourceIdx := range unitCount {
		source := &s.units[sourceIdx]
		if !source.present {
			continue
		}
		for e := range source.hero.passives {
			effect := &source.hero.passives[e]
			if effect.kind != passiveBonusDamagePercent || effect.trigger != triggerBeforeDamage {
				continue
			}
			recipients := filterMatchCount(&effect.recipient, source, attacker)
			if recipients <= 0 {
				continue
			}
			targets := 1
			if effect.hasTarget {
				targets = filterMatchCount(&effect.target, source, target)
			}
			if targets > 0 {
				bonus += mul(mul(effect.valuePct, float32(recipients)), float32(targets))
			}
		}
	}
	return bonus
}

func runeTargetBeats(preference runeTarget, a, b *unit) bool {
	switch preference {
	case runeTargetHighestPower:
		return a.power > b.power
	case runeTargetHighestSpeed:
		return a.initClass > b.initClass
	case runeTargetLowestSpeed:
		return a.initClass < b.initClass
	case runeTargetLowestHealth:
		return int64(a.health)*int64(b.maxHealth) < int64(b.health)*int64(a.maxHealth)
	}
	return false
}

func (s *sim) runeTargetPreferred(preference runeTarget, t *unit) bool {
	for slot := range SlotCount {
		other := s.unitAt(t.side, slot)
		if other.alive && runeTargetBeats(preference, other, t) {
			return false
		}
	}
	return true
}

func (s *sim) gearTargetMultiplier(attacker, t *unit) float32 {
	g := &attacker.gear
	multiplier := float32(1)
	if g.targetHighestPower && s.runeTargetPreferred(runeTargetHighestPower, t) {
		multiplier = mul(multiplier, gearTargetWeight)
	}
	if g.targetLowestHealth && s.runeTargetPreferred(runeTargetLowestHealth, t) {
		multiplier = mul(multiplier, gearTargetWeight)
	}
	if g.targetSlowest && s.runeTargetPreferred(runeTargetLowestSpeed, t) {
		multiplier = mul(multiplier, gearTargetWeight)
	}
	if g.targetQuick && t.hero.def.speed == speedQuick {
		multiplier = mul(multiplier, gearStrongTargetWeight)
	}
	if gearPrefersMatchedTarget(attacker, t) {
		multiplier = mul(multiplier, gearTargetWeight)
	}
	return multiplier
}

func flankWeight(attacker, t *unit) float32 {
	flanks := attacker.hero.def.hasSpecialty(specFlank) || attacker.gear.targetBackRow
	if !flanks || t.giant() || !slotIsBack(t.slot) {
		return 1
	}
	if t.gear.flankReduction {
		return mul(gearFlankWeight, gearFlankReduction)
	}
	return gearFlankWeight
}

func (s *sim) runeTargetMultiplier(attacker, t *unit) float32 {
	bonus := &s.runeBonus[attacker.side]
	multiplier := float32(1)
	for preference := range runeTargetCount {
		level := bonus.targetLevel[preference]
		if level > 0 && s.runeTargetPreferred(preference, t) {
			multiplier = mul(multiplier, (1 + mul(float32(level), runeTargetWeightPerLevel)))
		}
	}
	return multiplier
}

func (s *sim) targetWeight(attacker, t *unit) float32 {
	w := float32(1)
	if incoming, explicit := s.incomingTargetMultiplier(t); explicit {
		w = mul(w, incoming)
	} else {
		if t.hero.def.hasSpecialty(specTaunt) {
			w = mul(w, tauntWeight)
		}
		if t.hero.def.hasSpecialty(specElusive) {
			w = mul(w, elusiveWeight)
		}
	}
	w = mul(w, flankWeight(attacker, t))
	if t.marked > 0 {
		w = mul(w, markedWeight)
	}
	w = mul(w, s.passiveOutgoingTargetMultiplier(attacker, t))
	w = mul(w, s.runeTargetMultiplier(attacker, t))
	return mul(w, s.gearTargetMultiplier(attacker, t))
}

func (s *sim) pickWeighted(attacker *unit, enemySide int, includeGiant bool) int {
	var weights [SlotCount]float32
	total := float32(0)
	for slot := range SlotCount {
		t := s.unitAt(enemySide, slot)
		if !t.alive || (slot == GiantSlot && !includeGiant) {
			continue
		}
		weights[slot] = s.targetWeight(attacker, t)
		total += weights[slot]
	}
	if total <= 0 {
		return -1
	}
	roll := mul(float32(s.rand()%targetRollResolution)/targetRollResolution, total)
	for slot := range SlotCount {
		roll -= weights[slot]
		if weights[slot] > 0 && roll <= 0 {
			return enemySide*SlotCount + slot
		}
	}
	for slot := SlotCount - 1; slot >= 0; slot-- {
		if weights[slot] > 0 {
			return enemySide*SlotCount + slot
		}
	}
	return -1
}

func (s *sim) selectTargets(attacker *unit, shape targetPattern, out []int) []int {
	enemySide := 1 - attacker.side
	if shape == patternAll {
		for slot := range SlotCount {
			if s.unitAt(enemySide, slot).alive {
				out = append(out, enemySide*SlotCount+slot)
			}
		}
		return out
	}
	if shape == patternOne {
		if pick := s.pickWeighted(attacker, enemySide, true); pick >= 0 {
			out = append(out, pick)
		}
		return out
	}
	pick := s.pickWeighted(attacker, enemySide, false)
	if pick < 0 {
		if pick = s.pickWeighted(attacker, enemySide, true); pick >= 0 {
			out = append(out, pick)
		}
		return out
	}
	coordinate := func(slot int) int {
		if shape == patternRow {
			return slotReferenceX(slot)
		}
		return slotReferenceY(slot)
	}
	wanted := coordinate(pick % SlotCount)
	for candidate := range GiantSlot {
		if coordinate(candidate) == wanted && s.unitAt(enemySide, candidate).alive {
			out = append(out, enemySide*SlotCount+candidate)
		}
	}
	return out
}

func (s *sim) emit(actor int) *Action {
	if len(s.actions) >= maxActions {
		s.overflow = true
		return nil
	}
	s.actions = append(s.actions, Action{Actor: actor})
	return &s.actions[len(s.actions)-1]
}

func (s *sim) applyTarget(a *Action, t Target) {
	if a == nil || len(a.Targets) >= maxTargets {
		return
	}
	u := &s.units[t.Unit]
	applied := t
	if applied.PowerDelta < -u.power {
		applied.PowerDelta = -u.power
	}
	a.Targets = append(a.Targets, applied)
	wasAlive := u.alive
	u.health += applied.HealthDelta + applied.HealthFactionDelta
	u.health = min(u.health, u.maxHealth)
	u.power += applied.PowerDelta
	u.curInitiative += applied.InitiativeDelta
	u.critPct += applied.CritDelta
	u.damageBonusPct += applied.DamageBonusDelta
	if u.health <= 0 {
		u.health = 0
		u.alive = false
		u.stunned = false
		u.cursePower, u.curseHealth = false, false
		u.cursePowerTurns, u.curseHealthTurns = 0, 0
		if wasAlive {
			s.gearPendingDeaths |= 1 << t.Unit
			s.applyEnemyDeathPassives(a, t.Unit)
		}
	}
}

func (s *sim) recordPassiveHeal(a *Action, unitIdx, requested int) {
	u := &s.units[unitIdx]
	if !u.alive || u.curseHealth || requested <= 0 || u.health >= u.maxHealth {
		return
	}
	applied := min(requested, u.maxHealth-u.health)
	for i := range a.Targets {
		if a.Targets[i].Unit == unitIdx {
			a.Targets[i].HealthDelta += applied
			u.health += applied
			return
		}
	}
	if len(a.Targets) >= maxTargets {
		return
	}
	a.Targets = append(a.Targets, Target{Unit: unitIdx, HealthDelta: applied})
	u.health += applied
}

func (s *sim) applyEnemyDeathPassives(a *Action, deadIdx int) {
	dead := &s.units[deadIdx]
	for sourceIdx := range unitCount {
		source := &s.units[sourceIdx]
		if !source.alive || source.side == dead.side {
			continue
		}
		for e := range source.hero.passives {
			effect := &source.hero.passives[e]
			if effect.kind != passiveHealPercentMaxHealth || effect.trigger != triggerEnemyDeath {
				continue
			}
			targetMultiplier := 1
			if effect.hasTarget {
				targetMultiplier = filterMatchCount(&effect.target, source, dead)
			}
			if targetMultiplier <= 0 {
				continue
			}
			for recipientIdx := range unitCount {
				recipient := &s.units[recipientIdx]
				recipientMultiplier := filterMatchCount(&effect.recipient, source, recipient)
				if recipientMultiplier <= 0 {
					continue
				}
				pct := mul(mul(effect.valuePct, float32(recipientMultiplier)), float32(targetMultiplier)) / 100
				s.recordPassiveHeal(a, recipientIdx, lround(mul(float32(recipient.maxHealth), pct)))
			}
		}
	}
}

func (s *sim) teamDead(side int) bool {
	for slot := range SlotCount {
		if s.unitAt(side, slot).alive {
			return false
		}
	}
	return true
}

func (s *sim) teamHealthFrac(side int) float32 {
	var current, maximum int64
	for slot := range SlotCount {
		u := s.unitAt(side, slot)
		if !u.present {
			continue
		}
		current += int64(u.health)
		maximum += int64(u.maxHealth)
	}
	if maximum <= 0 {
		return 0
	}
	return float32(current) / float32(maximum)
}

func recipePattern(actor *unit, target recipeTarget) targetPattern {
	if target == recipeTargetAbility {
		return actor.hero.def.ability.target
	}
	return targetPattern(target - 1)
}

func (s *sim) pickAlly(actor *unit) int {
	var choices []int
	for slot := range SlotCount {
		idx := actor.side*SlotCount + slot
		if slot != actor.slot && s.units[idx].alive {
			choices = append(choices, idx)
		}
	}
	if len(choices) == 0 {
		return -1
	}
	return choices[s.rangeRoll(0, len(choices)-1)]
}

func hasHostileEffect(hit *attackRecipe) bool {
	return hit.stunPct != 0 || hit.slowPct != 0 || hit.drainPct != 0 || hit.cursePowerPct != 0 ||
		hit.curseHealthPct != 0 || hit.assassinatePct != 0 || hit.markPct != 0
}

func (s *sim) applyPerHitSelfEffects(actorIdx int, hit *attackRecipe) {
	if hit.selfHealPct == 0 && hit.selfCritPct == 0 {
		return
	}
	actor := &s.units[actorIdx]
	st := Target{Unit: actorIdx}
	if hit.selfHealPct != 0 && !actor.curseHealth {
		st.HealthDelta = lround(mul(float32(actor.maxHealth), percent(hit.selfHealPct)))
	}
	st.CritDelta = float32(hit.selfCritPct)
	if st.HealthDelta == 0 && st.CritDelta == 0 {
		return
	}
	a := s.emit(actorIdx)
	if a == nil {
		return
	}
	s.applyTarget(a, st)
}

func (s *sim) runeResists(resistPct int) bool {
	return resistPct > 0 && s.chance(resistPct)
}

func (s *sim) effectLands(chancePct, resistPct int) bool {
	if chancePct <= 0 {
		return false
	}
	if resistPct < 0 {
		chancePct -= resistPct
	}
	return s.chance(chancePct) && !s.runeResists(resistPct)
}

func damageAfterResist(damage, shield, resist int) int {
	if damage <= 0 || (shield == 0 && resist == 0) {
		return damage
	}
	mitigated := damage - shield
	resist = min(resist, runeResistMaxPct)
	if resist != 0 {
		mitigated = lround(mul(float32(mitigated), percent(100-resist)))
	}
	return max(mitigated, runeShieldedHitMinDamage)
}

func (s *sim) mitigatedDamage(actor, t *unit, damage int) int {
	bonus := &s.runeBonus[t.side]
	shield := bonus.damageShield + t.gear.rawDamageResist + s.gearVersusStat(t, actor, gearStatRawDamageResist)
	resist := bonus.damageResistPct + t.gear.damageResistPct + s.gearVersusStat(t, actor, gearStatDamageResist)
	return damageAfterResist(damage, shield, resist)
}

func (s *sim) hitIsHidden(actor, t *unit) bool {
	stealth := s.runeBonus[actor.side].stealth || actor.gear.stealth
	perceptive := s.runeBonus[t.side].perceptive || t.gear.perceptive
	return stealth && !perceptive
}

func (s *sim) runeRetaliates(actor, t *unit) bool {
	if t.side == actor.side {
		return false
	}
	return s.runeBonus[t.side].retaliation > 0 && !s.hitIsHidden(actor, t)
}

func (s *sim) applyRuneRetaliation(actorIdx, reflected int) {
	if reflected <= 0 || !s.units[actorIdx].alive {
		return
	}
	a := s.emit(actorIdx)
	if a == nil {
		return
	}
	s.applyTarget(a, Target{Unit: actorIdx, HealthDelta: -reflected})
}

type gearReaction struct {
	targets [unitCount]Target
	touched [unitCount]bool
}

func (s *sim) gearReactionAdd(reaction *gearReaction, effect *gearEffect, recipientIdx, amount int) {
	recipient := &s.units[recipientIdx]
	pct := percent(effect.value)
	t := &reaction.targets[recipientIdx]
	t.Unit = recipientIdx
	base := 0
	switch effect.stat {
	case gearStatPower:
		base = recipient.startPower
		if effect.damageBasis {
			base = amount
		}
		if !recipient.cursePower {
			t.PowerDelta += lround(mul(float32(base), pct))
		}
	case gearStatHealth:
		base = recipient.maxHealth
		if effect.damageBasis {
			base = amount
		}
		if !recipient.curseHealth {
			t.HealthDelta += lround(mul(float32(base), pct))
		}
	case gearStatDamage:
		base = recipient.maxHealth
		if effect.damageBasis {
			base = amount
		}
		t.HealthDelta -= lround(mul(float32(base), pct))
	case gearStatDrain:
		if !s.runeResists(s.runeBonus[recipient.side].drainResistPct + recipient.gear.drainResistPct) {
			t.PowerDelta -= lround(mul(float32(recipient.startPower), pct))
		}
	case gearStatCrit:
		t.CritDelta += float32(effect.value)
	case gearStatBonusDamage:
		t.DamageBonusDelta += float32(effect.value)
	default:
		applyGearStat(&recipient.gear, effect.stat, effect.value)
		return
	}
	reaction.touched[recipientIdx] = true
}

func (s *sim) gearFireEffect(reaction *gearReaction, wearer *unit, effect *gearEffect, amount int) {
	if !wearerQualifies(effect, wearer) {
		return
	}
	for idx := range unitCount {
		recipient := &s.units[idx]
		if !recipient.alive {
			continue
		}
		multiplier := filterMatchCount(&effect.recipient, wearer, recipient)
		for range multiplier {
			s.gearReactionAdd(reaction, effect, idx, amount)
		}
	}
}

func (s *sim) gearFireTrigger(reaction *gearReaction, wearer *unit, on gearTrigger, amount int) {
	for _, def := range wearer.hero.gear {
		if def == nil {
			continue
		}
		for e := range def.effects {
			if def.effects[e].on == on {
				s.gearFireEffect(reaction, wearer, &def.effects[e], amount)
			}
		}
	}
}

func (s *sim) gearEmitReaction(actorIdx int, reaction *gearReaction) {
	var a *Action
	for idx := range unitCount {
		t := &reaction.targets[idx]
		if !reaction.touched[idx] || !s.units[idx].alive {
			continue
		}
		if t.HealthDelta == 0 && t.PowerDelta == 0 && t.CritDelta == 0 && t.DamageBonusDelta == 0 {
			continue
		}
		if a == nil {
			a = s.emit(actorIdx)
		}
		if a == nil {
			return
		}
		s.applyTarget(a, *t)
	}
}

func (s *sim) gearCanReact() bool {
	return len(s.actions) < maxActions-gearActionReserve
}

func (s *sim) gearResolveDeaths(actorIdx int) {
	for pass := 0; pass < gearDeathPasses && s.gearPendingDeaths != 0 && s.gearCanReact(); pass++ {
		deaths := s.gearPendingDeaths
		s.gearPendingDeaths = 0
		var reaction gearReaction
		for deadIdx := range unitCount {
			if deaths&(1<<deadIdx) == 0 {
				continue
			}
			dead := &s.units[deadIdx]
			s.gearFireTrigger(&reaction, dead, onSelfDeath, s.gearLastDamage)
			for idx := range unitCount {
				wearer := &s.units[idx]
				if !wearer.alive {
					continue
				}
				on := onEnemyDeath
				if wearer.side == dead.side {
					on = onAllyDeath
				}
				s.gearFireTrigger(&reaction, wearer, on, s.gearLastDamage)
			}
		}
		s.gearEmitReaction(actorIdx, &reaction)
	}
	s.gearPendingDeaths = 0
}

func (s *sim) gearReactToAttack(actorIdx int, hits []Target) {
	if !s.gearCanReact() {
		return
	}
	actor := &s.units[actorIdx]
	var reaction gearReaction
	dealtTotal := 0
	for h := range hits {
		target := &s.units[hits[h].Unit]
		dealt := -(hits[h].HealthDelta + hits[h].HealthFactionDelta)
		if dealt <= 0 {
			continue
		}
		dealtTotal += dealt
		hidden := target.side != actor.side && s.hitIsHidden(actor, target)
		for idx := range unitCount {
			wearer := &s.units[idx]
			if !wearer.alive {
				continue
			}
			switch {
			case wearer.side != target.side:
				s.gearFireTrigger(&reaction, wearer, onEnemyHit, dealt)
			case hidden:
				continue
			case wearer == target:
				s.gearFireTrigger(&reaction, wearer, onSelfHit, dealt)
			default:
				s.gearFireTrigger(&reaction, wearer, onAllyHit, dealt)
			}
		}
	}
	if dealtTotal > 0 {
		s.gearLastDamage = dealtTotal
		for idx := range unitCount {
			wearer := &s.units[idx]
			if !wearer.alive || wearer.side != actor.side {
				continue
			}
			on := onAllyAttack
			if idx == actorIdx {
				on = onSelfAttack
			}
			s.gearFireTrigger(&reaction, wearer, on, dealtTotal)
			if idx == actorIdx {
				s.gearFireTrigger(&reaction, wearer, onAllyAttack, dealtTotal)
			}
		}
	}
	s.gearEmitReaction(actorIdx, &reaction)
	s.gearResolveDeaths(actorIdx)
}

type landedHit struct {
	target   Target
	ailments uint32
}

func (s *sim) pickAttackTargets(actorIdx int, hit *attackRecipe, friendly bool) []int {
	actor := &s.units[actorIdx]
	picked := make([]int, 0, maxTargets)
	switch {
	case friendly:
		if pick := s.pickAlly(actor); pick >= 0 {
			picked = append(picked, pick)
		}
	case hit.target == recipeTargetAny:
		var choices []int
		for idx := range unitCount {
			if idx != actorIdx && s.units[idx].alive {
				choices = append(choices, idx)
			}
		}
		if len(choices) > 0 {
			picked = append(picked, choices[s.rangeRoll(0, len(choices)-1)])
		}
	case hit.target == recipeTargetBackRow:
		enemySide := 1 - actor.side
		for slot := range GiantSlot {
			if slotIsBack(slot) && s.unitAt(enemySide, slot).alive {
				picked = append(picked, enemySide*SlotCount+slot)
			}
		}
	default:
		picked = s.selectTargets(actor, recipePattern(actor, hit.target), picked)
	}
	return picked
}

func (s *sim) attack(actorIdx int, r *abilityRecipe, hit *attackRecipe, attacks, applySecondary bool) {
	actor := &s.units[actorIdx]
	friendly := hit.target == recipeTargetOneAlly
	var picked []int
	if attacks {
		picked = s.pickAttackTargets(actorIdx, hit, friendly)
		if len(picked) == 0 {
			return
		}
	}
	n := len(picked)

	crit := attacks && !friendly && !hit.statusOnly &&
		s.rangeRoll(1, critRollSides) <= lround(mul(actor.critPct, critRollScale))
	dmgPct := 0
	if !hit.statusOnly {
		dmgPct = hit.dmgPct
		if dmgPct == 0 {
			dmgPct = 100
		}
	}
	critMultiplier := 1
	if crit {
		critMultiplier = 2
	}

	base := make([]int, n)
	defender, defenderPct := -1, 0
	for i := range n {
		t := &s.units[picked[i]]
		bonusPct := actor.damageBonusPct + s.passiveBonusDamagePct(actor, t) +
			float32(s.gearVersusStat(actor, t, gearStatBonusDamage))
		damageScale := mul(percent(dmgPct), 1+bonusPct/100)
		base[i] = lround(mul(float32(actor.power), damageScale)) * critMultiplier
		if !hit.statusOnly && hit.levyPct > 0 {
			base[i] += lround(mul(float32(t.power), percent(hit.levyPct)))
		}
		if t.marked > 0 {
			base[i] += lround(mul(float32(base[i]), percent(t.marked)))
		}
		absorbPct := t.gear.defenderPct
		if t.hero.def.hasSpecialty(specDefender) {
			absorbPct = defenderAbsorbPct
		}
		if n > 1 && absorbPct > 0 {
			defender, defenderPct = i, absorbPct
		}
	}
	if defender >= 0 {
		for i := range n {
			if i == defender {
				continue
			}
			moved := int(int64(base[i]) * int64(defenderPct) / 100)
			base[i] -= moved
			base[defender] += moved
		}
	}
	for i := range n {
		t := &s.units[picked[i]]
		if t.side != actor.side {
			base[i] = s.mitigatedDamage(actor, t, base[i])
		}
	}

	hits := make([]landedHit, 0, n)
	reflected := 0
	assassinateTarget := -1
	if hit.assassinatePct > 0 && s.chance(hit.assassinatePct) {
		var choices []int
		for i := range n {
			if !s.units[picked[i]].giant() {
				choices = append(choices, picked[i])
			}
		}
		if len(choices) > 0 {
			assassinateTarget = choices[s.rangeRoll(0, len(choices)-1)]
		}
	}

	for i := range n {
		t := &s.units[picked[i]]
		extra := 0
		if t.side != actor.side && hasAdvantage(actor.faction, t.faction) {
			extra = lround(mul(factionAdvantageDamageRate, float32(base[i])))
		}
		bt := Target{Unit: picked[i], HealthDelta: -base[i], HealthFactionDelta: -extra}
		ailments := uint32(0)
		defense := &s.runeBonus[t.side]
		if s.effectLands(hit.stunPct, defense.stunResistPct+t.gear.stunResistPct) {
			ailments |= ailStunned
		}
		if s.effectLands(hit.slowPct, defense.slowResistPct+t.gear.slowResistPct) {
			bt.InitiativeDelta += hit.slowAmt
		}
		if hit.drainPct > 0 && (hit.drainChance == 0 || s.chance(hit.drainChance)) &&
			!s.runeResists(defense.drainResistPct+t.gear.drainResistPct) {
			bt.PowerDelta -= lround(mul(float32(actor.power), percent(hit.drainPct)))
		}
		if s.effectLands(hit.cursePowerPct, t.gear.cursePowerResistPct) && !defense.cursePowerImmune {
			ailments |= ailCursePower
		}
		if s.effectLands(hit.curseHealthPct, t.gear.curseHealthResistPct) {
			ailments |= ailCurseHealth
		}
		if picked[i] == assassinateTarget {
			if extra < t.health {
				bt.HealthDelta, bt.HealthFactionDelta = extra-t.health, -extra
			} else {
				bt.HealthDelta, bt.HealthFactionDelta = 0, -t.health
			}
		}
		if s.runeRetaliates(actor, t) {
			dealt := -float32(bt.HealthDelta + bt.HealthFactionDelta)
			reflected += lround(mul(dealt, percent(defense.retaliation)))
		}
		hits = append(hits, landedHit{target: bt, ailments: ailments})
	}

	firstRecord := true
	for mask := uint32(0); mask < 8; mask++ {
		var a *Action
		for i := range hits {
			if hits[i].ailments != mask {
				continue
			}
			if a == nil {
				if a = s.emit(actorIdx); a == nil {
					return
				}
				a.Crit = crit
				a.Ailments = mask
				a.SameSwing = !firstRecord
				a.ReplayAttack = firstRecord && !hit.statusOnly
				firstRecord = false
			}
			s.applyTarget(a, hits[i].target)
			t := &s.units[hits[i].target.Unit]
			if !t.alive {
				continue
			}
			if mask&ailStunned != 0 {
				t.stunned = true
			}
			curseTurns := hit.curseTurns
			if curseTurns <= 0 {
				curseTurns = 1
			}
			if mask&ailCursePower != 0 {
				t.cursePower = true
				t.cursePowerTurns = max(t.cursePowerTurns, curseTurns)
			}
			if mask&ailCurseHealth != 0 {
				t.curseHealth = true
				t.curseHealthTurns = max(t.curseHealthTurns, curseTurns)
			}
		}
	}
	if hit.markPct > 0 {
		for i := range n {
			if s.units[picked[i]].alive {
				s.units[picked[i]].marked = hit.markPct
			}
		}
	}

	s.applyPerHitSelfEffects(actorIdx, hit)

	landed := make([]Target, len(hits))
	for i := range hits {
		landed[i] = hits[i].target
	}
	s.gearReactToAttack(actorIdx, landed)

	if applySecondary {
		s.applySecondaryEffects(actorIdx, &r.fx)
		if s.overflow {
			return
		}
	}
	s.applyRuneRetaliation(actorIdx, reflected)
}

func (s *sim) applySecondaryEffects(actorIdx int, fx *secondaryEffects) {
	actor := &s.units[actorIdx]
	selfPower := fx.selfPowerPct != 0 && (fx.selfPowerChance == 0 || s.chance(fx.selfPowerChance))
	selfHeal := fx.selfHealPct != 0 && (fx.selfHealChance == 0 || s.chance(fx.selfHealChance))
	allyHeal := fx.allyHealPct != 0 && (fx.allyHealChance == 0 || s.chance(fx.allyHealChance))
	allyPower := fx.allyPowerPct != 0 && (fx.allyPowerChance == 0 || s.chance(fx.allyPowerChance))
	hasAlly := allyHeal || allyPower || fx.allyPowerFromActorPct != 0 || fx.allySlowAmt != 0 ||
		fx.allyCritPct != 0 || fx.allyDamageBonusPct != 0 || fx.allyDamagePct != 0
	if !selfPower && !selfHeal && fx.selfDamageBonusPct == 0 && fx.selfDamagePct == 0 && !hasAlly {
		return
	}
	secIndex := len(s.actions)
	sec := s.emit(actorIdx)
	if sec == nil {
		return
	}
	st := Target{Unit: actorIdx}
	if selfPower {
		st.PowerDelta += lround(mul(float32(actor.startPower), percent(fx.selfPowerPct)))
	}
	if actor.cursePower && st.PowerDelta > 0 {
		st.PowerDelta = 0
	}
	if selfHeal && !actor.curseHealth {
		st.HealthDelta += lround(mul(float32(actor.maxHealth), percent(fx.selfHealPct)))
	}
	if fx.selfDamageBonusPct != 0 {
		st.DamageBonusDelta += float32(fx.selfDamageBonusPct)
	}
	if fx.selfDamagePct != 0 {
		st.HealthDelta += lround(mul(float32(actor.power), percent(fx.selfDamagePct)))
	}
	if st.PowerDelta != 0 || st.HealthDelta != 0 || st.DamageBonusDelta != 0 {
		s.applyTarget(sec, st)
	}
	if hasAlly {
		for slot := range SlotCount {
			idx := actor.side*SlotCount + slot
			ally := &s.units[idx]
			if !ally.alive || idx == actorIdx {
				continue
			}
			at := Target{Unit: idx}
			if allyHeal && !ally.curseHealth {
				at.HealthDelta += lround(mul(float32(actor.power), percent(fx.allyHealPct)))
			}
			if allyPower && !ally.cursePower {
				at.PowerDelta += lround(mul(float32(ally.startPower), percent(fx.allyPowerPct)))
			}
			if fx.allyPowerFromActorPct != 0 && !ally.cursePower {
				at.PowerDelta += lround(mul(float32(actor.power), percent(fx.allyPowerFromActorPct)))
			}
			if fx.allySlowAmt != 0 {
				at.InitiativeDelta += fx.allySlowAmt
			}
			if fx.allyCritPct != 0 {
				at.CritDelta += float32(fx.allyCritPct)
			}
			if fx.allyDamageBonusPct != 0 {
				at.DamageBonusDelta += float32(fx.allyDamageBonusPct)
			}
			if fx.allyDamagePct != 0 {
				at.HealthDelta += lround(mul(float32(actor.power), percent(fx.allyDamagePct)))
			}
			if at.HealthDelta != 0 || at.PowerDelta != 0 || at.InitiativeDelta != 0 || at.CritDelta != 0 ||
				at.DamageBonusDelta != 0 {
				s.applyTarget(sec, at)
			}
		}
	}
	if len(sec.Targets) == 0 {
		s.actions = s.actions[:secIndex]
	}
}

func followupAttack(r *abilityRecipe, index int) attackRecipe {
	hit := r.followup
	damage := hit.dmgPct
	if damage == 0 {
		damage = 100
	}
	if r.followupDamageStepPct != 0 {
		damage += r.followupDamageStepPct * (index + 1)
	}
	if r.followupFinalDamagePct != 0 && index == r.followupCount-1 {
		damage = r.followupFinalDamagePct
	}
	hit.dmgPct = damage
	return hit
}

func (s *sim) expireAilments(actorIdx int, unstun bool) {
	u := &s.units[actorIdx]
	ailments := uint32(0)
	if unstun {
		ailments = ailUnstunned
	}
	if u.cursePower {
		if u.cursePowerTurns > 1 {
			u.cursePowerTurns--
		} else {
			ailments |= ailUncursePower
			u.cursePower = false
			u.cursePowerTurns = 0
		}
	}
	if u.curseHealth {
		if u.curseHealthTurns > 1 {
			u.curseHealthTurns--
		} else {
			ailments |= ailUncurseHealth
			u.curseHealth = false
			u.curseHealthTurns = 0
		}
	}
	if ailments != 0 {
		if a := s.emit(actorIdx); a != nil {
			a.Ailments = ailments
		}
	}
	u.stunned = false
}

func (s *sim) generatedTurn(actorIdx int, recipe *abilityRecipe, noAttack bool) {
	actor := &s.units[actorIdx]
	var chanceGroup [chanceGroupCount]int
	for i := range chanceGroup {
		chanceGroup[i] = -1
	}
	for i := range recipe.steps {
		step := &recipe.steps[i]
		for repeat := 0; repeat < step.repeat; repeat++ {
			if !actor.alive || s.overflow {
				return
			}
			proc := true
			if step.chancePct < 100 {
				if step.chanceGroup > 0 && step.chanceGroup < chanceGroupCount {
					if chanceGroup[step.chanceGroup] < 0 {
						chanceGroup[step.chanceGroup] = 0
						if s.chance(step.chancePct) {
							chanceGroup[step.chanceGroup] = 1
						}
					}
					proc = chanceGroup[step.chanceGroup] != 0
				} else {
					proc = s.chance(step.chancePct)
				}
			}
			if !proc {
				if step.stopOnFail {
					break
				}
				continue
			}
			hit := step.attack
			if step.finalDamagePct != 0 && repeat == step.repeat-1 {
				hit.dmgPct = step.finalDamagePct
			}
			if noAttack {
				hit.statusOnly = true
			}
			applyEffects := !step.effectsOnce || repeat == 0
			if applyEffects {
				hit.selfCritPct += step.selfCritPct
			}
			if hit.drainChance == alwaysPct {
				hit.drainChance = 0
			}
			var effects abilityRecipe
			if applyEffects {
				effects.fx = step.fx
			}
			hostile := hasHostileEffect(&hit)
			attacksEnemy := !hit.statusOnly
			if attacksEnemy && s.teamDead(1-actor.side) {
				return
			}
			s.attack(actorIdx, &effects, &hit, attacksEnemy || hostile, true)
		}
	}
}

func (s *sim) takeTurn(actorIdx int) {
	u := &s.units[actorIdx]
	if !u.alive {
		return
	}
	if u.stunned {
		s.expireAilments(actorIdx, true)
		return
	}
	r := u.recipe
	noAttack := u.hero.def.ability.mechanics&mechanicAttackNone != 0 || u.hero.def.hasSpecialty(specAttackNone)
	if r.generated {
		s.generatedTurn(actorIdx, r, noAttack)
	} else {
		primary := r.hit
		if noAttack {
			primary.statusOnly = true
		}
		s.attack(actorIdx, r, &primary, !noAttack || hasHostileEffect(&primary), true)

		if r.effectPct > 0 && u.alive && !s.teamDead(1-u.side) && s.chance(r.effectPct) {
			effect := r.effect
			if noAttack {
				effect.statusOnly = true
			}
			s.attack(actorIdx, r, &effect, !noAttack || hasHostileEffect(&effect), false)
		}

		for f := 0; f < r.followupCount; f++ {
			if s.teamDead(1-u.side) || !u.alive || s.overflow {
				break
			}
			if !s.chance(r.followupPct) {
				break
			}
			followup := followupAttack(r, f)
			if noAttack {
				followup.statusOnly = true
			}
			s.attack(actorIdx, r, &followup, !noAttack || hasHostileEffect(&followup), false)
		}
	}
	s.expireAilments(actorIdx, false)
}

func (s *sim) rollInitiative(u *unit) int {
	lo := u.initClass * initClassSpan
	hi := lo + initClassSpan - 1
	runeSteps := s.runeBonus[u.side].initiativeSteps
	roll := s.rangeRoll(lo, hi)
	if u.gear.haste {
		roll = max(roll, s.rangeRoll(lo, hi))
	}
	return roll + u.curInitiative + runeSteps*initClassSpan
}

func (s *sim) rollRoundOrder() []int {
	order := make([]int, 0, unitCount)
	inits := make([]int, 0, unitCount)
	for i := range unitCount {
		if !s.units[i].alive {
			continue
		}
		order = append(order, i)
		inits = append(inits, s.rollInitiative(&s.units[i]))
		s.units[i].curInitiative = 0
	}
	for i := 1; i < len(order); i++ {
		oi, vi, j := order[i], inits[i], i-1
		for j >= 0 && (inits[j] < vi || (inits[j] == vi && order[j] > oi)) {
			order[j+1] = order[j]
			inits[j+1] = inits[j]
			j--
		}
		order[j+1] = oi
		inits[j+1] = vi
	}
	return order
}

func (s *sim) applyRuneBonus(side int, bonus *runeBonus) {
	for slot := range SlotCount {
		u := s.unitAt(side, slot)
		if !u.present {
			continue
		}
		u.power = runeApplyPct(u.power, bonus.powerPct)
		u.startPower = u.power
		u.maxHealth = runeApplyPct(u.maxHealth, bonus.healthPct)
		u.health = u.maxHealth
	}
}

func changerEffectMatches(effect *ChangerEffect, u *unit) bool {
	for _, group := range effect.Groups {
		if !u.hero.def.hasGroup(group) {
			return false
		}
	}
	if effect.FactionMask != 0 && u.faction&effect.FactionMask == 0 {
		return false
	}
	if len(effect.Units) == 0 {
		return true
	}
	for _, id := range effect.Units {
		if u.hero.def.id == id {
			return true
		}
	}
	return false
}

func (s *sim) applyChanger(changer *Changer) {
	if changer == nil {
		return
	}
	for i := range unitCount {
		u := &s.units[i]
		if !u.present {
			continue
		}
		for e := range changer.Effects {
			effect := &changer.Effects[e]
			if !changerEffectMatches(effect, u) {
				continue
			}
			powerPct, healthPct := effect.PowerPct, effect.HealthPct
			if s.budget != nil {
				powerPct, healthPct = s.budget.cappedChangerPct(powerPct), s.budget.cappedChangerPct(healthPct)
			}
			u.power = lround(mul(float32(u.power), 1+percent(powerPct)))
			u.startPower = u.power
			u.maxHealth = lround(mul(float32(u.maxHealth), 1+percent(healthPct)))
			u.health = u.maxHealth
			u.gameChangerApplied = true
		}
	}
}

func (s *sim) setup(player, enemy *side, changer *Changer) {
	for slot := range SlotCount {
		s.initUnit(0, slot, player.heroes[slot])
		s.initUnit(1, slot, enemy.heroes[slot])
	}
	s.battleStartPassives()
	s.formationBonus(0)
	s.formationBonus(1)
	s.runeBonus[0] = runesBonus(player.runes)
	s.runeBonus[1] = runesBonus(enemy.runes)
	if s.budget != nil {
		s.runeBonus[0].dropFlatShield()
		s.runeBonus[1].dropFlatShield()
	}
	s.applyRuneBonus(0, &s.runeBonus[0])
	s.applyRuneBonus(1, &s.runeBonus[1])
	s.applyChanger(changer)
}

func (s *sim) run(player, enemy *side, changer *Changer, seed uint32) error {
	s.setup(player, enemy, changer)
	s.rng = seed
	if s.rng == 0 {
		s.rng = defaultSeed
	}
	for round := 1; round <= roundCap; round++ {
		s.rounds = round
		if round > 1 {
			if nr := s.emit(-1); nr != nil {
				nr.NewRound = true
			}
		}
		for _, actorIdx := range s.rollRoundOrder() {
			s.takeTurn(actorIdx)
			s.gearResolveDeaths(actorIdx)
			if s.overflow {
				return ErrActionOverflow
			}
			if s.teamDead(0) || s.teamDead(1) {
				s.playerWon = s.teamDead(1)
				return nil
			}
		}
	}
	s.playerWon = s.teamHealthFrac(0) > s.teamHealthFrac(1)
	return nil
}
