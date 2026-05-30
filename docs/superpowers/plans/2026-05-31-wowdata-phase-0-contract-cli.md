# wowdata Phase 0 Contract and CLI Skeleton Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Establish the Go module, `wowdata` CLI command tree, help contract, output envelope, Node capability inventory, and golden fixture framework before porting CASC/DB2/BLP business logic.

**Architecture:** Keep Phase 0 intentionally shallow: create a Go CLI shell with deterministic help, stub command handlers, shared JSON response types, and tests that lock the public command surface. Add documentation and inventory files that define the complete Node capability parity target for later implementation phases.

**Tech Stack:** Go, Cobra CLI, Go `testing`, JSON fixtures, PowerShell-friendly commands, existing Node source as baseline.

---

## Scope Check

The design spec covers several independent subsystems: CASC, DB2/WDC/DBC/DBD, BLTE, BLP, listfile, spell, encounter, item, creature, decor, diagnostics, and golden comparison.

This plan only implements Phase 0:

- Go module and CLI skeleton
- `wowdata --help` and command-group help
- stable JSON response envelope
- stub handlers that return explicit `not_implemented` responses
- capability inventory for old Node entry and core modules
- golden fixture directory conventions and compare/capture command stubs

Do not port CASC/DB2/BLP parsing in this phase.

## File Structure

- Create: `go.mod`
  - Declares the Go module and Cobra dependency.
- Create: `cmd/wowdata/main.go`
  - Minimal binary entrypoint.
- Create: `internal/app/root.go`
  - Root command, global flags, output writer, and command registration.
- Create: `internal/app/commands.go`
  - Command tree construction for every planned command group and subcommand.
- Create: `internal/app/output.go`
  - Stable JSON response envelope and error response helpers.
- Create: `internal/app/root_test.go`
  - Tests for top-level help, command help, output envelope, and not-implemented command behavior.
- Create: `internal/app/testutil_test.go`
  - Command execution helper for tests.
- Create: `internal/golden/golden.go`
  - Golden fixture path conventions and placeholder capture/compare service.
- Create: `internal/golden/golden_test.go`
  - Tests for deterministic fixture path generation and placeholder compare behavior.
- Create: `docs/wowdata/capability-inventory.md`
  - Human-reviewed inventory of old Node entry capabilities and core capabilities that must be replicated.
- Create: `docs/wowdata/golden-fixtures.md`
  - Fixture naming, capture, compare, and expected-difference rules.

---

### Task 1: Initialize Go Module

**Files:**
- Create: `go.mod`

- [ ] **Step 1: Write the module file**

```go
module wowdata

go 1.22

require github.com/spf13/cobra v1.8.1
```

- [ ] **Step 2: Download module metadata**

Run: `go mod tidy`

Expected: `go.sum` is created and Cobra dependencies resolve.

- [ ] **Step 3: Commit**

```bash
git add go.mod go.sum
git commit -m "build: initialize go module"
```

---

### Task 2: Add JSON Output Contract

**Files:**
- Create: `internal/app/output.go`
- Test: `internal/app/root_test.go`

- [ ] **Step 1: Write failing tests for output envelope**

Create `internal/app/root_test.go` with:

```go
package app

import (
	"encoding/json"
	"testing"
)

func TestResponseEnvelopeSuccess(t *testing.T) {
	resp := NewSuccessResponse("wowdata test", map[string]any{"value": 42})

	if !resp.OK {
		t.Fatalf("expected OK response")
	}
	if resp.Command != "wowdata test" {
		t.Fatalf("command = %q", resp.Command)
	}
	if resp.Error != nil {
		t.Fatalf("unexpected error: %#v", resp.Error)
	}

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal response: %v", err)
	}

	var decoded map[string]any
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	for _, key := range []string{"ok", "command", "data", "warnings"} {
		if _, exists := decoded[key]; !exists {
			t.Fatalf("missing envelope key %q in %#v", key, decoded)
		}
	}
}

func TestResponseEnvelopeError(t *testing.T) {
	resp := NewErrorResponse("wowdata bad", "not_implemented", "command is not implemented yet")

	if resp.OK {
		t.Fatalf("expected non-OK response")
	}
	if resp.Error == nil {
		t.Fatalf("expected error payload")
	}
	if resp.Error.Code != "not_implemented" {
		t.Fatalf("error code = %q", resp.Error.Code)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/app`

Expected: FAIL because `NewSuccessResponse` and `NewErrorResponse` do not exist.

- [ ] **Step 3: Implement output envelope**

Create `internal/app/output.go` with:

```go
package app

type Response struct {
	OK       bool           `json:"ok"`
	Command  string         `json:"command"`
	Data     any            `json:"data"`
	Warnings []string       `json:"warnings"`
	Error    *ErrorResponse `json:"error,omitempty"`
}

type ErrorResponse struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func NewSuccessResponse(command string, data any) Response {
	return Response{
		OK:       true,
		Command:  command,
		Data:     data,
		Warnings: []string{},
	}
}

func NewErrorResponse(command string, code string, message string) Response {
	return Response{
		OK:       false,
		Command:  command,
		Data:     nil,
		Warnings: []string{},
		Error: &ErrorResponse{
			Code:    code,
			Message: message,
		},
	}
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/app`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/app/output.go internal/app/root_test.go
git commit -m "feat: add wowdata response envelope"
```

---

### Task 3: Build Root CLI and Binary Entrypoint

**Files:**
- Create: `cmd/wowdata/main.go`
- Create: `internal/app/root.go`
- Modify: `internal/app/root_test.go`
- Create: `internal/app/testutil_test.go`

- [ ] **Step 1: Add command execution test helper**

Create `internal/app/testutil_test.go` with:

```go
package app

import (
	"bytes"
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
```

- [ ] **Step 2: Add failing tests for root help**

Append to `internal/app/root_test.go`:

```go
func TestRootHelp(t *testing.T) {
	stdout, stderr, err := executeCommand(t, "--help")
	if err != nil {
		t.Fatalf("help returned error: %v stderr=%s", err, stderr)
	}
	for _, want := range []string{
		"wowdata",
		"warmup",
		"db2",
		"golden",
		"Examples:",
	} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("root help missing %q:\n%s", want, stdout)
		}
	}
}
```

Also add this import to `internal/app/root_test.go`:

```go
	"strings"
```

- [ ] **Step 3: Run tests to verify they fail**

Run: `go test ./internal/app`

Expected: FAIL because `NewRootCommand` does not exist.

- [ ] **Step 4: Implement root command**

Create `internal/app/root.go` with:

```go
package app

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/spf13/cobra"
)

func NewRootCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "wowdata",
		Short: "Query and export World of Warcraft data from local clients or remote CDN builds.",
		Long: "wowdata is a Go CLI for querying World of Warcraft CASC, DB2, listfile, texture, item, creature, decor, spell, and encounter data.\n\n" +
			"Run warmup before commands that require an active build context. First use may take time while manifests, indexes, listfiles, and DB definitions are cached.",
		Example: "  wowdata warmup --source remote --region cn --product wow\n" +
			"  wowdata db2 rows SpellName --id 123\n" +
			"  wowdata file lookup --file-data-id 456\n" +
			"  wowdata icon export --file-data-id 789 --format png",
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	registerCommands(cmd)
	return cmd
}

func writeJSON(w io.Writer, resp Response) error {
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	return encoder.Encode(resp)
}

func notImplementedHandler(commandName string) func(cmd *cobra.Command, args []string) error {
	return func(cmd *cobra.Command, args []string) error {
		resp := NewErrorResponse(commandName, "not_implemented", fmt.Sprintf("%s is planned but not implemented in Phase 0", commandName))
		return writeJSON(cmd.OutOrStdout(), resp)
	}
}
```

Create `cmd/wowdata/main.go` with:

```go
package main

import (
	"fmt"
	"os"

	"wowdata/internal/app"
)

func main() {
	cmd := app.NewRootCommand()
	if err := cmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
```

- [ ] **Step 5: Run tests to verify they still fail for missing command registration**

Run: `go test ./internal/app`

Expected: FAIL because `registerCommands` does not exist or help does not list command groups.

- [ ] **Step 6: Commit root skeleton**

```bash
git add cmd/wowdata/main.go internal/app/root.go internal/app/root_test.go internal/app/testutil_test.go
git commit -m "feat: add wowdata root command"
```

---

### Task 4: Register Full Command Tree

**Files:**
- Create: `internal/app/commands.go`
- Modify: `internal/app/root_test.go`

- [ ] **Step 1: Add failing tests for command help coverage**

Append to `internal/app/root_test.go`:

```go
func TestAllPlannedCommandHelp(t *testing.T) {
	commands := [][]string{
		{"warmup"},
		{"db2"}, {"db2", "schema"}, {"db2", "rows"}, {"db2", "search"}, {"db2", "foreign-key"}, {"db2", "stream"},
		{"spell"}, {"spell", "info"}, {"spell", "auras"}, {"spell", "summons"},
		{"encounter"}, {"encounter", "get"},
		{"file"}, {"file", "lookup"}, {"file", "search"}, {"file", "extension"}, {"file", "get"}, {"file", "exists"}, {"file", "encoding"}, {"file", "export"},
		{"icon"}, {"icon", "export"},
		{"casc"}, {"casc", "info"}, {"casc", "products"}, {"casc", "diagnose"},
		{"item"}, {"item", "get"}, {"item", "models"}, {"item", "geosets"}, {"item", "textures"},
		{"creature"}, {"creature", "display"}, {"creature", "model"},
		{"decor"}, {"decor", "list"}, {"decor", "get"},
		{"golden"}, {"golden", "capture"}, {"golden", "compare"},
	}

	for _, parts := range commands {
		args := append(append([]string{}, parts...), "--help")
		stdout, stderr, err := executeCommand(t, args...)
		if err != nil {
			t.Fatalf("%v --help returned error: %v stderr=%s", parts, err, stderr)
		}
		if !strings.Contains(stdout, "Usage:") {
			t.Fatalf("%v --help missing usage:\n%s", parts, stdout)
		}
		if !strings.Contains(stdout, "Examples:") {
			t.Fatalf("%v --help missing examples:\n%s", parts, stdout)
		}
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/app`

Expected: FAIL because command tree is missing.

- [ ] **Step 3: Implement command tree**

Create `internal/app/commands.go` with:

```go
package app

import "github.com/spf13/cobra"

type commandSpec struct {
	use     string
	short   string
	example string
	child   []commandSpec
}

func registerCommands(root *cobra.Command) {
	specs := []commandSpec{
		{use: "warmup", short: "Initialize local or remote WoW data context.", example: "  wowdata warmup --source remote --region cn --product wow"},
		{use: "db2", short: "Query DB2 tables.", example: "  wowdata db2 rows SpellName --id 123", child: []commandSpec{
			{use: "schema <table>", short: "Print parsed schema metadata.", example: "  wowdata db2 schema SpellName"},
			{use: "rows <table>", short: "Fetch rows by ID, fields, filter, and limit.", example: "  wowdata db2 rows SpellName --id 123 --limit 1"},
			{use: "search <table>", short: "Search a field case-insensitively.", example: "  wowdata db2 search SpellName --field Name_lang --query fire"},
			{use: "foreign-key <table>", short: "Query rows by foreign key relationship.", example: "  wowdata db2 foreign-key SpellEffect --field SpellID --value 123"},
			{use: "stream <table>", short: "Stream large table rows as JSON lines.", example: "  wowdata db2 stream SpellEffect --limit 100"},
		}},
		{use: "spell", short: "Inspect spell relationships.", example: "  wowdata spell info --spell-id 123", child: []commandSpec{
			{use: "info", short: "Inspect spell trigger chains and description references.", example: "  wowdata spell info --spell-id 123 --max-depth 5"},
			{use: "auras", short: "Detect aura presence for spells.", example: "  wowdata spell auras --spell-id 123"},
			{use: "summons", short: "Detect NPC summons from spell effects.", example: "  wowdata spell summons --spell-id 123 --npc-id 456"},
		}},
		{use: "encounter", short: "Query JournalEncounter data.", example: "  wowdata encounter get --journal-encounter-id 123", child: []commandSpec{
			{use: "get", short: "Return section tree and related spell IDs.", example: "  wowdata encounter get --journal-encounter-id 123"},
		}},
		{use: "file", short: "Query and export CASC files.", example: "  wowdata file lookup --file-data-id 456", child: []commandSpec{
			{use: "lookup", short: "Resolve fileDataID to filename.", example: "  wowdata file lookup --file-data-id 456"},
			{use: "search", short: "Search listfile entries.", example: "  wowdata file search --query interface/icons"},
			{use: "extension", short: "List files by extension.", example: "  wowdata file extension --extension blp"},
			{use: "get", short: "Fetch a raw CASC file by ID or name.", example: "  wowdata file get --file-data-id 456 --output out.bin"},
			{use: "exists", short: "Check whether a file exists.", example: "  wowdata file exists --file-data-id 456"},
			{use: "encoding", short: "Inspect content key and encoding key metadata.", example: "  wowdata file encoding --file-data-id 456"},
			{use: "export", short: "Write raw CASC files to disk.", example: "  wowdata file export --file-data-id 456 --output output/file.bin"},
		}},
		{use: "icon", short: "Export BLP textures.", example: "  wowdata icon export --file-data-id 789 --format png", child: []commandSpec{
			{use: "export", short: "Export BLP as PNG or WebP.", example: "  wowdata icon export --file-data-id 789 --format png --mipmap 0"},
		}},
		{use: "casc", short: "Inspect CASC source state.", example: "  wowdata casc info", child: []commandSpec{
			{use: "info", short: "Show current build and cache state.", example: "  wowdata casc info"},
			{use: "products", short: "List available products and builds.", example: "  wowdata casc products --source remote --region cn"},
			{use: "diagnose", short: "Inspect CDN, archive, root, encoding, cache, and TACT state.", example: "  wowdata casc diagnose"},
		}},
		{use: "item", short: "Query item metadata and assets.", example: "  wowdata item get --item-id 19019", child: []commandSpec{
			{use: "get", short: "Return item summary and slot information.", example: "  wowdata item get --item-id 19019"},
			{use: "models", short: "Return model fileDataIDs and textures.", example: "  wowdata item models --item-id 19019 --race-id 1 --gender 0"},
			{use: "geosets", short: "Return geoset and helmet-hide data.", example: "  wowdata item geosets --item-id 19019"},
			{use: "textures", short: "Return character texture fileDataIDs.", example: "  wowdata item textures --item-id 19019"},
		}},
		{use: "creature", short: "Query creature displays and models.", example: "  wowdata creature display --display-id 123", child: []commandSpec{
			{use: "display", short: "Query creature display metadata.", example: "  wowdata creature display --display-id 123"},
			{use: "model", short: "Query creature model fileDataIDs and variants.", example: "  wowdata creature model --file-data-id 456"},
		}},
		{use: "decor", short: "Query decor data.", example: "  wowdata decor list", child: []commandSpec{
			{use: "list", short: "List decor entries.", example: "  wowdata decor list --limit 50"},
			{use: "get", short: "Query decor item by ID or model fileDataID.", example: "  wowdata decor get --id 123"},
		}},
		{use: "golden", short: "Capture and compare golden fixtures.", example: "  wowdata golden compare --fixture warmup/remote-cn-wow.json", child: []commandSpec{
			{use: "capture", short: "Capture Node baseline or Go command output.", example: "  wowdata golden capture --name db2-spellname-123 -- wowdata db2 rows SpellName --id 123"},
			{use: "compare", short: "Compare Go command output against a fixture.", example: "  wowdata golden compare --fixture fixtures/golden/db2-spellname-123.json"},
		}},
	}

	for _, spec := range specs {
		root.AddCommand(buildCommand(spec))
	}
}

func buildCommand(spec commandSpec) *cobra.Command {
	cmd := &cobra.Command{
		Use:          spec.use,
		Short:        spec.short,
		Example:      spec.example,
		SilenceUsage: true,
		RunE:         notImplementedHandler(spec.use),
	}
	for _, child := range spec.child {
		cmd.AddCommand(buildCommand(child))
	}
	return cmd
}
```

- [ ] **Step 4: Run tests to verify command help passes**

Run: `go test ./internal/app`

Expected: PASS.

- [ ] **Step 5: Build CLI**

Run: `go build ./cmd/wowdata`

Expected: PASS and a `wowdata.exe` binary appears in the repository root on Windows.

- [ ] **Step 6: Remove local build artifact if created**

Run: `Remove-Item .\wowdata.exe`

Expected: local binary is removed.

- [ ] **Step 7: Commit**

```bash
git add cmd/wowdata internal/app
git commit -m "feat: register wowdata command tree"
```

---

### Task 5: Add Golden Fixture Framework Stubs

**Files:**
- Create: `internal/golden/golden.go`
- Create: `internal/golden/golden_test.go`

- [ ] **Step 1: Write failing tests**

Create `internal/golden/golden_test.go` with:

```go
package golden

import "testing"

func TestFixturePathIsDeterministic(t *testing.T) {
	path := FixturePath("db2", "spellname-123")
	want := "fixtures/golden/db2/spellname-123.json"
	if path != want {
		t.Fatalf("path = %q, want %q", path, want)
	}
}

func TestComparePlaceholder(t *testing.T) {
	result := CompareResult{
		Fixture: "fixtures/golden/db2/spellname-123.json",
		Equal:   false,
		Reason:  "not_implemented",
	}
	if result.Equal {
		t.Fatalf("placeholder compare should not report equality")
	}
	if result.Reason != "not_implemented" {
		t.Fatalf("reason = %q", result.Reason)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/golden`

Expected: FAIL because `FixturePath` and `CompareResult` do not exist.

- [ ] **Step 3: Implement golden stubs**

Create `internal/golden/golden.go` with:

```go
package golden

import "path/filepath"

type CompareResult struct {
	Fixture string `json:"fixture"`
	Equal   bool   `json:"equal"`
	Reason  string `json:"reason,omitempty"`
}

func FixturePath(group string, name string) string {
	return filepath.ToSlash(filepath.Join("fixtures", "golden", group, name+".json"))
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/golden`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/golden
git commit -m "feat: add golden fixture stubs"
```

---

### Task 6: Document Node Capability Inventory

**Files:**
- Create: `docs/wowdata/capability-inventory.md`

- [ ] **Step 1: Create inventory document**

Create `docs/wowdata/capability-inventory.md` with:

```markdown
# wowdata Node Capability Inventory

This inventory is the required parity checklist for the Go `wowdata` CLI.

## Old Node Entry Capabilities

- warmup source selection: local, remote
- region selection: cn, us, eu, kr, tw
- product/build selection: wow, wow_classic, wow_classic_titan, wow_classic_era, wowt, wowxptr, buildIndex
- listfile warmup and cache validation
- DBD manifest warmup
- DB2 table preload
- DB2 schema, rows, search, field selection, filters, limits
- spell info chains, aura detection, summon detection
- encounter section tree and related spell IDs
- file lookup, search, extension query, icon extraction

## Core Capabilities That Must Become CLI Commands

- CASC file access by fileDataID
- CASC file access by filename
- CASC virtual file access by fileDataID and filename
- file existence checks
- file encoding metadata: content key and encoding key
- install manifest reading
- local source loading, indexes, configs, root, encoding
- remote source loading, CDN config, archives, root, encoding
- CDN host resolution and diagnostics
- build cache integrity and manifest behavior
- TACT key loading, lookup, and missing-key diagnostics
- BLTE full decode and stream decode
- Salsa20 decryption behavior used by BLTE
- DBD manifest lookup by table name and file ID
- DB2 get row, get all rows, relation rows, foreign-key helper, stream rows
- DBC legacy reader support used by legacy creature data
- listfile lookup by ID, search, extension, unknown model/texture enrichment
- BLP PNG export, WebP export, mipmap, mask, quality, raw buffer behavior
- item summary and slot lookup
- item model lookup by race and gender
- item geoset and helmet-hide lookup
- item character texture lookup
- creature display lookup by fileDataID and display ID
- legacy creature display lookup by model path
- decor list and lookup by ID or model fileDataID
- equipment slot and item slot naming helpers
- VP9 AVI demuxer if retained as a meaningful data capability

## Difference Policy

Any Node behavior not replicated must be listed in an implementation note as an intentional difference with:

- original Node behavior
- Go replacement behavior
- reason for the difference
- affected commands
- test coverage proving the new behavior
```

- [ ] **Step 2: Verify inventory mentions required parity language**

Run: `Select-String -Path docs/wowdata/capability-inventory.md -Pattern "required parity checklist","Core Capabilities","Difference Policy"`

Expected: All three patterns are printed.

- [ ] **Step 3: Commit**

```bash
git add docs/wowdata/capability-inventory.md
git commit -m "docs: add node capability inventory"
```

---

### Task 7: Document Golden Fixture Rules

**Files:**
- Create: `docs/wowdata/golden-fixtures.md`

- [ ] **Step 1: Create golden rules document**

Create `docs/wowdata/golden-fixtures.md` with:

```markdown
# wowdata Golden Fixture Rules

Golden fixtures prove that Go `wowdata` replicates Node business behavior.

## Fixture Layout

- `fixtures/golden/node/<group>/<name>.json`: captured Node baseline
- `fixtures/golden/go/<group>/<name>.json`: captured Go command output when useful for debugging
- `fixtures/golden/diff/<group>/<name>.json`: semantic comparison output

## Naming

Use lowercase names with hyphens:

- `warmup/remote-cn-wow`
- `db2/spellname-id-123`
- `file/lookup-id-456`
- `icon/export-id-789-png`

## Capture Rules

- Capture successful paths and business-meaningful error paths.
- Do not commit machine-specific absolute paths unless the command being tested is specifically about path behavior.
- For exported files, store metadata and content hash, not large binary payloads.
- Record source, region, product, build, command, args, and timestamp.

## Compare Rules

- Compare semantic JSON fields.
- Ignore raw formatting.
- Normalize path separators when path style is not the behavior under test.
- Compare generated artifact hashes for export commands.
- Fail when a Node capability has no Go CLI equivalent.

## Expected Differences

Intentional differences must be documented next to the fixture with:

- original Node behavior
- Go behavior
- reason
- affected command
```

- [ ] **Step 2: Verify rules document**

Run: `Select-String -Path docs/wowdata/golden-fixtures.md -Pattern "Fail when a Node capability has no Go CLI equivalent","Expected Differences"`

Expected: Both patterns are printed.

- [ ] **Step 3: Commit**

```bash
git add docs/wowdata/golden-fixtures.md
git commit -m "docs: add golden fixture rules"
```

---

### Task 8: Run Phase 0 Verification

**Files:**
- Verify all files created by prior tasks.

- [ ] **Step 1: Run Go tests**

Run: `go test ./...`

Expected: PASS.

- [ ] **Step 2: Run CLI help**

Run: `go run ./cmd/wowdata --help`

Expected: output includes `wowdata`, `warmup`, `db2`, `golden`, and `Examples:`.

- [ ] **Step 3: Run nested help**

Run: `go run ./cmd/wowdata file encoding --help`

Expected: output includes `Inspect content key and encoding key metadata`, `Usage:`, and `Examples:`.

- [ ] **Step 4: Run stub command**

Run: `go run ./cmd/wowdata db2 rows SpellName`

Expected: JSON response with `"ok": false`, `"code": "not_implemented"`, and `"command": "rows <table>"`.

- [ ] **Step 5: Check git status**

Run: `git status --short`

Expected: clean working tree after commits.

---

## Self-Review Checklist

- The plan does not port CASC/DB2/BLP parsing in Phase 0.
- The complete command tree from the spec is represented.
- `--help` is tested for every planned command and subcommand.
- JSON envelope is tested before command handlers use it.
- Node capability parity is documented before implementation phases begin.
- Golden fixture policy fails missing Go equivalents instead of silently skipping them.

## Execution Handoff

Plan complete and saved to `docs/superpowers/plans/2026-05-31-wowdata-phase-0-contract-cli.md`.

Two execution options:

1. **Subagent-Driven (recommended)** - dispatch a fresh subagent per task, review between tasks, fast iteration.
2. **Inline Execution** - execute tasks in this session using executing-plans, batch execution with checkpoints.

Which approach?
