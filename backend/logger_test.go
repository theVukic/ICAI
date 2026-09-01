package main

import "testing"

func TestTerminalLogColor(t *testing.T) {
	tests := []struct {
		message string
		want    string
	}{
		{"[PUBLISH] failed towKod=RB-010 error=validation", terminalColorRed},
		{"[OLX] create listing retry towKod=RB-010 attempt=2/2", terminalColorYellow},
		{"[PUBLISH] success towKod=RB-010 listingID=42", terminalColorGreen},
		{"[EVENT] [CATALOG] state requested files=1", terminalColorWhite},
	}

	for _, test := range tests {
		if got := terminalLogColor(test.message); got != test.want {
			t.Fatalf("terminalLogColor(%q) = %q, want %q", test.message, got, test.want)
		}
	}
}
