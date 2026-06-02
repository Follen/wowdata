package runtime

import "testing"

func TestLocalRuntimeDoesNotEnableHTTPFeatures(t *testing.T) {
	rt := NewLocalRuntime(LocalRuntimeOptions{CacheRoot: "cache"})
	if rt.HTTPEnabled() {
		t.Fatal("local runtime must not enable HTTP service features")
	}
	if rt.CacheRoot() != "cache" {
		t.Fatalf("cache root = %q, want cache", rt.CacheRoot())
	}
}

func TestLocalRuntimeSingleActiveContext(t *testing.T) {
	rt := NewLocalRuntime(LocalRuntimeOptions{CacheRoot: "cache"})
	rt.SetActiveContext(&Context{Source: "remote", Region: "cn", Product: "wow", BuildKey: "a", Locale: "zhCN"})
	rt.SetActiveContext(&Context{Source: "remote", Region: "cn", Product: "wowt", BuildKey: "b", Locale: "zhCN"})

	active := rt.ActiveContext()
	if active.Product != "wowt" || active.BuildKey != "b" {
		t.Fatalf("active context = %#v, want wowt build b", active)
	}
}

func TestLocalRuntimeActiveContextReturnsCopy(t *testing.T) {
	rt := NewLocalRuntime(LocalRuntimeOptions{CacheRoot: "cache"})
	rt.SetActiveContext(&Context{Product: "wowt", BuildKey: "b"})

	active := rt.ActiveContext()
	active.Product = "polluted"
	active.BuildKey = "changed"

	active = rt.ActiveContext()
	if active.Product != "wowt" || active.BuildKey != "b" {
		t.Fatalf("active context was mutated through returned pointer: %#v", active)
	}
}

func TestLocalRuntimeActiveContextCopiesTablesReady(t *testing.T) {
	rt := NewLocalRuntime(LocalRuntimeOptions{CacheRoot: "cache"})
	ctx := &Context{
		Product:     "wowt",
		BuildKey:    "b",
		TablesReady: map[string]bool{"SpellName": true},
	}

	rt.SetActiveContext(ctx)
	ctx.TablesReady["Injected"] = true

	active := rt.ActiveContext()
	if active.HasTable("Injected") {
		t.Fatal("active context included table injected through original context map")
	}

	active.TablesReady["InjectedReturn"] = true

	active = rt.ActiveContext()
	if active.HasTable("InjectedReturn") {
		t.Fatal("active context included table injected through returned context map")
	}
}
