package raid

import (
	"testing"
)

func TestParseRaidDump(t *testing.T) {
	tests := []struct {
		name     string
		lines    []string
		expected int
	}{
		{
			name: "pipe delimited format",
			lines: []string{
				"1 | Valorith | 60 | Warrior",
				"1 | Xackery | 55 | Cleric",
				"2 | TestPlayer | 60 | Wizard",
			},
			expected: 3,
		},
		{
			name: "name level class group format",
			lines: []string{
				"Valorith  60  Warrior  1",
				"Xackery  55  Cleric  1",
				"TestPlayer  60  Wizard  2",
			},
			expected: 3,
		},
		{
			name: "parenthetical format",
			lines: []string{
				"Valorith (60 Warrior)",
				"Xackery (55 Cleric)",
			},
			expected: 2,
		},
		{
			name: "simple name format",
			lines: []string{
				"Valorith",
				"Xackery",
				"TestPlayer",
			},
			expected: 3,
		},
		{
			name: "deduplication",
			lines: []string{
				"Valorith (60 Warrior)",
				"valorith (60 Warrior)",
				"Xackery (55 Cleric)",
			},
			expected: 2,
		},
		{
			name: "skip header words",
			lines: []string{
				"Name",
				"Valorith",
				"Level",
				"Xackery",
			},
			expected: 2,
		},
		{
			name: "bracket group format",
			lines: []string{
				"[Group 1] Valorith 60 Warrior",
				"[Group 2] Xackery 55 Cleric",
			},
			expected: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			members := ParseRaidDump(tt.lines)
			if len(members) != tt.expected {
				t.Errorf("expected %d members, got %d", tt.expected, len(members))
				for _, m := range members {
					t.Logf("  member: %+v", m)
				}
			}
		})
	}
}

func TestNormalizeClass(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"Warrior", "WARRIOR"},
		{"shadow knight", "SHADOWKNIGHT"},
		{"ShadowKnight", "SHADOWKNIGHT"},
		{"Cleric", "CLERIC"},
		{"WIZARD", "WIZARD"},
		{"Unknown Class", "UNKNOWN"},
		{"", "UNKNOWN"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := normalizeClass(tt.input)
			if result != tt.expected {
				t.Errorf("normalizeClass(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}
