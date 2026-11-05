package shop

import (
	"encoding/json"
)

type RefillKind string

const (
	RefillEnergy  RefillKind = "energy"
	RefillStamina RefillKind = "stamina"
)

func Refill(raw json.RawMessage, kind RefillKind) (json.RawMessage, error) {
	if kind != RefillEnergy && kind != RefillStamina {
		return nil, refuse(Unavailable, "nothing to refill called %q", kind)
	}
	doc, err := parseDocument(raw)
	if err != nil {
		return nil, refuse(BadProfile, "%v", err)
	}
	account, err := doc.section("account")
	if err != nil {
		return nil, refuse(BadProfile, "%v", err)
	}
	pool := string(kind)
	current, err := account.int64Field(pool)
	if err != nil {
		return nil, refuse(BadProfile, "%v", err)
	}
	maximum, err := account.int64Field(pool + "_max")
	if err != nil {
		return nil, refuse(BadProfile, "%v", err)
	}
	gems, err := account.int64Field("gems")
	if err != nil {
		return nil, refuse(BadProfile, "%v", err)
	}
	if current >= maximum {
		return nil, refuse(AlreadyFull, "%s is already full", pool)
	}
	if gems < RefillGemCost {
		return nil, refuse(CantAfford, "a refill costs %d gems", RefillGemCost)
	}
	account.setInt("gems", gems-RefillGemCost)
	account.setInt(pool, maximum)
	account.setInt(pool+"_regen_timer", 0)
	return doc.encode()
}
