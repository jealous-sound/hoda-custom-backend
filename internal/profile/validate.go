package profile

import (
	"encoding/json"
	"fmt"
	"regexp"
	"unicode/utf8"
)

const (
	MaxProfileBytes     = 8 << 20
	MaxPlayerNameLength = 25
)

var playerNamePattern = regexp.MustCompile(`^[\p{L}\p{N} -]+$`)

func ValidPlayerName(name string) bool {
	return utf8.RuneCountInString(name) <= MaxPlayerNameLength && playerNamePattern.MatchString(name)
}

var requiredSections = []string{"account", "roster", "squads"}

var nonNegativeAccountFields = []string{
	"level", "experience", "energy", "energy_max", "stamina", "stamina_max", "gold", "gems",
}

func Validate(raw []byte) (json.RawMessage, error) {
	if len(raw) > MaxProfileBytes {
		return nil, fmt.Errorf("profile is %d bytes, limit is %d", len(raw), MaxProfileBytes)
	}
	var sections map[string]json.RawMessage
	if err := json.Unmarshal(raw, &sections); err != nil {
		return nil, fmt.Errorf("profile must be a JSON object: %w", err)
	}
	for _, name := range requiredSections {
		if _, ok := sections[name]; !ok {
			return nil, fmt.Errorf("profile is missing %q", name)
		}
	}
	var account map[string]json.RawMessage
	if err := json.Unmarshal(sections["account"], &account); err != nil {
		return nil, fmt.Errorf("account must be an object: %w", err)
	}
	for _, field := range nonNegativeAccountFields {
		value, ok := account[field]
		if !ok {
			return nil, fmt.Errorf("account is missing %q", field)
		}
		var number float64
		if err := json.Unmarshal(value, &number); err != nil {
			return nil, fmt.Errorf("account.%s must be a number", field)
		}
		if number < 0 {
			return nil, fmt.Errorf("account.%s must not be negative", field)
		}
	}
	if value, ok := account["name"]; ok {
		var name string
		if err := json.Unmarshal(value, &name); err != nil || !ValidPlayerName(name) {
			return nil, fmt.Errorf("account.name must be 1-%d letters, digits, spaces or '-'", MaxPlayerNameLength)
		}
	}
	return json.RawMessage(raw), nil
}
