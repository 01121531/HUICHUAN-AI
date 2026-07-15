package systemupdate

import (
	"regexp"
	"strconv"
	"strings"
)

var semanticVersionPattern = regexp.MustCompile(`^v?(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(?:-([0-9A-Za-z.-]+))?(?:\+[0-9A-Za-z.-]+)?$`)

type semanticVersion struct {
	major      int64
	minor      int64
	patch      int64
	prerelease string
}

func isSemanticVersion(value string) bool {
	_, ok := parseSemanticVersion(value)
	return ok
}

func parseSemanticVersion(value string) (semanticVersion, bool) {
	matches := semanticVersionPattern.FindStringSubmatch(strings.TrimSpace(value))
	if matches == nil {
		return semanticVersion{}, false
	}
	major, err1 := strconv.ParseInt(matches[1], 10, 64)
	minor, err2 := strconv.ParseInt(matches[2], 10, 64)
	patch, err3 := strconv.ParseInt(matches[3], 10, 64)
	if err1 != nil || err2 != nil || err3 != nil {
		return semanticVersion{}, false
	}
	return semanticVersion{major: major, minor: minor, patch: patch, prerelease: matches[4]}, true
}

func compareVersions(left string, right string) int {
	l, lok := parseSemanticVersion(left)
	r, rok := parseSemanticVersion(right)
	if !lok {
		return 0
	}
	if !rok {
		return 1
	}
	for _, pair := range [][2]int64{{l.major, r.major}, {l.minor, r.minor}, {l.patch, r.patch}} {
		if pair[0] > pair[1] {
			return 1
		}
		if pair[0] < pair[1] {
			return -1
		}
	}
	if l.prerelease == r.prerelease {
		return 0
	}
	if l.prerelease == "" {
		return 1
	}
	if r.prerelease == "" {
		return -1
	}
	return strings.Compare(l.prerelease, r.prerelease)
}
