package battle

import "testing"

func TestTierStatsGrowEvenlyToTheCapAndOnThroughAscension(t *testing.T) {
	st := tierStats{basePower: 0, baseHealth: 100, maxPower: 10, maxHealth: 130, maxLevel: 4, catalogMaxLevel: 4}
	wantPower, wantHealth := []int{0, 3, 7, 10}, []int{100, 110, 120, 130}
	for level := 1; level <= 4; level++ {
		if got := st.powerAt(level); got != wantPower[level-1] {
			t.Errorf("power at level %d = %d, want %d", level, got, wantPower[level-1])
		}
		if got := st.healthAt(level); got != wantHealth[level-1] {
			t.Errorf("health at level %d = %d, want %d", level, got, wantHealth[level-1])
		}
	}
	st.maxLevel += ascensionBonusLevels
	if power, health := st.powerAt(st.maxLevel), st.healthAt(st.maxLevel); power != 27 || health != 180 {
		t.Errorf("ascended cap stats = %d/%d, want 27/180", power, health)
	}
}

func TestATierWithNoGrowthStaysAtItsBase(t *testing.T) {
	st := tierStats{basePower: 40, baseHealth: 400, maxLevel: 1, catalogMaxLevel: 1}
	if power, health := st.powerAt(1), st.healthAt(1); power != 40 || health != 400 {
		t.Errorf("stats = %d/%d, want 40/400", power, health)
	}
}
