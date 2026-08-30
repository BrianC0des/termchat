package system

import (
	"testing"
)

func TestCompareVersions(t *testing.T) {
	tests := []struct {
		v1       string
		v2       string
		expected int
	}{
		{"v2.1.1", "v2.1.1", 0},
		{"2.1.1", "v2.1.1", 0},
		{"v2.1.2", "v2.1.1", 1},
		{"v2.2.0", "v2.1.9", 1},
		{"v2.10.0", "v2.1.1", 1},
		{"v3.0.0", "v2.9.9", 1},
		{"v2.1.0", "v2.1.1", -1},
		{"v1.9.9", "v2.0.0", -1},
		{"v2.1.1-beta", "v2.1.1", 0},
	}

	for _, tt := range tests {
		got := compareVersions(tt.v1, tt.v2)
		if got != tt.expected {
			t.Errorf("compareVersions(%q, %q) = %d; want %d", tt.v1, tt.v2, got, tt.expected)
		}
	}
}

func TestIsNewerVersion(t *testing.T) {
	if !isNewerVersion("v2.1.2", "v2.1.1") {
		t.Errorf("expected v2.1.2 to be newer than v2.1.1")
	}
	if !isNewerVersion("v2.10.0", "v2.1.1") {
		t.Errorf("expected v2.10.0 to be newer than v2.1.1")
	}
	if isNewerVersion("v2.1.1", "v2.1.1") {
		t.Errorf("expected v2.1.1 not to be newer than v2.1.1")
	}
	if isNewerVersion("v2.1.0", "v2.1.1") {
		t.Errorf("expected v2.1.0 not to be newer than v2.1.1")
	}
}
