package app

import "github.com/spf13/cobra"

type Service struct {
	Warmup    func(cmd *cobra.Command, args []string) error
	Casc      func(cmd *cobra.Command, args []string) error
	DB2       func(cmd *cobra.Command, args []string) error
	Spell     func(cmd *cobra.Command, args []string) error
	Encounter func(cmd *cobra.Command, args []string) error
	File      func(cmd *cobra.Command, args []string) error
	Icon      func(cmd *cobra.Command, args []string) error
	Item      func(cmd *cobra.Command, args []string) error
	Creature  func(cmd *cobra.Command, args []string) error
	Decor     func(cmd *cobra.Command, args []string) error
	Video     func(cmd *cobra.Command, args []string) error
	Golden    func(cmd *cobra.Command, args []string) error
}

type cmdFlag struct {
	name  string
	typ   string // "uint32", "int", "string", "bool"
	short string
	dflt  string
	desc  string
}

type commandSpec struct {
	use     string
	short   string
	example string
	group   string
	flags   []cmdFlag
	child   []commandSpec
}

func registerCommands(root *cobra.Command, svc *Service) {
	specs := []commandSpec{
		{use: "warmup", short: "Initialize local or remote WoW data context.", example: "  wowdata warmup --source remote --region cn --product wow", group: "warmup",
			flags: []cmdFlag{
				{name: "source", typ: "string", dflt: "remote", desc: "Data source: local or remote"},
				{name: "path", typ: "string", desc: "Local WoW client path when source=local"},
				{name: "region", typ: "string", dflt: "cn", desc: "WoW region"},
				{name: "product", typ: "string", dflt: "wow", desc: "WoW product"},
				{name: "locale", typ: "string", dflt: "zhCN", desc: "WoW locale, such as zhCN or enUS"},
				{name: "cache", typ: "string", desc: "Cache directory; defaults to cache next to the executable"},
				{name: "tables", typ: "string", dflt: "SpellName,Spell,SpellEffect,SpellMisc,SpellCastTimes,SpellDuration,SpellRange,JournalEncounterSection", desc: "Comma-separated DB2 tables to preload"},
				{name: "listfile", typ: "bool", dflt: "true", desc: "Warm listfile cache"},
				{name: "listfile-format", typ: "string", dflt: "binary", desc: "Listfile source format: binary for current full listfile, text for community CSV listfile"},
				{name: "dbd-manifest", typ: "bool", dflt: "true", desc: "Warm DBD manifest cache"},
			}},
		{use: "query", short: "Query DB2 tables.", example: "  wowdata query rows SpellName --id 123", group: "db2", child: []commandSpec{
			{use: "schema <table>", short: "Print parsed schema metadata.", example: "  wowdata query schema SpellName", group: "db2"},
			{use: "rows <table>", short: "Fetch rows by ID, fields, filter, and limit.", example: "  wowdata query rows SpellName --id 123 --limit 1", group: "db2",
				flags: []cmdFlag{
					{name: "id", typ: "string", desc: "Record ID, repeat as comma-separated values for multiple IDs"},
					{name: "ids", typ: "string", desc: "Comma-separated record IDs"},
					{name: "fields", typ: "string", desc: "Comma-separated fields to include"},
					{name: "filter", typ: "string", desc: "Filter expression in field=value form"},
					{name: "limit", typ: "int", desc: "Max rows"},
				}},
			{use: "search <table>", short: "Search a field case-insensitively.", example: "  wowdata query search SpellName --field Name_lang --query fire", group: "db2",
				flags: []cmdFlag{
					{name: "field", typ: "string", desc: "Field to search"},
					{name: "query", typ: "string", desc: "Search query"},
					{name: "limit", typ: "int", desc: "Max rows"},
				}},
			{use: "foreign-key <table>", short: "Query rows by foreign key relationship.", example: "  wowdata query foreign-key SpellEffect --field SpellID --value 123", group: "db2",
				flags: []cmdFlag{
					{name: "field", typ: "string", desc: "Foreign key field"},
					{name: "value", typ: "uint32", desc: "Foreign key value"},
				}},
			{use: "stream <table>", short: "Stream large table rows as JSON lines.", example: "  wowdata query stream SpellEffect --limit 100", group: "db2",
				flags: []cmdFlag{
					{name: "fields", typ: "string", desc: "Comma-separated fields to include"},
					{name: "filter", typ: "string", desc: "Filter expression in field=value form"},
					{name: "limit", typ: "int", desc: "Max rows"},
					{name: "format", typ: "string", dflt: "jsonl", desc: "Output format: jsonl for one JSON object per line, or json for one JSON response"},
				}},
		}},
		{use: "spell", short: "Inspect spell relationships.", example: "  wowdata spell info --spell-id 123", group: "spell", child: []commandSpec{
			{use: "info", short: "Inspect spell trigger chains and description references.", example: "  wowdata spell info --spell-id 123 --max-depth 5", group: "spell",
				flags: []cmdFlag{
					{name: "spell-id", typ: "uint32", desc: "Spell ID to inspect"},
					{name: "max-depth", typ: "int", desc: "Maximum traversal depth"},
				}},
			{use: "auras", short: "Detect aura presence for spells.", example: "  wowdata spell auras --spell-id 123", group: "spell",
				flags: []cmdFlag{
					{name: "spell-id", typ: "uint32", desc: "Spell ID to check"},
				}},
			{use: "summons", short: "Detect NPC summons from spell effects.", example: "  wowdata spell summons --spell-id 123 --npc-id 456", group: "spell",
				flags: []cmdFlag{
					{name: "spell-id", typ: "uint32", desc: "Spell ID to check"},
					{name: "npc-id", typ: "uint32", desc: "NPC ID filter (optional)"},
				}},
		}},
		{use: "encounter", short: "Query JournalEncounter data.", example: "  wowdata encounter get --journal-encounter-id 123", group: "encounter", child: []commandSpec{
			{use: "get", short: "Return section tree and related spell IDs.", example: "  wowdata encounter get --journal-encounter-id 123", group: "encounter",
				flags: []cmdFlag{
					{name: "journal-encounter-id", typ: "uint32", desc: "Journal encounter ID"},
				}},
		}},
		{use: "file", short: "Query and export CASC files.", example: "  wowdata file lookup --file-data-id 456", group: "file", child: []commandSpec{
			{use: "lookup", short: "Resolve fileDataID to filename.", example: "  wowdata file lookup --file-data-id 456", group: "file",
				flags: []cmdFlag{{name: "file-data-id", typ: "uint32", desc: "File data ID to resolve"}}},
			{use: "search", short: "Search listfile entries.", example: "  wowdata file search --query interface/icons --limit 50", group: "file",
				flags: []cmdFlag{
					{name: "query", typ: "string", desc: "Search query"},
					{name: "limit", typ: "int", desc: "Max entries"},
				}},
			{use: "extension", short: "List files by extension.", example: "  wowdata file extension --extension blp --limit 50", group: "file",
				flags: []cmdFlag{
					{name: "extension", typ: "string", desc: "File extension (e.g. blp, m2)"},
					{name: "limit", typ: "int", desc: "Max entries"},
				}},
			{use: "get", short: "Fetch a raw CASC file by ID or name.", example: "  wowdata file get --file-data-id 456 --output out.bin", group: "file",
				flags: []cmdFlag{
					{name: "file-data-id", typ: "uint32", desc: "File data ID"},
					{name: "filename", typ: "string", desc: "File name"},
					{name: "output", typ: "string", desc: "Output path"},
				}},
			{use: "exists", short: "Check whether a file exists.", example: "  wowdata file exists --file-data-id 456", group: "file",
				flags: []cmdFlag{
					{name: "file-data-id", typ: "uint32", desc: "File data ID"},
					{name: "filename", typ: "string", desc: "File name"},
				}},
			{use: "encoding", short: "Inspect content key and encoding key metadata.", example: "  wowdata file encoding --file-data-id 456", group: "file",
				flags: []cmdFlag{{name: "file-data-id", typ: "uint32", desc: "File data ID"}}},
			{use: "export", short: "Write raw CASC files to disk.", example: "  wowdata file export --file-data-id 456 --output output/file.bin", group: "file",
				flags: []cmdFlag{
					{name: "file-data-id", typ: "uint32", desc: "File data ID"},
					{name: "filename", typ: "string", desc: "File name"},
					{name: "output", typ: "string", desc: "Output file path"},
				}},
		}},
		{use: "icon", short: "Export BLP textures.", example: "  wowdata icon export --file-data-id 789 --format png", group: "icon", child: []commandSpec{
			{use: "export", short: "Export BLP as PNG or WebP.", example: "  wowdata icon export --file-data-id 789 --format png --mipmap 0", group: "icon",
				flags: []cmdFlag{
					{name: "file-data-id", typ: "uint32", desc: "File data ID of BLP texture"},
					{name: "format", typ: "string", dflt: "png", desc: "Output format: png or lossless webp"},
					{name: "mipmap", typ: "int", desc: "Mipmap level to export"},
					{name: "mask", typ: "int", dflt: "15", desc: "Channel mask for export compatibility"},
					{name: "output", typ: "string", desc: "Output file path"},
				}},
		}},
		{use: "casc", short: "Inspect CASC source state.", example: "  wowdata casc info", group: "casc", child: []commandSpec{
			{use: "info", short: "Show current build and cache state.", example: "  wowdata casc info", group: "casc"},
			{use: "products", short: "List available products and builds.", example: "  wowdata casc products --source remote --region cn", group: "casc",
				flags: []cmdFlag{
					{name: "source", typ: "string", dflt: "remote", desc: "Data source"},
					{name: "path", typ: "string", desc: "Local WoW client path when source=local"},
					{name: "region", typ: "string", dflt: "cn", desc: "WoW region"},
				}},
			{use: "diagnose", short: "Inspect CDN, archive, root, encoding, cache, and TACT state.", example: "  wowdata casc diagnose", group: "casc"},
		}},
		{use: "item", short: "Query item metadata and assets.", example: "  wowdata item get --item-id 19019", group: "item", child: []commandSpec{
			{use: "get", short: "Return item summary and slot information.", example: "  wowdata item get --item-id 19019", group: "item",
				flags: []cmdFlag{{name: "item-id", typ: "uint32", desc: "Item ID"}}},
			{use: "models", short: "Return model fileDataIDs and textures.", example: "  wowdata item models --item-id 19019 --race-id 1 --gender 0", group: "item",
				flags: []cmdFlag{
					{name: "item-id", typ: "uint32", desc: "Item ID"},
					{name: "race-id", typ: "int", desc: "Race ID (default 1 Human)"},
					{name: "gender", typ: "int", desc: "Gender index (0=male, 1=female)"},
				}},
			{use: "geosets", short: "Return geoset and helmet-hide data.", example: "  wowdata item geosets --item-id 19019", group: "item",
				flags: []cmdFlag{{name: "item-id", typ: "uint32", desc: "Item ID"}}},
			{use: "textures", short: "Return character texture fileDataIDs.", example: "  wowdata item textures --item-id 19019", group: "item",
				flags: []cmdFlag{{name: "item-id", typ: "uint32", desc: "Item ID"}}},
		}},
		{use: "creature", short: "Query creature displays and models.", example: "  wowdata creature display --display-id 123", group: "creature", child: []commandSpec{
			{use: "display", short: "Query creature display metadata.", example: "  wowdata creature display --display-id 123", group: "creature",
				flags: []cmdFlag{
					{name: "display-id", typ: "uint32", desc: "Creature display ID"},
					{name: "file-data-id", typ: "uint32", desc: "Model file data ID"},
				}},
			{use: "model", short: "Query creature model fileDataIDs and variants.", example: "  wowdata creature model --file-data-id 456", group: "creature",
				flags: []cmdFlag{{name: "file-data-id", typ: "uint32", desc: "Model file data ID"}}},
		}},
		{use: "decor", short: "Query decor data.", example: "  wowdata decor list", group: "decor", child: []commandSpec{
			{use: "list", short: "List decor entries.", example: "  wowdata decor list --limit 50", group: "decor",
				flags: []cmdFlag{{name: "limit", typ: "int", desc: "Max entries"}}},
			{use: "get", short: "Query decor item by ID or model fileDataID.", example: "  wowdata decor get --id 123", group: "decor",
				flags: []cmdFlag{
					{name: "id", typ: "uint32", desc: "Decor item ID"},
					{name: "model-file-data-id", typ: "uint32", desc: "Model file data ID"},
				}},
		}},
		{use: "video", short: "Process video container data from WoW files.", example: "  wowdata video demux --input movie.avi --output frames", group: "video", child: []commandSpec{
			{use: "demux", short: "Inspect VP9 AVI container frames.", example: "  wowdata video demux --input movie.avi --output frames", group: "video",
				flags: []cmdFlag{
					{name: "input", typ: "string", desc: "Input AVI file path"},
					{name: "output", typ: "string", desc: "Output directory for frames"},
				}},
		}},
		{use: "golden", short: "Capture and compare golden fixtures.", example: "  wowdata golden compare --fixture warmup/remote-cn-wow.json", group: "golden", child: []commandSpec{
			{use: "capture", short: "Capture Go command output.", example: "  wowdata golden capture --name db2-spellname-123 -- wowdata query rows SpellName --id 123", group: "golden",
				flags: []cmdFlag{
					{name: "name", typ: "string", desc: "Fixture name"},
					{name: "command", typ: "string", desc: "Command to execute and capture"},
					{name: "output", typ: "string", desc: "Fixture output path"},
					{name: "manifest", typ: "string", desc: "Golden manifest path to update"},
				}},
			{use: "compare", short: "Compare Go command output against a fixture.", example: "  wowdata golden compare --fixture fixtures/golden/db2-spellname-123.json", group: "golden",
				flags: []cmdFlag{
					{name: "fixture", typ: "string", desc: "Fixture path"},
					{name: "actual", typ: "string", desc: "Actual Go JSON output path"},
					{name: "all", typ: "bool", desc: "Compare all fixtures in manifest"},
				}},
		}},
	}

	for _, spec := range specs {
		root.AddCommand(buildCommand(spec, svc))
	}
}

func buildCommand(spec commandSpec, svc *Service) *cobra.Command {
	cmd := &cobra.Command{
		Use:          spec.use,
		Short:        spec.short,
		Example:      spec.example,
		SilenceUsage: true,
		RunE:         resolveHandler(spec, svc),
	}
	for _, f := range spec.flags {
		switch f.typ {
		case "uint32":
			cmd.Flags().Uint32(f.name, 0, f.desc)
		case "int":
			cmd.Flags().Int(f.name, 0, f.desc)
		case "string":
			cmd.Flags().String(f.name, f.dflt, f.desc)
		case "bool":
			cmd.Flags().Bool(f.name, f.dflt == "true", f.desc)
		}
	}
	for _, child := range spec.child {
		cmd.AddCommand(buildCommand(child, svc))
	}
	return cmd
}

func resolveHandler(spec commandSpec, svc *Service) func(cmd *cobra.Command, args []string) error {
	handler := unavailableHandler(spec.use)
	if svc == nil {
		return handler
	}
	switch spec.group {
	case "warmup":
		if svc.Warmup != nil {
			return svc.Warmup
		}
	case "casc":
		if svc.Casc != nil {
			return svc.Casc
		}
	case "db2":
		if svc.DB2 != nil {
			return svc.DB2
		}
	case "spell":
		if svc.Spell != nil {
			return svc.Spell
		}
	case "encounter":
		if svc.Encounter != nil {
			return svc.Encounter
		}
	case "file":
		if svc.File != nil {
			return svc.File
		}
	case "icon":
		if svc.Icon != nil {
			return svc.Icon
		}
	case "item":
		if svc.Item != nil {
			return svc.Item
		}
	case "creature":
		if svc.Creature != nil {
			return svc.Creature
		}
	case "decor":
		if svc.Decor != nil {
			return svc.Decor
		}
	case "video":
		if svc.Video != nil {
			return svc.Video
		}
	case "golden":
		if svc.Golden != nil {
			return svc.Golden
		}
	}
	return handler
}
