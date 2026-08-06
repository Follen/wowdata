package main

import (
	"strings"

	"wowdata/internal/resource"

	"github.com/spf13/cobra"
)

var accountingCoverage = map[string][]string{
	"warmup":           work("casc-metadata-parse", "file-read", "file-write", "listfile-parse", "sha256", "dbd-parse", "wdc-open", "wdc-row-decode"),
	"casc diagnose":    work("casc-metadata-parse", "file-read", "sha256"),
	"casc info":        work("casc-metadata-parse", "file-read"),
	"casc products":    work("casc-metadata-parse", "file-read"),
	"db2 schema":       db2Work(),
	"db2 rows":         db2Work("wdc-row-visit", "wdc-project", "dbc-row-visit", "dbc-project"),
	"db2 search":       db2Work("wdc-row-visit", "wdc-predicate", "wdc-project", "dbc-row-visit", "dbc-predicate", "dbc-project"),
	"db2 foreign-key":  db2Work("wdc-relationship", "wdc-row-visit", "dbc-relationship", "dbc-row-visit"),
	"db2 stream":       db2Work("wdc-row-visit", "wdc-predicate", "wdc-project", "dbc-row-visit", "dbc-predicate", "dbc-project"),
	"spell info":       db2Work("wdc-row-visit", "wdc-relationship"),
	"spell auras":      db2Work("wdc-row-visit", "wdc-relationship"),
	"spell summons":    db2Work("wdc-row-visit", "wdc-relationship"),
	"encounter get":    db2Work("wdc-row-visit", "wdc-relationship"),
	"encounter export": append(db2Work("wdc-row-visit", "wdc-relationship"), "blp-decode", "png-encode", "sha256", "file-read", "file-write"),
	"file lookup":      work("listfile-parse", "file-read"),
	"file search":      work("listfile-parse", "file-read"),
	"file extension":   work("listfile-parse", "file-read"),
	"file get":         work("casc-metadata-parse", "blte-normal-decode", "blte-zlib-decode", "file-read", "file-write", "sha256"),
	"file exists":      work("casc-metadata-parse", "file-read"),
	"file encoding":    work("casc-metadata-parse", "file-read"),
	"file export":      work("casc-metadata-parse", "blte-normal-decode", "blte-zlib-decode", "file-read", "file-write", "sha256"),
	"icon export":      work("casc-metadata-parse", "blte-normal-decode", "blte-zlib-decode", "blp-decode", "png-encode", "webp-encode", "sha256", "file-read", "file-write"),
	"item get":         db2Work("wdc-row-visit"),
	"item models":      db2Work("wdc-row-visit", "wdc-relationship"),
	"item geosets":     db2Work("wdc-row-visit", "wdc-relationship"),
	"item textures":    db2Work("wdc-row-visit", "wdc-relationship"),
	"creature display": db2Work("wdc-row-visit", "wdc-relationship"),
	"creature model":   db2Work("wdc-row-visit", "wdc-relationship"),
	"decor list":       db2Work("wdc-row-visit"),
	"decor get":        db2Work("wdc-row-visit"),
	"video demux":      work("file-read", "video-demux"),
	"profile list":     work("file-read"),
	"profile show":     work("file-read"),
	"profile set":      work("file-read", "file-write"),
	"profile remove":   work("file-read", "file-write"),
	"cache status":     work("file-read"),
	"cache verify":     work("file-read", "sha256"),
	"cache prune":      work("file-read", "file-write"),
	"cache clear":      work("file-read", "file-write"),
	"cache config":     work("file-read", "file-write"),
	"doctor":           work("file-read", "sha256"),
	"update":           work("file-read", "file-write"),
	"uninstall":        work("file-read", "file-write"),
	"golden capture":   work("file-read", "file-write", "sha256"),
	"golden compare":   work("file-read", "sha256"),
}

func work(classes ...string) []string {
	return append([]string{"json-encode"}, classes...)
}

func db2Work(classes ...string) []string {
	base := work("casc-metadata-parse", "file-read", "file-write", "dbd-parse", "wdc-open", "wdc-row-decode", "dbc-open", "dbc-row-decode")
	return append(base, classes...)
}

func beginCommandAccounting(root *cobra.Command, args []string) {
	leaf, _, err := root.Find(args)
	if err != nil || leaf == nil || leaf.HasSubCommands() {
		resource.BeginWork(false, nil)
		return
	}
	path := strings.TrimSpace(strings.TrimPrefix(leaf.CommandPath(), root.Name()))
	required, registered := accountingCoverage[path]
	resource.BeginWork(registered, required)
}
