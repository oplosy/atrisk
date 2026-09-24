package buildinfo

import "testing"

func TestFormat(t *testing.T) {
	if got := Format("component", "1.2.3", "go1.27.0"); got != "atlasrisk component version 1.2.3 runtime go1.27.0" {
		t.Fatalf("unexpected diagnostic: %q", got)
	}
}
