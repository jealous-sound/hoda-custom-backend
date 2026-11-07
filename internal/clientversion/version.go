package clientversion

import (
	"regexp"
	"strconv"
	"strings"
)

const UserAgentProduct = "HeroesOfDragonAge/"

const dirtySuffix = "-dirty"

var versionPattern = regexp.MustCompile(
	`^v?(\d+)\.(\d+)\.(\d+)(?:-([0-9A-Za-z]+(?:\.[0-9A-Za-z]+)*))?(?:-(\d+)-g[0-9a-f]+)?$`,
)

type Version struct {
	text       string
	core       [3]int
	prerelease []string
	ahead      int
}

func Parse(text string) (Version, bool) {
	match := versionPattern.FindStringSubmatch(strings.TrimSuffix(text, dirtySuffix))
	if match == nil {
		return Version{}, false
	}
	version := Version{text: text}
	for i := range version.core {
		number, err := strconv.Atoi(match[i+1])
		if err != nil {
			return Version{}, false
		}
		version.core[i] = number
	}
	if match[4] != "" {
		version.prerelease = strings.Split(match[4], ".")
	}
	if match[5] != "" {
		ahead, err := strconv.Atoi(match[5])
		if err != nil {
			return Version{}, false
		}
		version.ahead = ahead
	}
	return version, true
}

func FromUserAgent(header string) (Version, bool) {
	for _, product := range strings.Fields(header) {
		if text, found := strings.CutPrefix(product, UserAgentProduct); found {
			return Parse(text)
		}
	}
	return Version{}, false
}

func (v Version) Known() bool {
	return v.text != ""
}

func (v Version) String() string {
	return v.text
}

func (v Version) Compare(other Version) int {
	for i := range v.core {
		if order := compareInts(v.core[i], other.core[i]); order != 0 {
			return order
		}
	}
	if order := comparePrerelease(v.prerelease, other.prerelease); order != 0 {
		return order
	}
	return compareInts(v.ahead, other.ahead)
}

func (v Version) Older(other Version) bool {
	return v.Compare(other) < 0
}

func Newest(a, b Version) Version {
	if !a.Known() || (b.Known() && a.Older(b)) {
		return b
	}
	return a
}

func comparePrerelease(a, b []string) int {
	switch {
	case len(a) == 0 && len(b) == 0:
		return 0
	case len(a) == 0:
		return 1
	case len(b) == 0:
		return -1
	}
	for i := 0; i < len(a) && i < len(b); i++ {
		if order := compareIdentifiers(a[i], b[i]); order != 0 {
			return order
		}
	}
	return compareInts(len(a), len(b))
}

func compareIdentifiers(a, b string) int {
	aNumber, aErr := strconv.Atoi(a)
	bNumber, bErr := strconv.Atoi(b)
	switch {
	case aErr == nil && bErr == nil:
		return compareInts(aNumber, bNumber)
	case aErr == nil:
		return -1
	case bErr == nil:
		return 1
	}
	return strings.Compare(a, b)
}

func compareInts(a, b int) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}
