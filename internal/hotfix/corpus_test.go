package hotfix

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

type corpusManifest struct {
	Items []struct {
		Path   string `json:"path"`
		SHA256 string `json:"sha256"`
	} `json:"items"`
}

func TestOfflineWagoCorpusManifest(t *testing.T) {
	root := filepath.Join("testdata", "wago-hotfix-offline")
	var m corpusManifest
	b, _ := os.ReadFile(filepath.Join(root, "manifest.json"))
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	for _, item := range m.Items {
		if filepath.Ext(item.Path) != ".html" {
			continue
		}
		fixturePath := item.Path
		if _, statErr := os.Stat(fixturePath); os.IsNotExist(statErr) {
			fixturePath = filepath.Join("..", "..", filepath.FromSlash(item.Path))
		}
		raw, err := os.ReadFile(fixturePath)
		if err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(raw)
		if got := hex.EncodeToString(sum[:]); got != item.SHA256 {
			t.Fatalf("%s sha=%s want=%s", item.Path, got, item.SHA256)
		}
		q := Query{Product: "wow", Build: "68974", Region: 71, Locale: "enUS", Search: "68974"}
		r, meta, err := ParseWagoHTML(raw, q)
		if err != nil {
			t.Fatal(err)
		}
		if meta.CurrentPage < 1 || meta.LastPage < 100000 || meta.Total < 1000000 {
			t.Fatalf("corpus page=%+v records=%d", meta, len(r.Records))
		}
	}
}
