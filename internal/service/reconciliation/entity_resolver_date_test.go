package reconciliation

import (
	"testing"
)

func TestIsDateClose(t *testing.T) {
	tests := []struct {
		name     string
		d1       string
		d2       string
		maxDays  int
		expected bool
	}{
		{
			name:     "Exact same day",
			d1:       "2026-01-01",
			d2:       "2026-01-01",
			maxDays:  7,
			expected: true,
		},
		{
			name:     "7 days apart within 7 maxDays",
			d1:       "2026-01-01",
			d2:       "2026-01-08",
			maxDays:  7,
			expected: true,
		},
		{
			name:     "8 days apart exceeds 7 maxDays",
			d1:       "2026-01-01",
			d2:       "2026-01-09",
			maxDays:  7,
			expected: false,
		},
		{
			name:     "Cross month transition 1 day apart within tolerance",
			d1:       "2026-01-31",
			d2:       "2026-02-01",
			maxDays:  7,
			expected: true,
		},
		{
			name:     "Same month 30 days apart must fail 7 days tolerance",
			d1:       "2026-01-01",
			d2:       "2026-01-31",
			maxDays:  7,
			expected: false,
		},
		{
			name:     "Reverse order cross month transition within tolerance",
			d1:       "2026-02-02",
			d2:       "2026-01-31",
			maxDays:  7,
			expected: true,
		},
		{
			name:     "Invalid date format d1",
			d1:       "2026/01/01",
			d2:       "2026-01-02",
			maxDays:  7,
			expected: false,
		},
		{
			name:     "Invalid date format d2",
			d1:       "2026-01-01",
			d2:       "01-02-2026",
			maxDays:  7,
			expected: false,
		},
		{
			name:     "Short length string",
			d1:       "2026-01",
			d2:       "2026-01-02",
			maxDays:  7,
			expected: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			actual := isDateClose(tc.d1, tc.d2, tc.maxDays)
			if actual != tc.expected {
				t.Errorf("[%s] isDateClose(%q, %q, %d) = %v, expected %v", tc.name, tc.d1, tc.d2, tc.maxDays, actual, tc.expected)
			}
		})
	}
}
