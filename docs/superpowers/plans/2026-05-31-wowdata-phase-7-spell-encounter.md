# wowdata Phase 7 Spell and Encounter Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement spell chain, aura, summon, and encounter section-tree commands with Node-equivalent business output.

**Architecture:** Build on Phase 4 DB2 queries. Implement services in `internal/wowdata` that are independent of CLI parsing, then connect `spell` and `encounter` commands.

**Tech Stack:** Go, DB2 services, regex parsing, golden JSON comparison.

---

## Hard Constraints

- Do not leave `TODO`, `TBD`, placeholder text, fake implementations, empty functions, unconditional success paths, or "fill this in later" work in any plan output or implementation.
- Do not mark a capability complete until it has real behavior, tests, and golden coverage where Node parity applies.
- Do not skip hard parser/export behavior by returning mock JSON, static fixtures, or hand-written sample data.
- Do not weaken Node parity. Every non-MCP Node business capability must have an equivalent `wowdata` CLI path.
- Phase 0 may introduce explicit `not_implemented` command responses only as a temporary command-surface scaffold. Later phases must remove those responses for their owned commands before claiming completion.
- If an agent cannot implement a capability, it must stop and report the blocker instead of silently substituting a placeholder.

## File Structure

- Create: `internal/wowdata/spell.go`
- Create: `internal/wowdata/spell_test.go`
- Create: `internal/wowdata/encounter.go`
- Create: `internal/wowdata/encounter_test.go`
- Modify: `internal/app/commands.go`

### Task 1: Implement Spell Info

- [ ] Port trigger graph traversal from Node `batchSpellInfo`.
- [ ] Port description reference regex behavior.
- [ ] Preserve `seedCount`, `totalCount`, `chainDepth`, `triggers`, `descRefs`, and `spells` business semantics.
- [ ] Compare with Node fixture.

### Task 2: Implement Aura Detection

- [ ] Query `SpellEffect`.
- [ ] Return `hasAura` and `noAura`.
- [ ] Compare with Node fixture.

### Task 3: Implement Summon Detection

- [ ] Query `SpellEffect`.
- [ ] Detect effect 28 with matching spell and NPC IDs.
- [ ] Return summon rows with spellID, npcID, effectIndex, and row.
- [ ] Compare with Node fixture.

### Task 4: Implement Encounter Tree

- [ ] Query `JournalEncounterSection`.
- [ ] Build parent/child tree.
- [ ] Preserve ordering by `orderIndex`.
- [ ] Return `journalEncounterID`, `sectionCount`, `spellCount`, `spellIds`, and `sections`.
- [ ] Compare with Node fixture.

### Task 5: Wire CLI Commands

- [ ] Replace `spell info`, `spell auras`, `spell summons`, and `encounter get` stubs.
- [ ] Add command tests for required args and JSON envelope.
- [ ] Update golden manifest.
- [ ] Commit: `git commit -m "feat: add spell and encounter commands"`
