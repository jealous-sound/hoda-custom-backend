package battle

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math"
)

type Unit struct {
	Slot       int      `json:"slot"`
	InstanceID int64    `json:"instance_id,omitempty"`
	ID         string   `json:"id"`
	Tier       string   `json:"tier"`
	Level      int      `json:"level"`
	ConsumeXP  int      `json:"consume_xp,omitempty"`
	Ascended   bool     `json:"ascended,omitempty"`
	Gear       []string `json:"gear,omitempty"`
}

type Side struct {
	Units []Unit   `json:"units"`
	Runes []string `json:"runes"`
}

type ChangerEffect struct {
	Groups      []string `json:"groups"`
	Units       []string `json:"units"`
	FactionMask uint32   `json:"faction_mask"`
	HealthPct   int      `json:"health_pct"`
	PowerPct    int      `json:"power_pct"`
}

type Changer struct {
	Effects []ChangerEffect `json:"effects"`
}

type Replay struct {
	Seed    uint32   `json:"seed"`
	Player  Side     `json:"player"`
	Enemy   Side     `json:"enemy"`
	Changer *Changer `json:"changer"`
}

type Outcome struct {
	Won         bool   `json:"won"`
	Crits       int    `json:"crits"`
	Rounds      int    `json:"rounds"`
	Actions     int    `json:"actions"`
	Timeline    string `json:"timeline"`
	Damage      int64  `json:"damage"`
	FactionHit  bool   `json:"faction_hit"`
	SquadBuffed bool   `json:"squad_buffed"`
}

const CritGoldPerCrit = 10

func CritGoldFor(crits int) int {
	if crits > math.MaxInt32/CritGoldPerCrit {
		return math.MaxInt32
	}
	return crits * CritGoldPerCrit
}

type hero struct {
	def        *character
	rarity     rarity
	tier       Tier
	level      int
	consumeXP  int
	critChance int
	stats      [tierCount]tierStats
	gear       [gearSlotCapacity]*gearDef
	recipe     *abilityRecipe
	passives   []passiveEffect
}

func (h *hero) statPower() int {
	st := &h.stats[h.tier]
	return st.basePower + (h.level-1)*st.powerPerLevel
}

func (h *hero) statHealth() int {
	st := &h.stats[h.tier]
	return st.baseHealth + (h.level-1)*st.healthPerLevel
}

type side struct {
	heroes [SlotCount]*hero
	runes  []*runeDef
}

func (d *Data) buildHero(u Unit, playerSide bool) (*hero, error) {
	if u.Slot < 0 || u.Slot >= SlotCount {
		return nil, fmt.Errorf("unit %s has slot %d", u.ID, u.Slot)
	}
	def, ok := d.characters[u.ID]
	if !ok {
		return nil, fmt.Errorf("unknown unit %s", u.ID)
	}
	h := &hero{def: def, rarity: def.rarity, tier: ParseTier(u.Tier), level: max(u.Level, 1), stats: def.stats}
	if !def.hasTier[h.tier] || def.giant != (u.Slot == GiantSlot) {
		return nil, fmt.Errorf("unit %s cannot stand in slot %d at tier %s", u.ID, u.Slot, u.Tier)
	}
	h.recipe = d.recipeFor(def.ability.key, def.ability.mechanics)
	h.passives = d.passives[def.dataID()]
	if !playerSide {
		return h, nil
	}
	if u.Ascended {
		h.rarity = rarityMythic
		for t := range h.stats {
			h.stats[t].maxLevel += ascensionBonusLevels
		}
	}
	profile, ok := d.consume[def.dataID()]
	if !ok {
		return nil, fmt.Errorf("unit %s has no consume profile", u.ID)
	}
	h.consumeXP = u.ConsumeXP
	profile.normalize(h)
	if len(u.Gear) > gearSlotCapacity {
		return nil, fmt.Errorf("unit %s has %d gear slots", u.ID, len(u.Gear))
	}
	for slot, id := range u.Gear {
		if id == "" {
			continue
		}
		gear, ok := d.gear[id]
		if !ok {
			return nil, fmt.Errorf("unknown gear %s", id)
		}
		h.gear[slot] = gear
	}
	return h, nil
}

func (d *Data) buildSide(s Side, playerSide bool) (*side, error) {
	built := &side{}
	for _, u := range s.Units {
		h, err := d.buildHero(u, playerSide)
		if err != nil {
			return nil, err
		}
		if built.heroes[u.Slot] != nil {
			return nil, fmt.Errorf("slot %d is taken twice", u.Slot)
		}
		built.heroes[u.Slot] = h
	}
	if len(s.Runes) > RuneSlotCount {
		return nil, fmt.Errorf("%d runes for %d slots", len(s.Runes), RuneSlotCount)
	}
	for _, id := range s.Runes {
		def, ok := d.runesByID[id]
		if !ok {
			return nil, fmt.Errorf("unknown rune %s", id)
		}
		built.runes = append(built.runes, def)
	}
	return built, nil
}

func (d *Data) Run(r Replay) (Outcome, error) {
	player, err := d.buildSide(r.Player, true)
	if err != nil {
		return Outcome{}, fmt.Errorf("player side: %w", err)
	}
	enemy, err := d.buildSide(r.Enemy, false)
	if err != nil {
		return Outcome{}, fmt.Errorf("enemy side: %w", err)
	}
	s := &sim{actions: make([]Action, 0, 256)}
	if err := s.run(player, enemy, r.Changer, r.Seed); err != nil {
		return Outcome{}, err
	}
	damage, factionHit := s.playerDamage()
	return Outcome{
		Won: s.playerWon, Crits: s.critSwings(0), Rounds: s.rounds, Actions: len(s.actions),
		Timeline: s.digest(), Damage: damage, FactionHit: factionHit, SquadBuffed: s.squadBuffed(),
	}, nil
}

func (s *sim) playerDamage() (int64, bool) {
	var damage int64
	factionHit := false
	for i := range s.actions {
		a := &s.actions[i]
		if a.Actor < 0 || a.Actor/SlotCount != 0 {
			continue
		}
		for _, t := range a.Targets {
			if t.Unit/SlotCount == 0 {
				continue
			}
			if dealt := t.HealthDelta + t.HealthFactionDelta; dealt < 0 {
				damage -= int64(dealt)
			}
			if t.HealthFactionDelta < 0 {
				factionHit = true
			}
		}
	}
	return damage, factionHit
}

func (s *sim) squadBuffed() bool {
	any := false
	for slot := range SlotCount {
		u := s.unitAt(0, slot)
		if !u.present {
			continue
		}
		if !u.gameChangerApplied {
			return false
		}
		any = true
	}
	return any
}

func (s *sim) swingContinues(index, actor int) bool {
	if index < 0 || index >= len(s.actions) {
		return false
	}
	a := &s.actions[index]
	return a.SameSwing && !a.NewRound && a.Actor == actor
}

func (s *sim) critSwings(side int) int {
	crits := 0
	for i := 0; i < len(s.actions); {
		a := &s.actions[i]
		if a.NewRound || a.Actor < 0 {
			i++
			continue
		}
		attackerSide := a.Actor / SlotCount
		crit, hitEnemy := false, false
		next := i
		for {
			part := &s.actions[next]
			crit = crit || part.Crit
			for _, t := range part.Targets {
				if t.Unit/SlotCount != attackerSide && t.HealthDelta+t.HealthFactionDelta < 0 {
					hitEnemy = true
				}
			}
			next++
			if !s.swingContinues(next, a.Actor) {
				break
			}
		}
		if crit && hitEnemy && attackerSide == side {
			crits++
		}
		i = next
	}
	return crits
}

func bit(value bool) int {
	if value {
		return 1
	}
	return 0
}

func (s *sim) digest() string {
	hash := sha256.New()
	for i := range s.actions {
		a := &s.actions[i]
		fmt.Fprintf(hash, "%d %d%d%d%d %d %d", a.Actor, bit(a.NewRound), bit(a.Crit), bit(a.ReplayAttack),
			bit(a.SameSwing), a.Ailments, len(a.Targets))
		for _, t := range a.Targets {
			fmt.Fprintf(hash, " %d %d %d %d %d %08x %08x", t.Unit, t.HealthDelta, t.PowerDelta, t.InitiativeDelta,
				t.HealthFactionDelta, math.Float32bits(t.CritDelta), math.Float32bits(t.DamageBonusDelta))
		}
		hash.Write([]byte("\n"))
	}
	return hex.EncodeToString(hash.Sum(nil))
}
