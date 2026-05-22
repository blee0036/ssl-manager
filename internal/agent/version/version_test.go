package version

import (
	"testing"
)

// --- Parse tests ---

func TestParse_ValidInputs(t *testing.T) {
	tests := []struct {
		input    string
		expected Version
	}{
		{"1.2.3", Version{Major: 1, Minor: 2, Patch: 3}},
		{"0.0.0", Version{Major: 0, Minor: 0, Patch: 0}},
		{"10.20.30", Version{Major: 10, Minor: 20, Patch: 30}},
		{"999.999.999", Version{Major: 999, Minor: 999, Patch: 999}},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := Parse(tt.input)
			if err != nil {
				t.Fatalf("Parse(%q) returned unexpected error: %v", tt.input, err)
			}
			if got != tt.expected {
				t.Errorf("Parse(%q) = %+v, want %+v", tt.input, got, tt.expected)
			}
		})
	}
}

func TestParse_InvalidInputs(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{"empty string", ""},
		{"two components", "1.2"},
		{"four components", "1.2.3.4"},
		{"non-numeric", "a.b.c"},
		{"negative major", "-1.0.0"},
		{"prerelease suffix", "1.2.3-beta"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Parse(tt.input)
			if err == nil {
				t.Errorf("Parse(%q) expected error, got nil", tt.input)
			}
		})
	}
}

// --- Compare tests ---

func TestCompare_EqualVersions(t *testing.T) {
	a := Version{Major: 1, Minor: 2, Patch: 3}
	b := Version{Major: 1, Minor: 2, Patch: 3}
	if got := Compare(a, b); got != 0 {
		t.Errorf("Compare(%+v, %+v) = %d, want 0", a, b, got)
	}
}

func TestCompare_DifferentMajor(t *testing.T) {
	tests := []struct {
		name     string
		a, b     Version
		expected int
	}{
		{"a.Major > b.Major", Version{2, 0, 0}, Version{1, 9, 9}, 1},
		{"a.Major < b.Major", Version{1, 9, 9}, Version{2, 0, 0}, -1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Compare(tt.a, tt.b); got != tt.expected {
				t.Errorf("Compare(%+v, %+v) = %d, want %d", tt.a, tt.b, got, tt.expected)
			}
		})
	}
}

func TestCompare_DifferentMinor(t *testing.T) {
	tests := []struct {
		name     string
		a, b     Version
		expected int
	}{
		{"a.Minor > b.Minor", Version{1, 3, 0}, Version{1, 2, 9}, 1},
		{"a.Minor < b.Minor", Version{1, 2, 9}, Version{1, 3, 0}, -1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Compare(tt.a, tt.b); got != tt.expected {
				t.Errorf("Compare(%+v, %+v) = %d, want %d", tt.a, tt.b, got, tt.expected)
			}
		})
	}
}

func TestCompare_DifferentPatch(t *testing.T) {
	tests := []struct {
		name     string
		a, b     Version
		expected int
	}{
		{"a.Patch > b.Patch", Version{1, 2, 4}, Version{1, 2, 3}, 1},
		{"a.Patch < b.Patch", Version{1, 2, 3}, Version{1, 2, 4}, -1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Compare(tt.a, tt.b); got != tt.expected {
				t.Errorf("Compare(%+v, %+v) = %d, want %d", tt.a, tt.b, got, tt.expected)
			}
		})
	}
}

// --- IsNewer tests ---

func TestIsNewer_RemoteIsNewer(t *testing.T) {
	got, err := IsNewer("1.0.0", "1.0.1")
	if err != nil {
		t.Fatalf("IsNewer returned unexpected error: %v", err)
	}
	if !got {
		t.Error("IsNewer(\"1.0.0\", \"1.0.1\") = false, want true")
	}
}

func TestIsNewer_RemoteIsOlder(t *testing.T) {
	got, err := IsNewer("2.0.0", "1.9.9")
	if err != nil {
		t.Fatalf("IsNewer returned unexpected error: %v", err)
	}
	if got {
		t.Error("IsNewer(\"2.0.0\", \"1.9.9\") = true, want false")
	}
}

func TestIsNewer_EqualVersions(t *testing.T) {
	got, err := IsNewer("1.2.3", "1.2.3")
	if err != nil {
		t.Fatalf("IsNewer returned unexpected error: %v", err)
	}
	if got {
		t.Error("IsNewer(\"1.2.3\", \"1.2.3\") = true, want false")
	}
}

func TestIsNewer_InvalidLocal(t *testing.T) {
	_, err := IsNewer("invalid", "1.0.0")
	if err == nil {
		t.Error("IsNewer with invalid local version expected error, got nil")
	}
}

func TestIsNewer_InvalidRemote(t *testing.T) {
	_, err := IsNewer("1.0.0", "bad")
	if err == nil {
		t.Error("IsNewer with invalid remote version expected error, got nil")
	}
}
