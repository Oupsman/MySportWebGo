package vars

import (
	"fmt"
	"strconv"
	"strings"
)

// DefaultImportMaxSize is the largest zip of an export a user can import, when IMPORT_MAX_SIZE is unset
const DefaultImportMaxSize = 2 << 30

// ImportMaxSize is the largest zip of an export a user can import, in bytes: IMPORT_MAX_SIZE. The reverse
// proxy in front of the backend must accept requests as large (client_max_body_size of nginx).
var ImportMaxSize int64 = DefaultImportMaxSize

// DefaultUserQuota is the storage quota of a user without one of their own, when USER_QUOTA is unset
const DefaultUserQuota = 1 << 30

// UserQuota is the storage quota of the users without one of their own (set by an admin), in bytes: the
// files of their activities, thumbnails and pictures. 0: no quota. USER_QUOTA, DefaultUserQuota without.
var UserQuota int64 = DefaultUserQuota

// ParseSize reads a size: a number of bytes, or followed by k, m, g or t (powers of 1024, an optional b,
// any case): "1g", "500MB", "2T"; "none" or "0" for no limit (0)
func ParseSize(text string) (int64, error) {
	s := strings.ToLower(strings.TrimSpace(text))
	if s == "none" {
		return 0, nil
	}
	s = strings.TrimSuffix(s, "b")
	multiplier := int64(1)
	if s != "" {
		switch s[len(s)-1] {
		case 'k':
			multiplier = 1 << 10
		case 'm':
			multiplier = 1 << 20
		case 'g':
			multiplier = 1 << 30
		case 't':
			multiplier = 1 << 40
		}
		if multiplier > 1 {
			s = s[:len(s)-1]
		}
	}
	n, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil || n < 0 || n*float64(multiplier) > 1<<62 {
		return 0, fmt.Errorf("%q is not a size: a number followed by k, m, g or t (1g, 500m), or none", text)
	}
	return int64(n * float64(multiplier)), nil
}
