package storage

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMeasureCacheUsageClassifiesAndCountsDuplicatePayloads(t *testing.T) {
	root := t.TempDir()
	files := map[string]string{
		"casc/build-a/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa.index": "payload",
		"casc/build-b/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa.index": "payload",
		"casc/build-a/data/unique":                            "unique",
		"casc/build-a/build_manifest.json":                    "meta",
		"casc/build-a/cache_integrity.json":                   "integrity",
		"casc/build-a/resume/object.part":                     "partial",
		"casc/build-a/resume/object.json":                     "resume-state",
		"dbd/revision.json":                                   "revision",
		"dbd/rev/Spell.dbd":                                   "dbd-payload",
	}
	for name, body := range files {
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0644); err != nil {
			t.Fatal(err)
		}
	}

	usage, err := MeasureCacheUsage(root, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	wantPayload := int64(len("payload")*2 + len("unique") + len("dbd-payload"))
	wantUnique := int64(len("payload") + len("unique") + len("dbd-payload"))
	wantResume := int64(len("partial") + len("resume-state"))
	wantDerived := int64(len("meta") + len("integrity") + len("revision"))
	if usage.PayloadBytes != wantPayload || usage.UniquePayloadBytes != wantUnique || usage.DuplicatePayloadBytes != int64(len("payload")) {
		t.Fatalf("payload usage = %#v", usage)
	}
	if usage.ResumeBytes != wantResume || usage.DerivedMetadataBytes != wantDerived {
		t.Fatalf("metadata usage = %#v", usage)
	}
	if usage.TotalBytes != wantPayload+wantResume+wantDerived || !usage.WithinMaxBytes {
		t.Fatalf("total usage = %#v", usage)
	}
}

func TestMeasureCacheUsageMissingRootIsEmpty(t *testing.T) {
	usage, err := MeasureCacheUsage(filepath.Join(t.TempDir(), "missing"), 100)
	if err != nil {
		t.Fatal(err)
	}
	if usage.TotalBytes != 0 || !usage.WithinMaxBytes || !usage.WithinAmplification {
		t.Fatalf("usage = %#v", usage)
	}
}
