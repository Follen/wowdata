package runtime

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"wowdata/internal/casc"
	"wowdata/internal/resource"
)

func TestHTTPDBDSourceFetchesDefinition(t *testing.T) {
	casc.ResetHTTPMetrics()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "SpellName.dbd") {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		w.Write([]byte("COLUMNS\nint ID\n"))
	}))
	defer server.Close()

	source := NewHTTPDBDSource(t.TempDir(), []string{server.URL + "/%s.dbd"})
	def, err := source.Definition("SpellName")
	if err != nil {
		t.Fatalf("Definition: %v", err)
	}
	if !strings.Contains(def, "int ID") {
		t.Fatalf("definition = %q", def)
	}
	metrics := casc.SnapshotHTTPMetrics()
	if metrics.Requests != 1 || metrics.ResponseBytes == 0 || metrics.UniquePayloadBytes != metrics.ResponseBytes {
		t.Fatalf("HTTP metrics = %#v", metrics)
	}
}

func TestHTTPDBDSourceAttributesDefinitionRequestToStage(t *testing.T) {
	casc.ResetHTTPMetrics()
	resource.ResetStages()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("COLUMNS\nint ID\n"))
	}))
	defer server.Close()

	stage := resource.StartStage("dbd-definition", resource.StageOptions{Instance: "SpellName"})
	source := NewHTTPDBDSource(t.TempDir(), []string{server.URL + "/%s.dbd"})
	if _, err := source.DefinitionWithStage("SpellName", stage); err != nil {
		t.Fatalf("DefinitionWithStage: %v", err)
	}
	resource.FinishStage(stage, nil)
	metrics := casc.SnapshotHTTPMetrics()
	resource.ReconcileStageNetwork(uint64(metrics.UniquePayloadBytes), uint64(metrics.ResponseBytes))
	snapshot := resource.SnapshotStages()
	if !snapshot.Complete || len(snapshot.Work) != 1 {
		t.Fatalf("stages = %#v", snapshot)
	}
	work := snapshot.Work[0]
	if work.StageID != stage || work.NetworkUniqueBytes != uint64(metrics.UniquePayloadBytes) || work.NetworkTransferredBytes != uint64(metrics.ResponseBytes) {
		t.Fatalf("stage work = %#v, HTTP metrics = %#v", work, metrics)
	}
}

func TestHTTPDBDSourceRefreshesCachedDefinition(t *testing.T) {
	cacheDir := t.TempDir()
	cachePath := filepath.Join(cacheDir, "SpellName.dbd")
	if err := os.WriteFile(cachePath, []byte("COLUMNS\nint Old\n"), 0644); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("COLUMNS\nint Current\n"))
	}))
	defer server.Close()

	source := NewHTTPDBDSource(cacheDir, []string{server.URL + "/%s.dbd"})
	definition, err := source.Definition("SpellName")
	if err != nil {
		t.Fatalf("Definition: %v", err)
	}
	if !strings.Contains(definition, "int Current") {
		t.Fatalf("definition = %q", definition)
	}
	cached, err := os.ReadFile(cachePath)
	if err != nil {
		t.Fatal(err)
	}
	if string(cached) != definition {
		t.Fatalf("cache = %q, definition = %q", cached, definition)
	}
}

func TestHTTPDBDSourceFallsBackToCachedDefinition(t *testing.T) {
	cacheDir := t.TempDir()
	cachePath := filepath.Join(cacheDir, "SpellName.dbd")
	if err := os.WriteFile(cachePath, []byte("COLUMNS\nint Cached\n"), 0644); err != nil {
		t.Fatal(err)
	}

	source := NewHTTPDBDSource(cacheDir, []string{"http://127.0.0.1:1/%s.dbd"})
	definition, err := source.Definition("SpellName")
	if err != nil {
		t.Fatalf("Definition: %v", err)
	}
	if !strings.Contains(definition, "int Cached") {
		t.Fatalf("definition = %q", definition)
	}
}
