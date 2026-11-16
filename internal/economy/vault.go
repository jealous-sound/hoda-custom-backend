package economy

import (
	"time"

	"github.com/jealous-sound/hoda-custom-backend/internal/catalog"
)

const (
	vaultSection      = "vault"
	vaultLocked       = "locked"
	vaultUnlocksAt    = "unlocks_at"
	vaultAccruedAt    = "accrued_at"
	vaultClaimedAt    = "claimed_at"
	vaultPending      = "pending_milligems"
	vaultInterestPaid = "interest_paid"
	vaultFeesPaid     = "fees_paid"
)

var vaultFields = []string{
	vaultLocked, vaultUnlocksAt, vaultAccruedAt, vaultClaimedAt, vaultPending, vaultInterestPaid, vaultFeesPaid,
}

type vault struct {
	Locked           int64
	UnlocksAt        int64
	AccruedAt        int64
	ClaimedAt        int64
	PendingMilligems int64
	InterestPaid     int64
	FeesPaid         int64
}

func (v *vault) fields() map[string]*int64 {
	return map[string]*int64{
		vaultLocked: &v.Locked, vaultUnlocksAt: &v.UnlocksAt, vaultAccruedAt: &v.AccruedAt,
		vaultClaimedAt: &v.ClaimedAt, vaultPending: &v.PendingMilligems, vaultInterestPaid: &v.InterestPaid,
		vaultFeesPaid: &v.FeesPaid,
	}
}

func readVault(doc document) (vault, error) {
	var v vault
	section, _ := doc.optionalSection(vaultSection)
	for name, field := range v.fields() {
		value, err := section.optionalInt64(name)
		if err != nil || value < 0 {
			return vault{}, refuse(BadProfile, "vault %s is not a count", name)
		}
		*field = value
	}
	return v, nil
}

func storeVault(doc document, v vault) error {
	section, err := doc.section(vaultSection)
	if err != nil {
		return refuse(BadProfile, "%v", err)
	}
	for name, field := range v.fields() {
		section.setInt(name, *field)
	}
	return nil
}

func (v *vault) settle(rules *catalog.VaultRules, now int64) {
	if v.Locked <= 0 {
		return
	}
	end := min(now, v.ClaimedAt+int64(rules.Unclaimed/time.Second))
	if end <= v.AccruedAt {
		return
	}
	earned := rules.MilligemsPerDay(v.Locked) * (end - v.AccruedAt) / int64(24*time.Hour/time.Second)
	v.PendingMilligems = saturatingAdd(v.PendingMilligems, earned)
	v.AccruedAt = end
}

func (v *vault) takeInterest() int64 {
	gems := v.PendingMilligems / catalog.MilligemsPerGem
	v.PendingMilligems %= catalog.MilligemsPerGem
	v.InterestPaid = saturatingAdd(v.InterestPaid, gems)
	return gems
}

type lockedVault struct {
	Locked int64 `json:"locked"`
}

func VaultTier(c *catalog.Catalog, locked int64) int {
	return int(c.Vault.Tier(locked))
}

type VaultRequest struct {
	Gems int64 `json:"gems"`
}

func (p *player) DepositInVault(request VaultRequest) (BattleResult, error) {
	rules := &p.catalog.Vault
	v, err := readVault(p.doc)
	if err != nil {
		return BattleResult{}, err
	}
	if !rules.DepositFits(v.Locked, request.Gems) {
		return BattleResult{}, refuse(Unavailable, "the vault cannot take %d gems", request.Gems)
	}
	if err := p.account.spendGems(request.Gems); err != nil {
		return BattleResult{}, err
	}
	now := p.now.Unix()
	if v.Locked == 0 {
		v.AccruedAt, v.ClaimedAt = now, now
	} else {
		v.settle(rules, now)
	}
	v.Locked += request.Gems
	v.UnlocksAt = p.now.Add(rules.Lock).Unix()
	return BattleResult{}, storeVault(p.doc, v)
}

func (p *player) WithdrawFromVault(request VaultRequest) (BattleResult, error) {
	rules := &p.catalog.Vault
	v, err := readVault(p.doc)
	if err != nil {
		return BattleResult{}, err
	}
	if request.Gems <= 0 || request.Gems > v.Locked {
		return BattleResult{}, refuse(Unavailable, "the vault holds %d gems", v.Locked)
	}
	now := p.now.Unix()
	v.settle(rules, now)
	returned := request.Gems
	if now < v.UnlocksAt {
		fee := rules.EarlyWithdrawFee(request.Gems)
		returned -= fee
		v.FeesPaid = saturatingAdd(v.FeesPaid, fee)
		v.PendingMilligems = 0
	}
	v.Locked -= request.Gems
	if v.Locked == 0 {
		returned = saturatingAdd(returned, v.takeInterest())
		v = vault{InterestPaid: v.InterestPaid, FeesPaid: v.FeesPaid}
	}
	p.addGems(returned)
	return BattleResult{Gems: returned}, storeVault(p.doc, v)
}

func (p *player) ClaimVaultInterest() (BattleResult, error) {
	v, err := readVault(p.doc)
	if err != nil {
		return BattleResult{}, err
	}
	now := p.now.Unix()
	v.settle(&p.catalog.Vault, now)
	gems := v.takeInterest()
	if gems <= 0 {
		return BattleResult{}, refuse(Unavailable, "no vault interest to collect")
	}
	v.AccruedAt, v.ClaimedAt = now, now
	p.addGems(gems)
	return BattleResult{Gems: gems}, storeVault(p.doc, v)
}
