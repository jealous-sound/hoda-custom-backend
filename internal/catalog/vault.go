package catalog

import (
	"fmt"
	"time"
)

const (
	VaultFile      = "vault.json"
	supportedVault = 1

	MilligemsPerGem     = 1000
	basisPointsPerWhole = 10000
	wholePercent        = 100
	vaultDay            = 24 * time.Hour
)

type VaultTier int

const (
	VaultTierNone VaultTier = iota
	VaultTierBronze
	VaultTierSilver
	VaultTierGold
)

var VaultTierIDs = map[VaultTier]string{VaultTierBronze: "bronze", VaultTierSilver: "silver", VaultTierGold: "gold"}

type VaultBracket struct {
	UpTo              int64
	BasisPointsPerDay int64
}

type VaultRules struct {
	Lock                time.Duration
	EarlyWithdrawFeePct int64
	Unclaimed           time.Duration
	MinimumDeposit      int64
	Brackets            []VaultBracket
	TierGems            map[VaultTier]int64
}

func (v VaultRules) Capacity() int64 {
	if len(v.Brackets) == 0 {
		return 0
	}
	return v.Brackets[len(v.Brackets)-1].UpTo
}

func (v VaultRules) MilligemsPerDay(locked int64) int64 {
	var total, floor int64
	for _, bracket := range v.Brackets {
		if locked <= floor {
			break
		}
		slice := min(locked, bracket.UpTo) - floor
		total += slice * bracket.BasisPointsPerDay * MilligemsPerGem / basisPointsPerWhole
		floor = bracket.UpTo
	}
	return total
}

func (v VaultRules) Tier(locked int64) VaultTier {
	tier := VaultTierNone
	for candidate := VaultTierBronze; candidate <= VaultTierGold; candidate++ {
		if threshold, listed := v.TierGems[candidate]; listed && locked >= threshold {
			tier = candidate
		}
	}
	return tier
}

func (v VaultRules) EarlyWithdrawFee(gems int64) int64 {
	if gems <= 0 {
		return 0
	}
	return (gems*v.EarlyWithdrawFeePct + wholePercent - 1) / wholePercent
}

func (v VaultRules) DepositFits(locked, gems int64) bool {
	room := v.Capacity() - locked
	return gems > 0 && gems <= room && (gems >= v.MinimumDeposit || gems == room)
}

func (c *Catalog) loadVault(read func(name string) ([]byte, error)) error {
	var raw struct {
		Version             int   `json:"version"`
		LockDays            int64 `json:"lock_days"`
		EarlyWithdrawFeePct int64 `json:"early_withdraw_fee_pct"`
		UnclaimedDays       int64 `json:"unclaimed_days"`
		MinimumDeposit      int64 `json:"minimum_deposit"`
		Brackets            []struct {
			UpTo              int64 `json:"up_to"`
			BasisPointsPerDay int64 `json:"basis_points_per_day"`
		} `json:"brackets"`
		TierGems map[string]int64 `json:"tier_gems"`
	}
	if err := decodeFile(read, VaultFile, &raw); err != nil {
		return err
	}
	if raw.Version != supportedVault {
		return fmt.Errorf("%s version %d, want %d", VaultFile, raw.Version, supportedVault)
	}
	if raw.LockDays <= 0 || raw.UnclaimedDays <= 0 || raw.MinimumDeposit <= 0 {
		return fmt.Errorf("%s needs positive lock_days, unclaimed_days and minimum_deposit", VaultFile)
	}
	if raw.EarlyWithdrawFeePct < 0 || raw.EarlyWithdrawFeePct > wholePercent {
		return fmt.Errorf("%s early_withdraw_fee_pct %d is not a percentage", VaultFile, raw.EarlyWithdrawFeePct)
	}
	if len(raw.Brackets) == 0 {
		return fmt.Errorf("%s has no brackets", VaultFile)
	}
	rules := VaultRules{
		Lock: time.Duration(raw.LockDays) * vaultDay, EarlyWithdrawFeePct: raw.EarlyWithdrawFeePct,
		Unclaimed: time.Duration(raw.UnclaimedDays) * vaultDay, MinimumDeposit: raw.MinimumDeposit,
		TierGems: map[VaultTier]int64{},
	}
	var floor int64
	for index, bracket := range raw.Brackets {
		if bracket.UpTo <= floor || bracket.BasisPointsPerDay <= 0 {
			return fmt.Errorf("%s bracket %d needs a rising up_to and a positive rate", VaultFile, index)
		}
		rules.Brackets = append(rules.Brackets, VaultBracket{UpTo: bracket.UpTo, BasisPointsPerDay: bracket.BasisPointsPerDay})
		floor = bracket.UpTo
	}
	var previous int64
	for tier := VaultTierBronze; tier <= VaultTierGold; tier++ {
		gems, listed := raw.TierGems[VaultTierIDs[tier]]
		if !listed || gems <= previous || gems > rules.Capacity() {
			return fmt.Errorf("%s tier %s needs gems above the tier below and within the capacity", VaultFile,
				VaultTierIDs[tier])
		}
		rules.TierGems[tier] = gems
		previous = gems
	}
	if len(raw.TierGems) != len(VaultTierIDs) {
		return fmt.Errorf("%s names a tier outside %v", VaultFile, VaultTierIDs)
	}
	c.Vault = rules
	return nil
}
