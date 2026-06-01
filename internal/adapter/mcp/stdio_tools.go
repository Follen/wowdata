package mcp

var stdioToolNames = []string{
	"wow_warmup",
	"wow_casc",
	"wow_db2",
	"wow_file",
	"wow_icon",
	"wow_spell",
	"wow_encounter",
	"wow_item",
	"wow_creature",
	"wow_decor",
	"wow_video",
}

func StdioToolNames() []string {
	return append([]string{}, stdioToolNames...)
}
