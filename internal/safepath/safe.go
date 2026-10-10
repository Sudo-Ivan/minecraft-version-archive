package safepath

import "strings"

func SafeSegment(name string) bool {
	if name == "" || name == "." || name == ".." {
		return false
	}
	if strings.Contains(name, "..") {
		return false
	}
	return !strings.ContainsAny(name, `/\`)
}
