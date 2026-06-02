package refresh

import (
	"context"
	"testing"
)

func TestRunUpdateFixtureCoversRemoteHarnessChecks(t *testing.T) {
	for _, check := range []string{
		"UpdateNoNewBuild",
		"UpdateCandidateSuccess",
		"UpdateCandidateFailure",
		"UpdateListfileSourceHashChange",
		"UpdateCASCIndexVersionChange",
		"UpdateDB2FingerprintChange",
		"UpdateUnaffectedTableStillValid",
	} {
		t.Run(check, func(t *testing.T) {
			got, err := RunUpdateFixture(context.Background(), check)
			if err != nil {
				t.Fatalf("RunUpdateFixture: %v", err)
			}
			if !got.Passed {
				t.Fatalf("fixture = %#v, want passed", got)
			}
		})
	}
}

func TestRunUpdateFixtureReportsUnknownCheck(t *testing.T) {
	got, err := RunUpdateFixture(context.Background(), "missing")
	if err != nil {
		t.Fatalf("RunUpdateFixture: %v", err)
	}
	if got.Passed || got.Error == "" {
		t.Fatalf("fixture = %#v, want failed with error", got)
	}
}
