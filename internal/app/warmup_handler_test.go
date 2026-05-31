package app

import (
	"strings"
	"testing"
)

func TestWarmupLocalHelpFlagSurface(t *testing.T) {
	stdout, stderr, err := executeCommandWithService(t, &Service{}, "warmup", "--help")
	if err != nil {
		t.Fatalf("warmup help returned error: %v stderr=%s", err, stderr)
	}
	if !strings.Contains(stdout, "--path") {
		t.Fatalf("warmup help should include --path for local source:\n%s", stdout)
	}
}
