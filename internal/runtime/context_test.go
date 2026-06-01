package runtime

import "testing"

func TestContextIdentityReadyState(t *testing.T) {
	ctx := &Context{
		Source:    "remote",
		Region:    "cn",
		Product:   "wow",
		BuildName: "12.0.0.61234",
		BuildKey:  "abcd",
		Locale:    "zhCN",
	}
	if ctx.Ready() {
		t.Fatal("context without CASC metadata must not be ready")
	}

	ctx.CASCReady = true
	ctx.DBDReady = true
	ctx.TablesReady = map[string]bool{"SpellName": true}
	if !ctx.Ready() {
		t.Fatal("context with CASC, DBD, and table state should be ready")
	}
	if !ctx.HasTable("SpellName") {
		t.Fatal("context should report ready SpellName table")
	}
	if ctx.HasTable("Missing") {
		t.Fatal("context should not report missing table as ready")
	}
}
