package main

import (
	"testing"

	"github.com/oplosy/atrisk/internal/buildinfo"
)

func TestVersionFormat(t *testing.T) {
	got := buildinfo.Format("collector", "test", "go1.27.0")
	if got != "atlasrisk collector version test runtime go1.27.0" {
		t.Fatalf("unexpected version output: %q", got)
	}
}
