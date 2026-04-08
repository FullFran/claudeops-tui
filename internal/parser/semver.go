package parser

import (
	"strconv"
	"strings"
)

// semverLE reports a <= b for "X.Y.Z" strings. Missing parts are treated as 0.
func semverLE(a, b string) bool {
	return semverCompare(a, b) <= 0
}

func semverLT(a, b string) bool {
	return semverCompare(a, b) < 0
}

func semverCompare(a, b string) int {
	pa := splitVersion(a)
	pb := splitVersion(b)
	for i := 0; i < 3; i++ {
		if pa[i] < pb[i] {
			return -1
		}
		if pa[i] > pb[i] {
			return 1
		}
	}
	return 0
}

func splitVersion(s string) [3]int {
	parts := strings.SplitN(s, ".", 3)
	var out [3]int
	for i := 0; i < len(parts) && i < 3; i++ {
		// strip non-digit suffix (e.g. "2.1.96-rc1" → "96")
		num := parts[i]
		for j, r := range num {
			if r < '0' || r > '9' {
				num = num[:j]
				break
			}
		}
		out[i], _ = strconv.Atoi(num)
	}
	return out
}
