package battle

import (
	"encoding/json"
	"fmt"
)

const (
	CharactersFile        = "characters.json"
	CustomHeroesFile      = "custom_heroes.json"
	PassivesFile          = "intrinsic_passives.json"
	GeneratedRecipesFile  = "ability_recipes.json"
	BuiltinRecipesFile    = "battle_recipes.json"
	GearFile              = "gear.json"
	RunesFile             = "runes.json"
	ConsumeFile           = "consume_tables.json"
	CampaignRostersFile   = "campaign_rosters.json"
	NexusRostersFile      = "nexus_rosters.json"
	NexusGameChangersFile = "nexus_game_changers.json"
	PvpRulesFile          = "pvp_rules.json"
	FortifyFile           = "fortify_tables.json"
)

type Data struct {
	characters       map[string]*character
	passives         map[string][]passiveEffect
	builtinRecipes   map[string]*abilityRecipe
	generatedRecipes map[string]*abilityRecipe
	gear             map[string]*gearDef
	runes            []*runeDef
	runesByID        map[string]*runeDef
	consume          map[string]*consumeProfile
	fortify          *fortifyTables
	campaignRosters  map[rosterKey]Side
	nexusRosters     map[rosterKey]Side
	nexusChangers    map[string]*Changer
	pvp              *pvpTables
}

func Load(read func(name string) ([]byte, error)) (*Data, error) {
	d := &Data{}
	var err error
	if d.characters, err = loadCharacters(read, CharactersFile, CustomHeroesFile); err != nil {
		return nil, err
	}
	steps := []struct {
		file  string
		parse func([]byte) error
	}{
		{PassivesFile, func(b []byte) (err error) { d.passives, err = loadPassives(b); return }},
		{BuiltinRecipesFile, func(b []byte) (err error) { d.builtinRecipes, err = loadBuiltinRecipes(b); return }},
		{GeneratedRecipesFile, func(b []byte) (err error) { d.generatedRecipes, err = loadGeneratedRecipes(b); return }},
		{GearFile, func(b []byte) (err error) { d.gear, err = loadGear(b); return }},
		{RunesFile, func(b []byte) (err error) { d.runes, d.runesByID, err = loadRunes(b); return }},
		{FortifyFile, func(b []byte) (err error) { d.fortify, err = loadFortify(b); return }},
		{ConsumeFile, func(b []byte) (err error) { d.consume, err = loadConsume(b, d.fortify.archetypes); return }},
		{CampaignRostersFile, func(b []byte) (err error) { d.campaignRosters, err = loadRosters(b); return }},
		{NexusRostersFile, func(b []byte) (err error) { d.nexusRosters, err = loadRosters(b); return }},
		{NexusGameChangersFile, func(b []byte) (err error) { d.nexusChangers, err = loadNexusChangers(b); return }},
		{PvpRulesFile, func(b []byte) (err error) { d.pvp, err = loadPvpTables(b); return }},
	}
	for _, step := range steps {
		data, err := read(step.file)
		if err != nil {
			return nil, err
		}
		if err := step.parse(data); err != nil {
			return nil, err
		}
	}
	return d, nil
}

type rosterKey struct {
	campaign string
	step     int
}

type rawCell struct {
	ID    string           `json:"id"`
	Tier  string           `json:"tier"`
	Level *json.RawMessage `json:"level"`
}

func cellLevel(raw *json.RawMessage) int {
	var level float64
	if raw == nil || json.Unmarshal(*raw, &level) != nil {
		return 1
	}
	return int(level)
}

func loadRosters(data []byte) (map[rosterKey]Side, error) {
	var root struct {
		Campaigns []struct {
			Campaign string `json:"campaign"`
			Nodes    []struct {
				Step   int        `json:"step"`
				Roster []*rawCell `json:"roster"`
			} `json:"nodes"`
		} `json:"campaigns"`
	}
	if err := json.Unmarshal(data, &root); err != nil {
		return nil, fmt.Errorf("rosters: %w", err)
	}
	rosters := map[rosterKey]Side{}
	for _, campaign := range root.Campaigns {
		for _, node := range campaign.Nodes {
			if len(node.Roster) > SlotCount {
				return nil, fmt.Errorf("rosters: %s step %d has too many cells", campaign.Campaign, node.Step)
			}
			roster := Side{Units: []Unit{}, Runes: []string{}}
			for slot, cell := range node.Roster {
				if cell == nil {
					continue
				}
				roster.Units = append(roster.Units, Unit{
					Slot: slot, ID: cell.ID, Tier: ParseTier(cell.Tier).String(), Level: cellLevel(cell.Level),
				})
			}
			rosters[rosterKey{campaign.Campaign, node.Step}] = roster
		}
	}
	return rosters, nil
}

type rawChangerEffect struct {
	Groups    []string `json:"groups"`
	Units     []string `json:"units"`
	Factions  []string `json:"factions"`
	HealthPct int      `json:"health_pct"`
	PowerPct  int      `json:"power_pct"`
}

const (
	maxChangerEffects = 4
	maxChangerGroups  = 2
	maxChangerUnits   = 2
)

func parseChanger(effects []rawChangerEffect) (*Changer, error) {
	if len(effects) > maxChangerEffects {
		return nil, fmt.Errorf("game changer has %d effects", len(effects))
	}
	changer := &Changer{Effects: []ChangerEffect{}}
	for _, raw := range effects {
		if len(raw.Groups) > maxChangerGroups || len(raw.Units) > maxChangerUnits {
			return nil, fmt.Errorf("game changer effect has too many groups or units")
		}
		effect := ChangerEffect{
			Groups: append([]string{}, raw.Groups...), Units: append([]string{}, raw.Units...),
			HealthPct: raw.HealthPct, PowerPct: raw.PowerPct,
		}
		for _, name := range raw.Factions {
			effect.FactionMask |= factionMask(parseFaction(name))
		}
		changer.Effects = append(changer.Effects, effect)
	}
	return changer, nil
}

func loadNexusChangers(data []byte) (map[string]*Changer, error) {
	var root []struct {
		Event   string             `json:"event"`
		Effects []rawChangerEffect `json:"effects"`
	}
	if err := json.Unmarshal(data, &root); err != nil {
		return nil, fmt.Errorf("nexus game changers: %w", err)
	}
	changers := map[string]*Changer{}
	for _, item := range root {
		changer, err := parseChanger(item.Effects)
		if err != nil {
			return nil, fmt.Errorf("nexus game changers: %s: %w", item.Event, err)
		}
		if previous, seen := changers[item.Event]; seen {
			previous.Effects = append(previous.Effects, changer.Effects...)
			if len(previous.Effects) > maxChangerEffects {
				return nil, fmt.Errorf("nexus game changers: %s has too many effects", item.Event)
			}
			continue
		}
		changers[item.Event] = changer
	}
	return changers, nil
}

func (d *Data) CampaignRoster(campaign string, step int) (Side, bool) {
	roster, ok := d.campaignRosters[rosterKey{campaign, step}]
	return roster, ok
}

func (d *Data) NexusRoster(difficulty string, step int) (Side, bool) {
	roster, ok := d.nexusRosters[rosterKey{difficulty, step}]
	return roster, ok
}

func (d *Data) NexusChanger(event string) *Changer {
	return d.nexusChangers[event]
}

func (d *Data) Knows(id string) bool {
	_, ok := d.characters[id]
	return ok
}

func (d *Data) RuneKnown(id string) bool {
	_, ok := d.runesByID[id]
	return ok
}
