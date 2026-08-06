package main

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	wexport "wowdata/internal/export"
	"wowdata/internal/wowdata"
)

func TestCollectEncounterLogicalSkillsPreservesSharedFileSemanticEntries(t *testing.T) {
	sections := []*wowdata.EncounterSection{
		{ID: 10, SpellID: 100, Title: "技能 A"},
		{ID: 11, SpellID: 101, Title: "光环 B"},
	}
	info := &wowdata.SpellInfo{Spells: map[uint32]wowdata.DetailedSpellInfo{
		100: {SpellID: 100, Misc: map[string]interface{}{"SpellIconFileDataID": 900}},
		101: {SpellID: 101, Misc: map[string]interface{}{"SpellIconFileDataID": 900}},
	}}
	logical := collectEncounterLogicalSkills(sections, info)
	if len(logical) != 2 || logical[0].fileID != 900 || logical[1].fileID != 900 || logical[0].name == logical[1].name {
		t.Fatalf("logical skills = %#v", logical)
	}
	dir := t.TempDir()
	rendered := map[uint32]*wexport.RenderedIcon{900: {Data: []byte("png"), Width: 2, Height: 2, SHA256: "4c4b6a3be1314ab86138bef4314dde022e600960d8689a2c8f8631802d20dab6"}}
	items, failures := writeEncounterIconOutputs(dir, logical, rendered, nil)
	if failures != 0 || len(items) != 2 || items[0].Path == items[1].Path {
		t.Fatalf("items=%#v failures=%d", items, failures)
	}
	for _, item := range items {
		if _, err := os.Stat(filepath.Clean(item.Path)); err != nil {
			t.Fatal(err)
		}
	}
}

func TestReusableEncounterIconOutputsRequiresManifestIdentityAndValidFiles(t *testing.T) {
	dir := t.TempDir()
	logical := []encounterLogicalSkill{
		{sectionID: 10, spellID: 100, name: "技能 A", fileID: 900},
		{sectionID: 11, spellID: 101, name: "光环 B", fileID: 900},
	}
	data := []byte("shared-png")
	hash := fmt.Sprintf("%x", sha256.Sum256(data))
	rendered := map[uint32]*wexport.RenderedIcon{900: {Data: data, Width: 2, Height: 2, SHA256: hash}}
	planned := planEncounterIconOutputs(dir, logical)
	items, failures := writePlannedEncounterIconOutputs(planned, rendered, nil, nil)
	if failures != 0 {
		t.Fatalf("initial failures = %d", failures)
	}
	identity := encounterExportManifest{Schema: "wowdata.encounter-export.v1", Source: "remote", Region: "cn", Product: "wow", Build: "1.2.3", BuildKey: "build-key", Locale: "zhCN", JournalEncounterID: 7, BossIndex: 1, Items: items}

	reused, pending := reusableEncounterIconOutputs(planned, &identity, identity)
	if len(reused) != 2 || len(pending) != 0 {
		t.Fatalf("valid reuse = %d items, pending %v", len(reused), pending)
	}

	if err := os.WriteFile(items[0].Path, []byte("corrupt"), 0o644); err != nil {
		t.Fatal(err)
	}
	reused, pending = reusableEncounterIconOutputs(planned, &identity, identity)
	if len(reused) != 1 || len(pending) != 1 || pending[0] != 900 {
		t.Fatalf("corrupt reuse = %d items, pending %v", len(reused), pending)
	}

	otherBuild := identity
	otherBuild.BuildKey = "other-build"
	reused, pending = reusableEncounterIconOutputs(planned, &identity, otherBuild)
	if len(reused) != 0 || len(pending) != 1 || pending[0] != 900 {
		t.Fatalf("cross-build reuse = %d items, pending %v", len(reused), pending)
	}
}

func TestWritePlannedEncounterIconOutputsPreservesSuccessOnPartialFailure(t *testing.T) {
	dir := t.TempDir()
	planned := planEncounterIconOutputs(dir, []encounterLogicalSkill{
		{sectionID: 10, spellID: 100, name: "技能 A", fileID: 900},
		{sectionID: 11, spellID: 101, name: "技能 B", fileID: 901},
	})
	data := []byte("valid-png")
	hash := fmt.Sprintf("%x", sha256.Sum256(data))
	items, failures := writePlannedEncounterIconOutputs(
		planned,
		map[uint32]*wexport.RenderedIcon{900: {Data: data, Width: 2, Height: 2, SHA256: hash}},
		map[uint32]error{901: fmt.Errorf("decode failed")},
		nil,
	)
	if failures != 1 || items[0].Status != "success" || items[1].Status != "error" || items[1].Error != "decode failed" {
		t.Fatalf("items=%#v failures=%d", items, failures)
	}
	got, err := os.ReadFile(items[0].Path)
	if err != nil || string(got) != string(data) {
		t.Fatalf("successful output = %q, %v", got, err)
	}
	temps, err := filepath.Glob(filepath.Join(dir, ".wowdata-*.tmp"))
	if err != nil || len(temps) != 0 {
		t.Fatalf("temporary files = %v, %v", temps, err)
	}
}
