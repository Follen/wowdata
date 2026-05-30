# wowdata Phase 8 Items, Creatures, and Decor Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement all Node cache-module business capabilities for items, item models, geosets, textures, creatures, legacy creatures, and decor.

**Architecture:** Port each Node cache module into a service with explicit initialization, then expose stable CLI commands. Cache services must be lazy, idempotent, and backed by DB2/DBC readers from earlier phases.

**Tech Stack:** Go, DB2/DBC services, lazy caches, golden JSON comparison.

---

## File Structure

- Create: `internal/wowdata/items.go`
- Create: `internal/wowdata/item_models.go`
- Create: `internal/wowdata/item_geosets.go`
- Create: `internal/wowdata/item_textures.go`
- Create: `internal/wowdata/creatures.go`
- Create: `internal/wowdata/creatures_legacy.go`
- Create: `internal/wowdata/decor.go`
- Create: `internal/wowdata/equipment_slots.go`
- Create: corresponding `*_test.go` files
- Modify: `internal/app/commands.go`

### Task 1: Port Item Summary and Slot Logic

- [ ] Port `DBItems.js`.
- [ ] Port `EquipmentSlots.js` and `ItemSlot.js` helpers.
- [ ] Implement `item get`.
- [ ] Compare with Node fixture.

### Task 2: Port Item Models and Textures

- [ ] Port `DBModelFileData.js`, `DBTextureFileData.js`, `DBComponentModelFileData.js`.
- [ ] Port `DBItemModels.js`.
- [ ] Port `DBItemCharTextures.js`.
- [ ] Implement `item models` and `item textures`.
- [ ] Compare race/gender selection with Node.

### Task 3: Port Item Geosets

- [ ] Port `DBItemGeosets.js`.
- [ ] Implement `item geosets`.
- [ ] Preserve affected char geoset and helmet-hide semantics.
- [ ] Compare with Node fixture.

### Task 4: Port Creatures

- [ ] Port `DBCreatures.js`.
- [ ] Port `DBCreaturesLegacy.js`.
- [ ] Implement `creature display` and `creature model`.
- [ ] Compare modern and legacy paths where fixtures exist.

### Task 5: Port Decor

- [ ] Port `DBDecor.js`.
- [ ] Implement `decor list` and `decor get`.
- [ ] Compare with Node fixture.

### Task 6: Verify Phase 8

- [ ] Run: `go test ./internal/wowdata ./...`
- [ ] Run representative item, creature, and decor commands.
- [ ] Update golden manifest.
- [ ] Commit: `git commit -m "feat: add item creature and decor commands"`
