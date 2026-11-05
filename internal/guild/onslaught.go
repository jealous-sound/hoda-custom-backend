package guild

import (
	"cmp"
	"maps"
	"slices"
)

const (
	OnslaughtsKept            = 6
	OnslaughtLeaderboardLimit = 100
)

type OnslaughtEntry struct {
	GuildID string            `json:"guild_id"`
	Name    string            `json:"name"`
	Icon    int               `json:"icon"`
	Members map[string]int    `json:"members"`
	Names   map[string]string `json:"names"`
}

func (e *OnslaughtEntry) Banners() int {
	total := 0
	for _, banners := range e.Members {
		total += banners
	}
	return total
}

func (e *OnslaughtEntry) clone() *OnslaughtEntry {
	copied := *e
	copied.Members = maps.Clone(e.Members)
	copied.Names = maps.Clone(e.Names)
	return &copied
}

type Onslaught struct {
	Cycle   int64             `json:"cycle"`
	Entries []*OnslaughtEntry `json:"guilds"`
	Claimed []string          `json:"claimed"`
}

func (o *Onslaught) clone() *Onslaught {
	copied := &Onslaught{Cycle: o.Cycle, Claimed: slices.Clone(o.Claimed)}
	for _, entry := range o.Entries {
		copied.Entries = append(copied.Entries, entry.clone())
	}
	return copied
}

type Standing struct {
	GuildID string `json:"id"`
	Name    string `json:"name"`
	Icon    int    `json:"icon"`
	Banners int    `json:"banners"`
	Rank    int    `json:"rank"`
}

type Contribution struct {
	PlayerID string `json:"id"`
	Name     string `json:"name"`
	Banners  int    `json:"banners"`
}

func (s *State) onslaught(cycle int64) *Onslaught {
	for _, o := range s.Onslaughts {
		if o.Cycle == cycle {
			return o
		}
	}
	return nil
}

func (s *State) openOnslaught(cycle int64) *Onslaught {
	if o := s.onslaught(cycle); o != nil {
		return o
	}
	o := &Onslaught{Cycle: cycle, Entries: []*OnslaughtEntry{}, Claimed: []string{}}
	s.Onslaughts = append(s.Onslaughts, o)
	slices.SortFunc(s.Onslaughts, func(a, b *Onslaught) int { return cmp.Compare(a.Cycle, b.Cycle) })
	if extra := len(s.Onslaughts) - OnslaughtsKept; extra > 0 {
		s.Onslaughts = slices.Clone(s.Onslaughts[extra:])
	}
	return o
}

func (o *Onslaught) entry(guildID string) *OnslaughtEntry {
	for _, entry := range o.Entries {
		if entry.GuildID == guildID {
			return entry
		}
	}
	return nil
}

func (o *Onslaught) contributor(playerID string) *OnslaughtEntry {
	for _, entry := range o.Entries {
		if _, found := entry.Members[playerID]; found {
			return entry
		}
	}
	return nil
}

func (s *State) CreditOnslaught(p Player, cycle, start int64, banners int) bool {
	g := s.GuildOf(p.ID)
	if g == nil || banners <= 0 {
		return false
	}
	member, _ := g.Member(p.ID)
	if member.JoinedAt > start {
		return false
	}
	o := s.openOnslaught(cycle)
	entry := o.entry(g.ID)
	if entry == nil {
		entry = &OnslaughtEntry{GuildID: g.ID, Members: map[string]int{}, Names: map[string]string{}}
		o.Entries = append(o.Entries, entry)
	}
	entry.Name, entry.Icon = g.Name, g.Icon
	entry.Members[p.ID] += banners
	entry.Names[p.ID] = member.Name
	return true
}

func (o *Onslaught) standings() []Standing {
	standings := make([]Standing, 0, len(o.Entries))
	for _, entry := range o.Entries {
		standings = append(standings, Standing{
			GuildID: entry.GuildID, Name: entry.Name, Icon: entry.Icon, Banners: entry.Banners(),
		})
	}
	slices.SortStableFunc(standings, func(a, b Standing) int {
		if order := cmp.Compare(b.Banners, a.Banners); order != 0 {
			return order
		}
		return cmp.Compare(a.Name, b.Name)
	})
	for i := range standings {
		standings[i].Rank = i + 1
	}
	return standings
}

func (s *State) OnslaughtStandings(cycle int64) []Standing {
	o := s.onslaught(cycle)
	if o == nil {
		return []Standing{}
	}
	return o.standings()
}

func (s *State) OnslaughtGuild(playerID string, cycle int64) (Standing, bool) {
	o := s.onslaught(cycle)
	if o == nil {
		return Standing{}, false
	}
	entry := o.contributor(playerID)
	if entry == nil {
		if mine := s.GuildOf(playerID); mine != nil {
			entry = o.entry(mine.ID)
		}
	}
	if entry == nil {
		return Standing{}, false
	}
	for _, standing := range o.standings() {
		if standing.GuildID == entry.GuildID {
			return standing, true
		}
	}
	return Standing{}, false
}

func (s *State) OnslaughtMembers(playerID string, cycle int64) []Contribution {
	contributions := []Contribution{}
	var entry *OnslaughtEntry
	if o := s.onslaught(cycle); o != nil {
		entry = o.contributor(playerID)
		if entry == nil {
			if mine := s.GuildOf(playerID); mine != nil {
				entry = o.entry(mine.ID)
			}
		}
	}
	listed := map[string]bool{}
	if mine := s.GuildOf(playerID); mine != nil && (entry == nil || entry.GuildID == mine.ID) {
		for _, m := range mine.Members {
			banners := 0
			if entry != nil {
				banners = entry.Members[m.PlayerID]
			}
			contributions = append(contributions, Contribution{PlayerID: m.PlayerID, Name: m.Name, Banners: banners})
			listed[m.PlayerID] = true
		}
	}
	if entry != nil {
		for id, banners := range entry.Members {
			if !listed[id] {
				contributions = append(contributions, Contribution{PlayerID: id, Name: entry.Names[id], Banners: banners})
			}
		}
	}
	slices.SortStableFunc(contributions, func(a, b Contribution) int {
		if order := cmp.Compare(b.Banners, a.Banners); order != 0 {
			return order
		}
		return cmp.Compare(a.Name, b.Name)
	})
	return contributions
}

func (s *State) OnslaughtBanners(playerID string, cycle int64) int {
	o := s.onslaught(cycle)
	if o == nil {
		return 0
	}
	if entry := o.contributor(playerID); entry != nil {
		return entry.Members[playerID]
	}
	return 0
}

func (s *State) OnslaughtClaimed(playerID string, cycle int64) bool {
	o := s.onslaught(cycle)
	return o != nil && slices.Contains(o.Claimed, playerID)
}

func (s *State) ClaimOnslaught(playerID string, cycle int64) bool {
	o := s.onslaught(cycle)
	if o == nil || o.contributor(playerID) == nil || slices.Contains(o.Claimed, playerID) {
		return false
	}
	o.Claimed = append(o.Claimed, playerID)
	return true
}
