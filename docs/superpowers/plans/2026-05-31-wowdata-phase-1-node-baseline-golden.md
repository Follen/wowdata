# wowdata Phase 1 Node Baseline and Golden Fixtures Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Capture the current Node behavior as a queryable baseline before Go implementation begins in earnest.

**Architecture:** Add small Node-side baseline scripts that call existing functions or CLI entry behavior, normalize output into golden fixture envelopes, and maintain a manifest of covered capabilities. This phase does not alter production Node behavior.

**Tech Stack:** Node.js, Go tests where useful, JSON fixtures, PowerShell-friendly scripts.

---

## File Structure

- Create: `tools/node-baseline/capture.mjs`
- Create: `tools/node-baseline/capabilities.mjs`
- Create: `fixtures/golden/node/manifest.json`
- Create: `docs/wowdata/node-baseline.md`
- Modify: `docs/wowdata/capability-inventory.md`

### Task 1: Add Node Capability Scanner

**Files:**
- Create: `tools/node-baseline/capabilities.mjs`
- Test: manual JSON validation

- [ ] **Step 1: Create scanner script**

```js
import fs from 'node:fs';
import path from 'node:path';

const root = process.cwd();
const files = [
  'index.js',
  ...fs.readdirSync(path.join(root, 'core', 'casc')).map(f => `core/casc/${f}`),
  ...fs.readdirSync(path.join(root, 'core', 'db')).filter(f => f.endsWith('.js')).map(f => `core/db/${f}`),
  ...fs.readdirSync(path.join(root, 'core', 'db', 'caches')).map(f => `core/db/caches/${f}`),
];

const capabilities = files.map(file => ({
  file,
  exports: fs.readFileSync(path.join(root, file), 'utf8')
    .split(/\r?\n/)
    .filter(line => line.includes('module.exports') || line.match(/^\s*(async\s+)?[A-Za-z0-9_]+\(/))
    .slice(0, 80),
}));

console.log(JSON.stringify({ ok: true, capabilities }, null, 2));
```

- [ ] **Step 2: Run scanner**

Run: `node tools/node-baseline/capabilities.mjs`

Expected: JSON prints file-level capability hints for `index.js`, `core/casc`, `core/db`, and `core/db/caches`.

- [ ] **Step 3: Commit**

```bash
git add tools/node-baseline/capabilities.mjs
git commit -m "test: add node capability scanner"
```

### Task 2: Add Golden Manifest

**Files:**
- Create: `fixtures/golden/node/manifest.json`

- [ ] **Step 1: Create manifest**

```json
{
  "version": 1,
  "source": "node",
  "requiredGroups": [
    "warmup",
    "db2",
    "spell",
    "encounter",
    "file",
    "icon",
    "casc",
    "item",
    "creature",
    "decor",
    "video",
    "errors"
  ],
  "fixtures": []
}
```

- [ ] **Step 2: Validate JSON**

Run: `node -e "JSON.parse(require('fs').readFileSync('fixtures/golden/node/manifest.json','utf8')); console.log('ok')"`

Expected: `ok`.

- [ ] **Step 3: Commit**

```bash
git add fixtures/golden/node/manifest.json
git commit -m "test: add node golden manifest"
```

### Task 3: Document Baseline Capture Rules

**Files:**
- Create: `docs/wowdata/node-baseline.md`

- [ ] **Step 1: Create baseline doc**

```markdown
# Node Baseline Capture

The Node implementation is the source of truth for capability parity until Go `wowdata` replaces it.

## Rules

- Capture business output, not incidental console noise.
- Include command args, region, product, build, cache state, and fixture name.
- Store generated artifact hashes instead of large binaries.
- Record only non-business intentional differences before accepting a Go mismatch.
- Reject any missing business capability except MCP transport itself.

## Required First Fixtures

- warmup prompt without args
- warmup remote product selection
- db2 schema for `SpellName`
- db2 rows for a known ID
- file lookup for a known fileDataID
- missing warmup error
```

- [ ] **Step 2: Commit**

```bash
git add docs/wowdata/node-baseline.md
git commit -m "docs: describe node baseline capture"
```

### Task 4: Verify Phase 1

- [ ] **Step 1: Run scanner**

Run: `node tools/node-baseline/capabilities.mjs`

Expected: JSON output with `"ok": true`.

- [ ] **Step 2: Validate manifest**

Run: `node -e "JSON.parse(require('fs').readFileSync('fixtures/golden/node/manifest.json','utf8')); console.log('ok')"`

Expected: `ok`.

- [ ] **Step 3: Check status**

Run: `git status --short`

Expected: clean working tree.
