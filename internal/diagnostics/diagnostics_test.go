package diagnostics

import "testing"

func TestGetInfo(t *testing.T) {
	ds := NewDiagnosticsService()
	ds.SetInfo(CASCInfo{
		Source: "remote", Region: "cn", Product: "wow",
		BuildName: "Wrath of the Lich King", BuildKey: "abc123",
		CachePath: "/tmp/wowcache",
	})

	info := ds.GetInfo()
	if info.Source != "remote" {
		t.Fatalf("source = %s", info.Source)
	}
	if info.Region != "cn" {
		t.Fatalf("region = %s", info.Region)
	}
}

func TestGetProducts(t *testing.T) {
	ds := NewDiagnosticsService()
	ds.SetProducts(CASCProducts{
		Source: "remote",
		Products: []Product{
			{Label: "World of Warcraft", BuildIndex: 0},
			{Label: "Wrath of the Lich King", BuildIndex: 1},
		},
	})

	products := ds.GetProducts()
	if len(products.Products) != 2 {
		t.Fatalf("products = %d", len(products.Products))
	}
}

func TestDiagnose(t *testing.T) {
	ds := NewDiagnosticsService()
	ds.SetInfo(CASCInfo{
		Source:    "remote",
		Region:    "cn",
		Product:   "wow",
		BuildKey:  "abc123",
		CachePath: t.TempDir(),
	})

	d := ds.Diagnose()
	if !d.OK {
		t.Fatal("diagnose should be OK with valid config")
	}
	if len(d.Checks) == 0 {
		t.Fatal("should have checks")
	}
}

func TestDiagnoseMissingBuild(t *testing.T) {
	ds := NewDiagnosticsService()
	ds.SetInfo(CASCInfo{
		Source:    "remote",
		Region:    "cn",
		CachePath: t.TempDir(),
	})

	d := ds.Diagnose()
	foundUnknown := false
	for _, c := range d.Checks {
		if c.Name == "build_key" && c.Status == "unknown" {
			foundUnknown = true
		}
	}
	if !foundUnknown {
		t.Fatal("should have unknown build_key check")
	}
}

func TestVerifyFileHash(t *testing.T) {
	dir := t.TempDir()
	tmp := dir + "/test.txt"
	// Can't verify non-existent path
	_, err := VerifyFileHash(tmp, "abc")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}
