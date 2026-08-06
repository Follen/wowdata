package main

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestNormalizeCaptureHomeHandlesNativeAndSlashPaths(t *testing.T) {
	home := filepath.Join("C:", "bench", "home")
	input := capture{Stdout: home + `\\cache` + "\n" + filepath.ToSlash(home) + "/cache", Stderr: home}
	got := normalizeCaptureHome(input, home)
	if strings.Contains(got.Stdout, filepath.Clean(home)) || strings.Contains(got.Stdout, filepath.ToSlash(filepath.Clean(home))) {
		t.Fatalf("stdout still contains home: %q", got.Stdout)
	}
	if got.Stderr != "<WOWDATA_HOME>" {
		t.Fatalf("stderr = %q", got.Stderr)
	}
}

func TestFreshCaptureHomeIsAbsolute(t *testing.T) {
	home, err := freshCaptureHome(filepath.Join("analyze", "golden"), "casc/diagnose")
	if err != nil {
		t.Fatal(err)
	}
	if !filepath.IsAbs(home) {
		t.Fatalf("fresh home is not absolute: %q", home)
	}
	if !strings.HasSuffix(filepath.ToSlash(home), "/analyze/golden/state-homes/casc-diagnose") {
		t.Fatalf("fresh home = %q", home)
	}
}

func TestValidateCaptureTargetRequiresPinnedRemoteBuild(t *testing.T) {
	base := capture{Name: "db2/rows", Args: []string{"--source", "remote", "--region", "us", "--product", "wow_classic_era"}}
	if _, _, err := validateCaptureTarget(base); err == nil || !strings.Contains(err.Error(), "explicit --build") {
		t.Fatalf("missing build error = %v", err)
	}
	base.Args = append(base.Args, "--build=latest")
	if _, _, err := validateCaptureTarget(base); err == nil || !strings.Contains(err.Error(), "pin --build") {
		t.Fatalf("latest build error = %v", err)
	}
	base.Args[len(base.Args)-1] = "--build=1.15.9.69109"
	identity, targeted, err := validateCaptureTarget(base)
	if err != nil || !targeted || identity.Build != "1.15.9.69109" || identity.Locale != "enUS" {
		t.Fatalf("identity = %#v, targeted=%v, err=%v", identity, targeted, err)
	}
	base.Args = append(base.Args, "--build", "69109")
	if _, _, err := validateCaptureTarget(base); err == nil || !strings.Contains(err.Error(), "exactly one") {
		t.Fatalf("duplicate build error = %v", err)
	}
}

func TestValidateCaptureTargetAllowsDynamicProductDiscovery(t *testing.T) {
	identity, targeted, err := validateCaptureTarget(capture{Name: "casc/products", Args: []string{"casc", "products", "--source", "remote", "--region", "us"}})
	if err != nil || targeted || identity.Product != "" {
		t.Fatalf("identity = %#v, targeted=%v, err=%v", identity, targeted, err)
	}
}

func TestCapturedBuildKey(t *testing.T) {
	value := capture{Stdout: `{"ok":true,"data":{"buildKey":"9f9686341092239cfa4812a0ba153dc6"}}`}
	if got := capturedBuildKey(value); got != "9f9686341092239cfa4812a0ba153dc6" {
		t.Fatalf("build key = %q", got)
	}
}

func TestCapturedProductBuildKeys(t *testing.T) {
	value := capture{Stdout: `{"ok":true,"data":{"products":[{"product":"wow","version":"12.0.7.68974","buildConfigKey":"96db6554c1ba271b52390175d50589f3"}]}}`}
	if got := capturedProductBuildKeys(value)["wow\x0012.0.7.68974"]; got != "96db6554c1ba271b52390175d50589f3" {
		t.Fatalf("build key = %q", got)
	}
}

func TestValidateVersionedCaptureRejectsTransientDownloadTelemetry(t *testing.T) {
	value := capture{Name: "casc/diagnose", Stderr: "prepare target=x\ndownload method=range duration=1s\nprepare status=ready\n"}
	if err := validateVersionedCapture(value); err == nil || !strings.Contains(err.Error(), "transient download telemetry") {
		t.Fatalf("error = %v", err)
	}
	value.Stderr = strings.Repeat("x", (64<<10)+1)
	if err := validateVersionedCapture(value); err == nil || !strings.Contains(err.Error(), "raw telemetry") {
		t.Fatalf("error = %v", err)
	}
}
