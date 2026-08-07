# Independent Hotfix queries

Use `wowdata hotfix query`; do not express Hotfix lookups as DB2 SQL. The default source is Wago; select `dbcache` for a local `DBCache.bin` or `raidbots` for a recent snapshot.

```powershell
wowdata hotfix query --source dbcache --dbcache <DBCache.bin> --product <product> --build <build> --region <id> --locale <locale> --table <table> --record <id> --raw
wowdata hotfix query --source wago --product wow_classic_titan --build 3.80.2.69137 --region 196 --locale zhCN --table SpellPowerDifficulty --latest --format json
```

Useful filters: `--table`, `--record`, `--push`, `--status`, `--search`, `--from`, `--to`, `--limit`, `--page`; `--decoded --dbd <definition>` adds decoded fields when the matching DBD is available. `--latest` returns the complete maximum-PushID batch. Output formats are `json`, `jsonl`, and `csv`; `--raw` preserves payloads.

Wago `--search` is candidate acceleration, not exact filtering. The adapter must exact-filter product, Build, region, locale, table, record, and status after parsing. A zero candidate page is not proof of a complete miss. Preserve raw status and PushID ordering; report provider, filters, page/coverage, and cache provenance.

Prefer local DBCache disk point lookups for immediate local data. Use Wago for remote history/complementary records and Raidbots only for recent snapshots. Do not route Hotfix through the static SQL path.
