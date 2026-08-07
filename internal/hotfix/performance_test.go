package hotfix

import (
	"os"
	"path/filepath"
	"testing"
)

func BenchmarkParseWagoOffline(b *testing.B) {
	raw, err := os.ReadFile(filepath.Join("testdata", "wago-hotfix-offline", "response-page-1.html"))
	if err != nil {
		b.Fatal(err)
	}
	q := Query{Product: "wow_classic_titan", Build: "3.80.2.69137", Region: 196, Locale: "zhCN", Table: "SpellPowerDifficulty", Search: "69137", Page: 1}
	b.ReportAllocs()
	b.SetBytes(int64(len(raw)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, _, err := ParseWagoHTML(raw, q); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkOpenSidecarAndFind(b *testing.B) {
	db := os.Getenv("WOWDATA_BENCH_DBCACHE")
	if db == "" {
		b.Skip("WOWDATA_BENCH_DBCACHE is not set")
	}
	q := Query{Product: "wow", Build: "68887", Region: 5, Locale: "zhCN", Latest: true}
	tableHash, recordID := uint32(3744420815), uint32(8210)
	q.TableHash, q.RecordID = &tableHash, &recordID
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r, err := OpenDBCacheLazy(db)
		if err != nil {
			b.Fatal(err)
		}
		s, err := OpenSidecar(db + ".sidecar")
		if err != nil {
			_ = r.Close()
			b.Fatal(err)
		}
		if _, err = r.QuerySidecar(s, q); err != nil {
			_ = s.Close()
			_ = r.Close()
			b.Fatal(err)
		}
		_ = s.Close()
		_ = r.Close()
	}
}
