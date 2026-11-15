package economy

import (
	"encoding/json"
	"strconv"
	"testing"
	"time"

	"github.com/jealous-sound/hoda-custom-backend/internal/catalog"
	"github.com/jealous-sound/hoda-custom-backend/internal/profile"
)

const (
	vaultWallet       = 20000
	silverDeposit     = 6200
	silverMilligems   = 16200
	vaultDayLength    = 24 * time.Hour
	shippedLockDays   = 14
	shippedFeePct     = 10
	shippedWindowDays = 7
)

func vaultBody(gems int64) string {
	return `{"gems":` + strconv.FormatInt(gems, 10) + `}`
}

func vaultAt(t *testing.T, raw json.RawMessage, at time.Time, action, body string) (json.RawMessage, BattleResult, error) {
	t.Helper()
	return Act(mustCatalog(t), raw, &profile.ServerState{}, action, []byte(body), at)
}

func mustVaultAt(t *testing.T, raw json.RawMessage, at time.Time, action, body string) (json.RawMessage, BattleResult) {
	t.Helper()
	next, result, err := vaultAt(t, raw, at, action, body)
	if err != nil {
		t.Fatalf("%s %s: %v", action, body, err)
	}
	return next, result
}

func vaultOf(t *testing.T, raw json.RawMessage) vault {
	t.Helper()
	doc, err := parseDocument(raw)
	if err != nil {
		t.Fatal(err)
	}
	v, err := readVault(doc)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func walletProfile(t *testing.T) json.RawMessage {
	return edited(t, withAccount(map[string]any{"gems": vaultWallet}))
}

func depositedAt(t *testing.T, at time.Time, gems int64) json.RawMessage {
	t.Helper()
	raw, _ := mustVaultAt(t, walletProfile(t), at, ActionVaultDeposit, vaultBody(gems))
	return raw
}

func TestShippedVaultTableLoads(t *testing.T) {
	rules := mustCatalog(t).Vault
	if rules.Lock != shippedLockDays*vaultDayLength || rules.Unclaimed != shippedWindowDays*vaultDayLength ||
		rules.EarlyWithdrawFeePct != shippedFeePct {
		t.Errorf("rules = %+v", rules)
	}
	for locked, want := range map[int64]int64{
		0: 0, 1000: 5000, 5000: 15000, silverDeposit: silverMilligems, 15000: 25000, 50000: 25000,
	} {
		if got := rules.MilligemsPerDay(locked); got != want {
			t.Errorf("%d locked earns %d milligems a day, want %d", locked, got, want)
		}
	}
	for locked, want := range map[int64]catalog.VaultTier{
		0: catalog.VaultTierNone, 999: catalog.VaultTierNone, 1000: catalog.VaultTierBronze,
		silverDeposit: catalog.VaultTierSilver, 15000: catalog.VaultTierGold,
	} {
		if got := rules.Tier(locked); got != want {
			t.Errorf("%d locked is tier %d, want %d", locked, got, want)
		}
	}
}

func TestDepositLocksGemsAndStartsInterest(t *testing.T) {
	raw := depositedAt(t, ruleNow, silverDeposit)
	v := vaultOf(t, raw)
	if v.Locked != silverDeposit || v.UnlocksAt != ruleNow.Add(shippedLockDays*vaultDayLength).Unix() ||
		v.AccruedAt != ruleNow.Unix() || v.ClaimedAt != ruleNow.Unix() || v.PendingMilligems != 0 {
		t.Errorf("vault = %+v", v)
	}
	if gems := accountOf(t, raw).Gems; gems != vaultWallet-silverDeposit {
		t.Errorf("wallet holds %d gems", gems)
	}
}

func TestDepositsTheVaultCannotTakeAreRefused(t *testing.T) {
	capacity := mustCatalog(t).Vault.Capacity()
	nearlyFull := depositedAt(t, ruleNow, capacity-50)
	cases := []struct {
		name string
		raw  json.RawMessage
		gems int64
		want RefusalCode
	}{
		{"nothing", walletProfile(t), 0, Unavailable},
		{"below the minimum", walletProfile(t), 99, Unavailable},
		{"past the capacity", walletProfile(t), capacity + 1, Unavailable},
		{"more than the wallet", gameProfile(t, nil), 1000, CantAfford},
		{"a small top-up that does not fill the vault", nearlyFull, 49, Unavailable},
	}
	for _, c := range cases {
		if _, _, err := vaultAt(t, c.raw, ruleNow, ActionVaultDeposit, vaultBody(c.gems)); refusalCode(err) != c.want {
			t.Errorf("%s: %v, want %s", c.name, err, c.want)
		}
	}
	filled, _ := mustVaultAt(t, nearlyFull, ruleNow, ActionVaultDeposit, vaultBody(50))
	if v := vaultOf(t, filled); v.Locked != capacity {
		t.Errorf("filling the last room locked %d", v.Locked)
	}
}

func TestInterestIsCollectedInWholeGemsAndKeepsTheRemainder(t *testing.T) {
	raw := depositedAt(t, ruleNow, silverDeposit)
	day := ruleNow.Add(vaultDayLength)
	claimed, result := mustVaultAt(t, raw, day, ActionVaultClaim, `{}`)
	if result.Gems != silverMilligems/catalog.MilligemsPerGem {
		t.Errorf("collected %d gems", result.Gems)
	}
	v := vaultOf(t, claimed)
	if v.PendingMilligems != silverMilligems%catalog.MilligemsPerGem || v.InterestPaid != result.Gems ||
		v.ClaimedAt != day.Unix() {
		t.Errorf("vault = %+v", v)
	}
	if gems := accountOf(t, claimed).Gems; gems != vaultWallet-silverDeposit+result.Gems {
		t.Errorf("wallet holds %d gems", gems)
	}
}

func TestUncollectedInterestStopsAfterTheWindow(t *testing.T) {
	raw := depositedAt(t, ruleNow, silverDeposit)
	_, result := mustVaultAt(t, raw, ruleNow.Add(30*vaultDayLength), ActionVaultClaim, `{}`)
	if want := int64(silverMilligems * shippedWindowDays / catalog.MilligemsPerGem); result.Gems != want {
		t.Errorf("30 days away collected %d gems, want %d", result.Gems, want)
	}
}

func TestCollectingNothingIsRefused(t *testing.T) {
	raw := depositedAt(t, ruleNow, silverDeposit)
	if _, _, err := vaultAt(t, raw, ruleNow.Add(time.Minute), ActionVaultClaim, `{}`); refusalCode(err) != Unavailable {
		t.Errorf("an empty collection gave %v", err)
	}
	if _, _, err := vaultAt(t, walletProfile(t), ruleNow, ActionVaultClaim, `{}`); refusalCode(err) != Unavailable {
		t.Errorf("collecting from no vault gave %v", err)
	}
}

func TestTopUpRelocksAndSettlesTheOldBalanceFirst(t *testing.T) {
	raw := depositedAt(t, ruleNow, 1000)
	day := ruleNow.Add(vaultDayLength)
	topped, _ := mustVaultAt(t, raw, day, ActionVaultDeposit, vaultBody(4000))
	v := vaultOf(t, topped)
	if v.Locked != 5000 || v.PendingMilligems != 5000 || v.UnlocksAt != day.Add(shippedLockDays*vaultDayLength).Unix() {
		t.Errorf("vault = %+v", v)
	}
}

func TestEarlyWithdrawalPaysAFeeAndForfeitsInterest(t *testing.T) {
	raw := depositedAt(t, ruleNow, silverDeposit)
	early := ruleNow.Add(3 * vaultDayLength)
	next, result := mustVaultAt(t, raw, early, ActionVaultWithdraw, vaultBody(1001))
	fee := int64(101)
	if result.Gems != 1001-fee {
		t.Errorf("got back %d gems", result.Gems)
	}
	v := vaultOf(t, next)
	if v.Locked != silverDeposit-1001 || v.PendingMilligems != 0 || v.FeesPaid != fee || v.InterestPaid != 0 {
		t.Errorf("vault = %+v", v)
	}
	if gems := accountOf(t, next).Gems; gems != vaultWallet-silverDeposit+1001-fee {
		t.Errorf("wallet holds %d gems", gems)
	}
}

func TestEmptyingAnUnlockedVaultReturnsEverythingWithInterest(t *testing.T) {
	raw := depositedAt(t, ruleNow, silverDeposit)
	unlocked := ruleNow.Add(shippedLockDays * vaultDayLength)
	claimed, _ := mustVaultAt(t, raw, ruleNow.Add(shippedWindowDays*vaultDayLength), ActionVaultClaim, `{}`)
	part, result := mustVaultAt(t, claimed, unlocked, ActionVaultWithdraw, vaultBody(200))
	if result.Gems != 200 {
		t.Errorf("an unlocked partial withdrawal returned %d", result.Gems)
	}
	pending := vaultOf(t, part).PendingMilligems
	emptied, result := mustVaultAt(t, part, unlocked, ActionVaultWithdraw, vaultBody(silverDeposit-200))
	if want := silverDeposit - 200 + pending/catalog.MilligemsPerGem; result.Gems != want {
		t.Errorf("emptying returned %d, want %d", result.Gems, want)
	}
	v := vaultOf(t, emptied)
	if v.Locked != 0 || v.UnlocksAt != 0 || v.PendingMilligems != 0 || v.InterestPaid == 0 {
		t.Errorf("vault = %+v", v)
	}
	if _, _, err := vaultAt(t, emptied, unlocked, ActionVaultWithdraw, vaultBody(1)); refusalCode(err) != Unavailable {
		t.Errorf("withdrawing from an empty vault gave %v", err)
	}
}

func TestUploadCannotChangeTheVault(t *testing.T) {
	stored := depositedAt(t, ruleNow, silverDeposit)
	forged := asMap(t, stored)
	section(forged, vaultSection)[vaultLocked] = 15000
	section(forged, vaultSection)[vaultPending] = 9_000_000
	record := profile.Record{PlayerID: "local", Profile: stored}
	merged, err := MergeUpload(mustCatalog(t), record, &profile.ServerState{}, reencode(t, forged), ruleNow)
	if err != nil {
		t.Fatal(err)
	}
	if v := vaultOf(t, merged); v.Locked != silverDeposit || v.PendingMilligems != 0 {
		t.Errorf("upload changed the vault to %+v", v)
	}
	minted := edited(t, func(doc map[string]any) { doc[vaultSection] = map[string]any{vaultLocked: 15000} })
	fresh, err := MergeUpload(mustCatalog(t), profile.Record{PlayerID: "local", Profile: gameProfile(t, nil)},
		&profile.ServerState{}, minted, ruleNow)
	if err != nil {
		t.Fatal(err)
	}
	if v := vaultOf(t, fresh); v.Locked != 0 {
		t.Errorf("upload opened a vault holding %d", v.Locked)
	}
}
