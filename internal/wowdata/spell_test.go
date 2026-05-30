package wowdata

import "testing"

func TestSpellTraversal(t *testing.T) {
	svc := NewSpellService()
	svc.SetTriggers(map[uint32][]uint32{
		100: {200, 300},
		200: {400},
	})

	info := svc.GetSpellInfo(100, 5)
	if info.ChainDepth < 2 {
		t.Fatalf("chainDepth = %d", info.ChainDepth)
	}
	found400 := false
	for _, n := range info.Spells {
		if n.SpellID == 400 {
			found400 = true
			if n.ParentID != 200 {
				t.Fatal("wrong parent for 400")
			}
		}
	}
	if !found400 {
		t.Fatal("spell 400 not found in chain")
	}
}

func TestSpellCycleDetection(t *testing.T) {
	svc := NewSpellService()
	svc.SetTriggers(map[uint32][]uint32{
		1: {2},
		2: {1},
	})

	info := svc.GetSpellInfo(1, 10)
	if info.TotalCount <= 0 {
		t.Fatal("should have spells without infinite loop")
	}
}

func TestAuraDetection(t *testing.T) {
	svc := NewSpellService()
	svc.SetEffects(map[uint32][]map[string]interface{}{
		100: {{"Effect": uint32(6), "Aura": uint32(137)}},
	})

	result := svc.DetectAuras(100)
	if !result.HasAura {
		t.Fatal("should detect aura")
	}
	if result.NoAura {
		t.Fatal("noAura should be false")
	}

	result = svc.DetectAuras(999)
	if result.HasAura {
		t.Fatal("missing spell should not have aura")
	}
}

func TestSummonDetection(t *testing.T) {
	svc := NewSpellService()
	svc.SetEffects(map[uint32][]map[string]interface{}{
		100: {
			{"Effect": uint32(28), "EffectMiscValue": uint32(12345), "EffectIndex": uint32(0)},
			{"Effect": uint32(6), "EffectMiscValue": uint32(0)}, // Not a summon
		},
	})

	summons := svc.DetectSummons(100, 0)
	if len(summons) != 1 {
		t.Fatalf("expected 1 summon, got %d", len(summons))
	}
	if summons[0].NPCID != 12345 {
		t.Fatalf("npcID = %d", summons[0].NPCID)
	}

	filtered := svc.DetectSummons(100, 12345)
	if len(filtered) != 1 {
		t.Fatal("should filter by npcID")
	}
}

func TestNoTriggers(t *testing.T) {
	svc := NewSpellService()
	info := svc.GetSpellInfo(1, 5)
	if len(info.Triggers) != 0 {
		t.Fatal("should have no triggers")
	}
	if info.ChainDepth != 0 {
		t.Fatal("chain depth should be 0")
	}
}
