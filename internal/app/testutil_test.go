package app

import (
	"bytes"
	"errors"
	"testing"
)

func executeCommand(t *testing.T, args ...string) (string, string, error) {
	t.Helper()

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd := NewRootCommand()
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs(args)

	err := cmd.Execute()
	return stdout.String(), stderr.String(), err
}

func executeCommandWithService(t *testing.T, svc *Service, args ...string) (string, string, error) {
	t.Helper()

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd := NewRootCommandWithService(svc)
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs(args)

	err := cmd.Execute()
	return stdout.String(), stderr.String(), err
}

func requireCommandError(t *testing.T, err error, code, stderr string) {
	t.Helper()
	var commandErr *CommandError
	if !errors.As(err, &commandErr) {
		t.Fatalf("error = %v, want CommandError(%s); stderr=%s", err, code, stderr)
	}
	if commandErr.Code != code {
		t.Fatalf("command error code = %q, want %q; stderr=%s", commandErr.Code, code, stderr)
	}
}
