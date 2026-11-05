package economy

import (
	"math"
	"time"

	"github.com/jealous-sound/hoda-custom-backend/internal/profile"
)

const (
	MaxPlayerLevel    = 100
	StartEnergyMax    = 12
	StartStaminaMax   = 8
	StartGold         = 1725
	StartGems         = 41
	StartPotions      = 3
	StartPotionMax    = 10
	RegenPeriod       = 300 * time.Second
	xpQuadratic       = 5
	xpLinear          = 15
	energyRegenTimer  = "energy_regen_timer"
	staminaRegenTimer = "stamina_regen_timer"
)

func cumulativeXPThroughLevel(level int) int64 {
	if level <= 0 {
		return 0
	}
	l := int64(level)
	return xpQuadratic*l*l + xpLinear*l
}

func clampLevel(level int) int {
	return min(max(level, 1), MaxPlayerLevel)
}

func xpForCurrentLevel(level int) int64 {
	return cumulativeXPThroughLevel(clampLevel(level) - 1)
}

func xpForNextLevel(level int) int64 {
	level = clampLevel(level)
	if level == MaxPlayerLevel {
		return xpForCurrentLevel(level)
	}
	return cumulativeXPThroughLevel(level)
}

func energyMaxForLevel(level int) int64 {
	return StartEnergyMax + int64(clampLevel(level)) - 1
}

func saturatingAdd(value, amount int64) int64 {
	if amount <= 0 {
		return value
	}
	if value > math.MaxInt32-amount {
		return math.MaxInt32
	}
	return value + amount
}

type account struct {
	doc        document
	Level      int
	Experience int64
	Energy     int64
	EnergyMax  int64
	Stamina    int64
	StaminaMax int64
	Gold       int64
	Gems       int64
}

func loadAccount(doc document) (*account, error) {
	section, err := doc.section("account")
	if err != nil {
		return nil, refuse(BadProfile, "%v", err)
	}
	a := &account{doc: section}
	level, err := section.intField("level")
	if err != nil {
		return nil, refuse(BadProfile, "%v", err)
	}
	a.Level = clampLevel(level)
	fields := map[string]*int64{
		"experience": &a.Experience, "energy": &a.Energy, "energy_max": &a.EnergyMax,
		"stamina": &a.Stamina, "stamina_max": &a.StaminaMax, "gold": &a.Gold, "gems": &a.Gems,
	}
	for name, target := range fields {
		if _, present := section[name]; !present {
			continue
		}
		value, err := section.int64Field(name)
		if err != nil {
			return nil, refuse(BadProfile, "%v", err)
		}
		*target = value
	}
	return a, nil
}

func (a *account) store() {
	a.doc.setInt("level", int64(a.Level))
	a.doc.setInt("experience", a.Experience)
	a.doc.setInt("experience_min", xpForCurrentLevel(a.Level))
	a.doc.setInt("experience_max", xpForNextLevel(a.Level))
	a.doc.setInt("energy", a.Energy)
	a.doc.setInt("energy_max", a.EnergyMax)
	a.doc.setInt("stamina", a.Stamina)
	a.doc.setInt("stamina_max", a.StaminaMax)
	a.doc.setInt("gold", a.Gold)
	a.doc.setInt("gems", a.Gems)
}

func (a *account) spendGold(cost int64) error {
	if cost < 0 || a.Gold < cost {
		return refuse(CantAfford, "needs %d gold", cost)
	}
	a.Gold -= cost
	return nil
}

func (a *account) spendGems(cost int64) error {
	if cost < 0 || a.Gems < cost {
		return refuse(CantAfford, "needs %d gems", cost)
	}
	a.Gems -= cost
	return nil
}

func (a *account) spendEnergy(cost int64) error {
	if cost < 0 || a.Energy < cost {
		return refuse(CantAfford, "needs %d energy", cost)
	}
	a.Energy -= cost
	return nil
}

func (a *account) spendStamina(cost int64) error {
	if cost < 0 || a.Stamina < cost {
		return refuse(CantAfford, "needs %d stamina", cost)
	}
	a.Stamina -= cost
	return nil
}

func (a *account) awardBattleXP(xp int64) {
	if xp <= 0 {
		return
	}
	before := a.Level
	a.Experience = saturatingAdd(a.Experience, xp)
	for a.Level < MaxPlayerLevel && a.Experience >= xpForNextLevel(a.Level) {
		a.Level++
	}
	if a.Level == MaxPlayerLevel && a.Experience > xpForNextLevel(a.Level) {
		a.Experience = xpForNextLevel(a.Level)
	}
	if a.Level == before {
		return
	}
	a.EnergyMax = energyMaxForLevel(a.Level)
	a.Energy = a.EnergyMax
	a.Stamina = a.StaminaMax
	a.doc.setFloat(energyRegenTimer, 0)
	a.doc.setFloat(staminaRegenTimer, 0)
}

type regenPool struct {
	value, maximum, timer string
}

var regenPools = []regenPool{
	{"energy", "energy_max", energyRegenTimer},
	{"stamina", "stamina_max", staminaRegenTimer},
}

func regenSince(server *profile.ServerState) time.Time {
	if server.RegenAt <= 0 {
		return time.Time{}
	}
	return time.Unix(server.RegenAt, 0)
}

func regenerate(doc document, since, now time.Time) error {
	section, err := doc.section("account")
	if err != nil {
		return refuse(BadProfile, "%v", err)
	}
	elapsed := 0.0
	if !since.IsZero() && now.After(since) {
		elapsed = now.Sub(since).Seconds()
	}
	period := RegenPeriod.Seconds()
	for _, pool := range regenPools {
		value, err := section.int64Field(pool.value)
		if err != nil {
			return refuse(BadProfile, "%v", err)
		}
		maximum, err := section.int64Field(pool.maximum)
		if err != nil {
			return refuse(BadProfile, "%v", err)
		}
		timer := 0.0
		if _, ok := section[pool.timer]; ok {
			if timer, err = section.floatField(pool.timer); err != nil {
				return refuse(BadProfile, "%v", err)
			}
		}
		if value >= maximum {
			section.setFloat(pool.timer, 0)
			continue
		}
		timer = max(timer, 0) + elapsed
		gained := min(int64(timer/period), maximum-value)
		value += gained
		timer -= float64(gained) * period
		if value >= maximum {
			timer = 0
		}
		section.setInt(pool.value, value)
		section.setFloat(pool.timer, timer)
	}
	return nil
}
