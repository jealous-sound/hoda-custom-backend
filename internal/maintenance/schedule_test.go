package maintenance

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

var start = time.Unix(1_800_000_000, 0)

type clock struct {
	current time.Time
}

func (c *clock) now() time.Time {
	return c.current
}

func open(t *testing.T, dir string, c *clock) *Schedule {
	t.Helper()
	schedule, err := Open(dir, c.now)
	if err != nil {
		t.Fatal(err)
	}
	return schedule
}

func TestNothingIsScheduledAtFirst(t *testing.T) {
	schedule := open(t, t.TempDir(), &clock{current: start})
	if _, scheduled := schedule.At(); scheduled || schedule.HoldsBattles() {
		t.Fatal("a fresh schedule announces maintenance")
	}
}

func TestSetAnnouncesTheTimeToTheSecond(t *testing.T) {
	schedule := open(t, t.TempDir(), &clock{current: start})
	if err := schedule.Set(start.Add(30*time.Minute + 400*time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	at, scheduled := schedule.At()
	if !scheduled || !at.Equal(start.Add(30*time.Minute)) {
		t.Fatalf("At() = %v, %v", at, scheduled)
	}
}

func TestSetRefusesATimeThatHasPassed(t *testing.T) {
	schedule := open(t, t.TempDir(), &clock{current: start})
	for _, at := range []time.Time{start, start.Add(-time.Minute)} {
		if err := schedule.Set(at); !errors.Is(err, ErrNotAhead) {
			t.Fatalf("Set(%v) = %v, want ErrNotAhead", at, err)
		}
	}
	if _, scheduled := schedule.At(); scheduled {
		t.Fatal("a refused time was announced")
	}
}

func TestBattlesAreHeldFromTheLeadUntilTheOverrunEnds(t *testing.T) {
	c := &clock{current: start}
	schedule := open(t, t.TempDir(), c)
	at := start.Add(time.Hour)
	if err := schedule.Set(at); err != nil {
		t.Fatal(err)
	}
	for _, moment := range []struct {
		when time.Time
		held bool
	}{
		{start, false},
		{at.Add(-BattleLead - time.Second), false},
		{at.Add(-BattleLead), true},
		{at, true},
		{at.Add(Overrun - time.Second), true},
		{at.Add(Overrun), false},
	} {
		c.current = moment.when
		if held := schedule.HoldsBattles(); held != moment.held {
			t.Fatalf("HoldsBattles() at %v = %v, want %v", moment.when.Sub(at), held, moment.held)
		}
	}
}

func TestAnOverrunScheduleIsForgotten(t *testing.T) {
	c := &clock{current: start}
	dir := t.TempDir()
	schedule := open(t, dir, c)
	if err := schedule.Set(start.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	c.current = start.Add(time.Minute + Overrun)
	if _, scheduled := schedule.At(); scheduled {
		t.Fatal("maintenance is still announced after its overrun")
	}
	if _, err := os.Stat(filepath.Join(dir, fileName)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the schedule file outlived the overrun: %v", err)
	}
}

func TestAPendingScheduleSurvivesARestart(t *testing.T) {
	c := &clock{current: start}
	dir := t.TempDir()
	at := start.Add(time.Hour)
	if err := open(t, dir, c).Set(at); err != nil {
		t.Fatal(err)
	}
	c.current = start.Add(20 * time.Minute)
	reopened, scheduled := open(t, dir, c).At()
	if !scheduled || !reopened.Equal(at) {
		t.Fatalf("At() after reopening = %v, %v", reopened, scheduled)
	}
}

func TestARestartAfterTheAnnouncedTimeEndsTheMaintenance(t *testing.T) {
	c := &clock{current: start}
	dir := t.TempDir()
	if err := open(t, dir, c).Set(start.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	c.current = start.Add(time.Hour + time.Second)
	reopened := open(t, dir, c)
	if _, scheduled := reopened.At(); scheduled || reopened.HoldsBattles() {
		t.Fatal("the restart that was announced did not end the maintenance")
	}
	if _, err := os.Stat(filepath.Join(dir, fileName)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the schedule file outlived the restart: %v", err)
	}
}

func TestClearCancelsTheMaintenance(t *testing.T) {
	c := &clock{current: start}
	dir := t.TempDir()
	schedule := open(t, dir, c)
	if err := schedule.Set(start.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := schedule.Clear(); err != nil {
		t.Fatal(err)
	}
	if _, scheduled := schedule.At(); scheduled || schedule.HoldsBattles() {
		t.Fatal("cleared maintenance is still announced")
	}
	if _, scheduled := open(t, dir, c).At(); scheduled {
		t.Fatal("cleared maintenance came back after reopening")
	}
	if err := schedule.Clear(); err != nil {
		t.Fatalf("clearing twice: %v", err)
	}
}
