package app

import "github.com/spf13/cobra"

// Service holds injected handlers for command groups.
// Each field is a handler that, when non-nil, replaces the not_implemented
// fallback for that group. Future phases set fields to wire real CASC/HTTP
// logic without touching the command tree definition.
type Service struct {
	Warmup func(cmd *cobra.Command, args []string) error
	Casc   func(cmd *cobra.Command, args []string) error
	DB2    func(cmd *cobra.Command, args []string) error
}

type commandSpec struct {
	use     string
	short   string
	example string
	group   string // command group name for handler dispatch (e.g. "warmup", "casc")
	child   []commandSpec
}

func registerCommands(root *cobra.Command, svc *Service) {
	specs := []commandSpec{
		{use: "warmup", short: "Initialize local or remote WoW data context.", example: "  wowdata warmup --source remote --region cn --product wow", group: "warmup"},
		{use: "db2", short: "Query DB2 tables.", example: "  wowdata db2 rows SpellName --id 123", group: "db2", child: []commandSpec{
			{use: "schema <table>", short: "Print parsed schema metadata.", example: "  wowdata db2 schema SpellName", group: "db2"},
			{use: "rows <table>", short: "Fetch rows by ID, fields, filter, and limit.", example: "  wowdata db2 rows SpellName --id 123 --limit 1", group: "db2"},
			{use: "search <table>", short: "Search a field case-insensitively.", example: "  wowdata db2 search SpellName --field Name_lang --query fire", group: "db2"},
			{use: "foreign-key <table>", short: "Query rows by foreign key relationship.", example: "  wowdata db2 foreign-key SpellEffect --field SpellID --value 123", group: "db2"},
			{use: "stream <table>", short: "Stream large table rows as JSON lines.", example: "  wowdata db2 stream SpellEffect --limit 100", group: "db2"},
		}},
		{use: "spell", short: "Inspect spell relationships.", example: "  wowdata spell info --spell-id 123", child: []commandSpec{
			{use: "info", short: "Inspect spell trigger chains and description references.", example: "  wowdata spell info --spell-id 123 --max-depth 5"},
			{use: "auras", short: "Detect aura presence for spells.", example: "  wowdata spell auras --spell-id 123"},
			{use: "summons", short: "Detect NPC summons from spell effects.", example: "  wowdata spell summons --spell-id 123 --npc-id 456"},
		}},
		{use: "encounter", short: "Query JournalEncounter data.", example: "  wowdata encounter get --journal-encounter-id 123", child: []commandSpec{
			{use: "get", short: "Return section tree and related spell IDs.", example: "  wowdata encounter get --journal-encounter-id 123"},
		}},
		{use: "file", short: "Query and export CASC files.", example: "  wowdata file lookup --file-data-id 456", child: []commandSpec{
			{use: "lookup", short: "Resolve fileDataID to filename.", example: "  wowdata file lookup --file-data-id 456"},
			{use: "search", short: "Search listfile entries.", example: "  wowdata file search --query interface/icons"},
			{use: "extension", short: "List files by extension.", example: "  wowdata file extension --extension blp"},
			{use: "get", short: "Fetch a raw CASC file by ID or name.", example: "  wowdata file get --file-data-id 456 --output out.bin"},
			{use: "exists", short: "Check whether a file exists.", example: "  wowdata file exists --file-data-id 456"},
			{use: "encoding", short: "Inspect content key and encoding key metadata.", example: "  wowdata file encoding --file-data-id 456"},
			{use: "export", short: "Write raw CASC files to disk.", example: "  wowdata file export --file-data-id 456 --output output/file.bin"},
		}},
		{use: "icon", short: "Export BLP textures.", example: "  wowdata icon export --file-data-id 789 --format png", child: []commandSpec{
			{use: "export", short: "Export BLP as PNG or WebP.", example: "  wowdata icon export --file-data-id 789 --format png --mipmap 0"},
		}},
		{use: "casc", short: "Inspect CASC source state.", example: "  wowdata casc info", group: "casc", child: []commandSpec{
			{use: "info", short: "Show current build and cache state.", example: "  wowdata casc info", group: "casc"},
			{use: "products", short: "List available products and builds.", example: "  wowdata casc products --source remote --region cn", group: "casc"},
			{use: "diagnose", short: "Inspect CDN, archive, root, encoding, cache, and TACT state.", example: "  wowdata casc diagnose", group: "casc"},
		}},
		{use: "item", short: "Query item metadata and assets.", example: "  wowdata item get --item-id 19019", child: []commandSpec{
			{use: "get", short: "Return item summary and slot information.", example: "  wowdata item get --item-id 19019"},
			{use: "models", short: "Return model fileDataIDs and textures.", example: "  wowdata item models --item-id 19019 --race-id 1 --gender 0"},
			{use: "geosets", short: "Return geoset and helmet-hide data.", example: "  wowdata item geosets --item-id 19019"},
			{use: "textures", short: "Return character texture fileDataIDs.", example: "  wowdata item textures --item-id 19019"},
		}},
		{use: "creature", short: "Query creature displays and models.", example: "  wowdata creature display --display-id 123", child: []commandSpec{
			{use: "display", short: "Query creature display metadata.", example: "  wowdata creature display --display-id 123"},
			{use: "model", short: "Query creature model fileDataIDs and variants.", example: "  wowdata creature model --file-data-id 456"},
		}},
		{use: "decor", short: "Query decor data.", example: "  wowdata decor list", child: []commandSpec{
			{use: "list", short: "List decor entries.", example: "  wowdata decor list --limit 50"},
			{use: "get", short: "Query decor item by ID or model fileDataID.", example: "  wowdata decor get --id 123"},
		}},
		{use: "video", short: "Process video container data from WoW files.", example: "  wowdata video demux --input movie.avi --output frames", child: []commandSpec{
			{use: "demux", short: "Replicate the Node VP9 AVI demuxer capability.", example: "  wowdata video demux --input movie.avi --output frames"},
		}},
		{use: "golden", short: "Capture and compare golden fixtures.", example: "  wowdata golden compare --fixture warmup/remote-cn-wow.json", child: []commandSpec{
			{use: "capture", short: "Capture Node baseline or Go command output.", example: "  wowdata golden capture --name db2-spellname-123 -- wowdata db2 rows SpellName --id 123"},
			{use: "compare", short: "Compare Go command output against a fixture.", example: "  wowdata golden compare --fixture fixtures/golden/db2-spellname-123.json"},
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
	for _, child := range spec.child {
		cmd.AddCommand(buildCommand(child, svc))
	}
	return cmd
}

// resolveHandler returns the injected Service handler for this command's group,
// or falls back to notImplementedHandler when no handler is registered.
func resolveHandler(spec commandSpec, svc *Service) func(cmd *cobra.Command, args []string) error {
	handler := notImplementedHandler(spec.use)
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
	}
	return handler
}
