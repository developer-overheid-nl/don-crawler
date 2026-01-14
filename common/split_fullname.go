//revive:disable:var-naming
package common

//revive:enable:var-naming

import (
	"strings"
)

// SplitFullName split a git FullName format to vendor and repo strings.
// Supports nested namespaces (e.g. group/subgroup/repo).
func SplitFullName(fullName string) (string, string) {
	parts := strings.Split(fullName, "/")

	if len(parts) == 0 {
		return "", ""
	}

	if len(parts) == 1 {
		return "", parts[0]
	}

	return strings.Join(parts[:len(parts)-1], "/"), parts[len(parts)-1]
}
