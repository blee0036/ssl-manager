package version

import (
	"fmt"
	"strconv"
	"strings"
)

// Version represents a semantic version with Major, Minor, and Patch components.
type Version struct {
	Major int
	Minor int
	Patch int
}

// Parse parses a version string in "MAJOR.MINOR.PATCH" format.
// It returns an error if the input is empty, has wrong number of components,
// or contains non-numeric or negative values.
func Parse(s string) (Version, error) {
	if s == "" {
		return Version{}, fmt.Errorf("version string is empty")
	}

	parts := strings.Split(s, ".")
	if len(parts) != 3 {
		return Version{}, fmt.Errorf("invalid version format %q: expected MAJOR.MINOR.PATCH", s)
	}

	major, err := strconv.Atoi(parts[0])
	if err != nil {
		return Version{}, fmt.Errorf("invalid major version %q: %w", parts[0], err)
	}
	if major < 0 {
		return Version{}, fmt.Errorf("major version must be non-negative, got %d", major)
	}

	minor, err := strconv.Atoi(parts[1])
	if err != nil {
		return Version{}, fmt.Errorf("invalid minor version %q: %w", parts[1], err)
	}
	if minor < 0 {
		return Version{}, fmt.Errorf("minor version must be non-negative, got %d", minor)
	}

	patch, err := strconv.Atoi(parts[2])
	if err != nil {
		return Version{}, fmt.Errorf("invalid patch version %q: %w", parts[2], err)
	}
	if patch < 0 {
		return Version{}, fmt.Errorf("patch version must be non-negative, got %d", patch)
	}

	return Version{Major: major, Minor: minor, Patch: patch}, nil
}

// Compare compares two versions and returns:
//
//	-1 if a < b
//	 0 if a == b
//	+1 if a > b
//
// Comparison is performed sequentially: major → minor → patch.
func Compare(a, b Version) int {
	if a.Major != b.Major {
		if a.Major < b.Major {
			return -1
		}
		return 1
	}
	if a.Minor != b.Minor {
		if a.Minor < b.Minor {
			return -1
		}
		return 1
	}
	if a.Patch != b.Patch {
		if a.Patch < b.Patch {
			return -1
		}
		return 1
	}
	return 0
}

// IsNewer determines if the remote version string is newer than the local version string.
// Both strings must be in "MAJOR.MINOR.PATCH" format.
// Returns true if remote > local, false otherwise.
func IsNewer(local, remote string) (bool, error) {
	localVer, err := Parse(local)
	if err != nil {
		return false, fmt.Errorf("parsing local version: %w", err)
	}

	remoteVer, err := Parse(remote)
	if err != nil {
		return false, fmt.Errorf("parsing remote version: %w", err)
	}

	return Compare(remoteVer, localVer) == 1, nil
}
