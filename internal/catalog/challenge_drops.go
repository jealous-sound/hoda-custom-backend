package catalog

import "fmt"

const (
	ChallengeDropsFile      = "challenge_drops.json"
	supportedChallengeDrops = 1
)

type ChallengeDropKind string

const (
	ChallengeDropUnit ChallengeDropKind = "unit"
	ChallengeDropRune ChallengeDropKind = "rune"
)

type ChallengeDrop struct {
	Kind   ChallengeDropKind
	ID     string
	Weight int
}

type ChallengeDrops struct {
	RollTotal int
	Maps      map[string][]ChallengeDrop
}

func (d ChallengeDrops) Pick(mapID string, roll int) (ChallengeDrop, bool) {
	for _, drop := range d.Maps[mapID] {
		if roll < drop.Weight {
			return drop, true
		}
		roll -= drop.Weight
	}
	return ChallengeDrop{}, false
}

func (c *Catalog) checkChallengeDrop(drop ChallengeDrop) error {
	if drop.Weight <= 0 {
		return fmt.Errorf("%s weighs %d, want a positive weight", drop.ID, drop.Weight)
	}
	switch drop.Kind {
	case ChallengeDropUnit:
		if hero, ok := c.Hero(drop.ID); !ok || !hero.Obtainable {
			return fmt.Errorf("names unknown or unobtainable hero %s", drop.ID)
		}
	case ChallengeDropRune:
		if !c.RuneKnown(drop.ID) {
			return fmt.Errorf("names unknown rune %s", drop.ID)
		}
	default:
		return fmt.Errorf("%s has unknown kind %q", drop.ID, drop.Kind)
	}
	return nil
}

func (c *Catalog) challengeMap(id string) (CampaignMap, bool) {
	for _, m := range c.Campaign {
		if m.ID == id {
			return m, true
		}
	}
	return CampaignMap{}, false
}

func (c *Catalog) loadChallengeDrops(read func(name string) ([]byte, error)) error {
	var raw struct {
		Version   int `json:"version"`
		RollTotal int `json:"roll_total"`
		Maps      []struct {
			Map   string `json:"map"`
			Drops []struct {
				Kind   ChallengeDropKind `json:"kind"`
				ID     string            `json:"id"`
				Weight int               `json:"weight"`
			} `json:"drops"`
		} `json:"maps"`
	}
	if err := decodeFile(read, ChallengeDropsFile, &raw); err != nil {
		return err
	}
	if raw.Version != supportedChallengeDrops {
		return fmt.Errorf("%s version %d, want %d", ChallengeDropsFile, raw.Version, supportedChallengeDrops)
	}
	if raw.RollTotal <= 0 {
		return fmt.Errorf("%s needs a positive roll_total", ChallengeDropsFile)
	}
	drops := ChallengeDrops{RollTotal: raw.RollTotal, Maps: map[string][]ChallengeDrop{}}
	for _, table := range raw.Maps {
		m, known := c.challengeMap(table.Map)
		if !known || len(m.Nodes) == 0 || !m.Nodes[len(m.Nodes)-1].Grind {
			return fmt.Errorf("%s lists %s, which has no challenge node", ChallengeDropsFile, table.Map)
		}
		if _, listed := drops.Maps[table.Map]; listed || len(table.Drops) == 0 {
			return fmt.Errorf("%s lists %s twice or with no drops", ChallengeDropsFile, table.Map)
		}
		total := 0
		for _, entry := range table.Drops {
			drop := ChallengeDrop{Kind: entry.Kind, ID: entry.ID, Weight: entry.Weight}
			if err := c.checkChallengeDrop(drop); err != nil {
				return fmt.Errorf("%s %s: %w", ChallengeDropsFile, table.Map, err)
			}
			total += drop.Weight
			drops.Maps[table.Map] = append(drops.Maps[table.Map], drop)
		}
		if total >= raw.RollTotal {
			return fmt.Errorf("%s %s weighs %d of %d, leaving no empty roll", ChallengeDropsFile, table.Map, total, raw.RollTotal)
		}
	}
	c.ChallengeDrops = drops
	return nil
}
