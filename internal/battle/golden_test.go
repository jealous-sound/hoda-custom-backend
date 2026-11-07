package battle

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

const goldenPath = "testdata/battle_replays.json"

var (
	testDataOnce sync.Once
	testData     *Data
	testDataErr  error
)

func loadTestData(t *testing.T) *Data {
	t.Helper()
	testDataOnce.Do(func() {
		testData, testDataErr = Load(func(name string) ([]byte, error) {
			return os.ReadFile(filepath.Join("..", "catalog", "data", name))
		})
	})
	if testDataErr != nil {
		t.Fatal(testDataErr)
	}
	return testData
}

type goldenBattle struct {
	Name string `json:"name"`
	Replay
	Outcome
}

type goldenRoster struct {
	Map   string `json:"map"`
	Step  int    `json:"step"`
	Enemy Side   `json:"enemy"`
}

type goldenOpponent struct {
	Kind             string      `json:"kind"`
	Level            int         `json:"level"`
	Trophies         int         `json:"trophies"`
	Now              int64       `json:"now"`
	Seed             uint32      `json:"seed"`
	Banners          int         `json:"banners"`
	Bot              int         `json:"bot"`
	Squad            int         `json:"squad"`
	OpponentTrophies int         `json:"opponent_trophies"`
	Opponent         PvpOpponent `json:"opponent"`
	Enemy            Side        `json:"enemy"`
}

type goldenCorpus struct {
	Version         int            `json:"version"`
	Battles         []goldenBattle `json:"battles"`
	CampaignRosters []goldenRoster `json:"campaign_rosters"`
	NexusRosters    []goldenRoster `json:"nexus_rosters"`
	NexusChangers   []struct {
		Event   string   `json:"event"`
		Changer *Changer `json:"changer"`
	} `json:"nexus_changers"`
	PvpChangers []struct {
		Cycle   int64    `json:"cycle"`
		Changer *Changer `json:"changer"`
	} `json:"pvp_changers"`
	PvpOpponents []goldenOpponent `json:"pvp_opponents"`
}

func loadCorpus(t *testing.T) goldenCorpus {
	t.Helper()
	data, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatal(err)
	}
	var corpus goldenCorpus
	if err := json.Unmarshal(data, &corpus); err != nil {
		t.Fatal(err)
	}
	if corpus.Version != 1 || len(corpus.Battles) == 0 {
		t.Fatalf("corpus version %d with %d battles", corpus.Version, len(corpus.Battles))
	}
	return corpus
}

func sameJSON(t *testing.T, label string, want, got any) {
	t.Helper()
	wantJSON, _ := json.Marshal(want)
	gotJSON, _ := json.Marshal(got)
	if !bytes.Equal(wantJSON, gotJSON) {
		t.Errorf("%s:\nwant %s\ngot  %s", label, wantJSON, gotJSON)
	}
}

func TestRecordedBattlesReplayIdentically(t *testing.T) {
	d := loadTestData(t)
	corpus := loadCorpus(t)
	wins, crits := 0, 0
	for _, battle := range corpus.Battles {
		outcome, err := d.Run(battle.Replay)
		if err != nil {
			t.Errorf("%s: %v", battle.Name, err)
			continue
		}
		if outcome != battle.Outcome {
			t.Errorf("%s: want %+v, got %+v", battle.Name, battle.Outcome, outcome)
		}
		if outcome.Won {
			wins++
		}
		crits += outcome.Crits
	}
	if wins == 0 || wins == len(corpus.Battles) || crits == 0 {
		t.Errorf("corpus is one-sided: %d wins of %d, %d crits", wins, len(corpus.Battles), crits)
	}
}

func TestRecordedRostersMatchTheClient(t *testing.T) {
	d := loadTestData(t)
	corpus := loadCorpus(t)
	for _, golden := range corpus.CampaignRosters {
		roster, ok := d.CampaignRoster(golden.Map, golden.Step)
		if !ok {
			t.Errorf("no campaign roster %s/%d", golden.Map, golden.Step)
			continue
		}
		sameJSON(t, golden.Map, golden.Enemy, roster)
	}
	for _, golden := range corpus.NexusRosters {
		roster, ok := d.NexusRoster(golden.Map, golden.Step)
		if !ok {
			t.Errorf("no nexus roster %s/%d", golden.Map, golden.Step)
			continue
		}
		sameJSON(t, golden.Map, golden.Enemy, roster)
	}
	for _, golden := range corpus.NexusChangers {
		sameJSON(t, golden.Event, golden.Changer, d.NexusChanger(golden.Event))
	}
	for _, golden := range corpus.PvpChangers {
		sameJSON(t, "pvp cycle", golden.Changer, d.PvpEventChanger(golden.Cycle))
	}
}

func TestRecordedPvpOpponentsMatchTheClient(t *testing.T) {
	d := loadTestData(t)
	corpus := loadCorpus(t)
	kinds := map[string]bool{}
	for _, golden := range corpus.PvpOpponents {
		kinds[golden.Kind] = true
		var opponent PvpOpponent
		switch golden.Kind {
		case "ladder":
			opponent = d.PvpLadderOpponent(golden.Level, golden.Trophies, golden.Now/SecondsPerDay, golden.Seed)
		case "event":
			opponent = d.PvpEventOpponent(golden.Level, golden.Trophies, golden.Banners, golden.Now, golden.Seed)
		case "bot":
			opponent = d.PvpOpponentFromBot(golden.Bot, golden.Level, golden.Trophies, golden.Now/SecondsPerDay,
				golden.Seed)
		case "revenge":
			opponent = PvpOpponent{
				Bot: golden.Bot, Squad: golden.Squad, Level: golden.Level, Trophies: golden.OpponentTrophies,
				Seed: golden.Seed,
			}
		default:
			t.Fatalf("unknown opponent kind %s", golden.Kind)
		}
		want := golden.Opponent
		want.Seed = opponent.Seed
		if opponent != want {
			t.Errorf("%s opponent: want %+v, got %+v", golden.Kind, want, opponent)
			continue
		}
		roster, err := d.PvpRoster(opponent)
		if err != nil {
			t.Errorf("%s roster: %v", golden.Kind, err)
			continue
		}
		sameJSON(t, golden.Kind+" roster", golden.Enemy, roster)
	}
	if len(kinds) != 4 {
		t.Errorf("corpus covers opponent kinds %v", kinds)
	}
}

func TestCorpusMatchesClientCheckout(t *testing.T) {
	client := filepath.Join("..", "..", "..", "hoda-custom-client")
	if dir := os.Getenv("HOD_CLIENT_DIR"); dir != "" {
		client = dir
	}
	theirs, err := os.ReadFile(filepath.Join(client, "tests", "golden", "battle_replays.json"))
	if err != nil {
		t.Skipf("no client corpus at %s", client)
	}
	ours, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(ours, theirs) {
		t.Error("battle_replays.json differs from the client; run scripts/sync-client-data.sh")
	}
}
