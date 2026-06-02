package wowdata

import "testing"

type spellDB2TestStore struct {
	rows map[string][]map[string]interface{}
}

func (s spellDB2TestStore) Rows(table string, ids []uint32, fields []string, filter string, limit int) ([]map[string]interface{}, error) {
	return s.rows[table], nil
}

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
	if _, ok := info.Spells[400]; !ok {
		t.Fatal("spell 400 not found in chain")
	}
	if len(info.Triggers[200]) != 1 || info.Triggers[200][0] != 400 {
		t.Fatalf("wrong trigger chain: %#v", info.Triggers)
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

func TestSummonDetectionUsesFirstDB2ArrayMiscValue(t *testing.T) {
	svc := NewSpellService()
	svc.SetEffects(map[uint32][]map[string]interface{}{
		100: {
			{"Effect": uint32(28), "EffectMiscValue": []uint32{12345, 61}, "EffectIndex": uint32(0)},
		},
	})

	summons := svc.DetectSummons(100, 12345)
	if len(summons) != 1 {
		t.Fatalf("expected array EffectMiscValue to match npcID, got %#v", summons)
	}
	if summons[0].NPCID != 12345 {
		t.Fatalf("npcID = %d", summons[0].NPCID)
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

func TestRowIntSliceConvertsFloatArrays(t *testing.T) {
	for name, row := range map[string]map[string]interface{}{
		"float32":   {"RangeMax": []float32{25, 25}},
		"interface": {"RangeMax": []interface{}{float32(25), float32(25)}},
	} {
		got := rowIntSlice(row, "RangeMax")
		if len(got) != 2 || got[0] != 25 || got[1] != 25 {
			t.Fatalf("%s rowIntSlice float array = %#v", name, got)
		}
	}
}

func TestSpellServiceUsesDB2SpellEffectRows(t *testing.T) {
	svc := NewSpellServiceWithDB2(spellDB2TestStore{rows: map[string][]map[string]interface{}{
		"SpellEffect": {
			{"SpellID": uint32(100), "EffectTriggerSpell": uint32(200)},
			{"SpellID": uint32(200), "Effect": uint32(64), "EffectMiscValue": uint32(300)},
			{"SpellID": uint32(100), "Effect": uint32(6)},
			{"SpellID": uint32(100), "Effect": uint32(28), "EffectMiscValue": uint32(12345), "EffectIndex": uint32(1)},
		},
	}})

	info := svc.GetSpellInfo(100, 5)
	if info.ChainDepth != 3 {
		t.Fatalf("chainDepth = %d, want 3", info.ChainDepth)
	}
	if len(info.Triggers[100]) != 1 || info.Triggers[100][0] != 200 || len(info.Triggers[200]) != 1 || info.Triggers[200][0] != 300 {
		t.Fatalf("triggers = %#v", info.Triggers)
	}
	if aura := svc.DetectAuras(100); !aura.HasAura || aura.NoAura {
		t.Fatalf("aura result = %#v", aura)
	}
	summons := svc.DetectSummons(100, 12345)
	if len(summons) != 1 || summons[0].NPCID != 12345 || summons[0].EffectIndex != 1 {
		t.Fatalf("summons = %#v", summons)
	}
}
