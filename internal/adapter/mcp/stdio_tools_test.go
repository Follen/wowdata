package mcp

import "testing"

func TestStdioToolNamesIncludeWarmupAndExistingTools(t *testing.T) {
	names := stringSet(StdioToolNames())

	for _, want := range []string{"wow_warmup", "wow_db2", "wow_icon"} {
		if !names[want] {
			t.Fatalf("StdioToolNames missing %q; got %#v", want, names)
		}
	}
}

func stringSet(values []string) map[string]bool {
	out := make(map[string]bool, len(values))
	for _, value := range values {
		out[value] = true
	}
	return out
}
