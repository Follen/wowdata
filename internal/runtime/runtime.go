package runtime

type SchemaField struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

type DB2Store interface {
	Schema(table string) ([]SchemaField, int, error)
	Rows(table string, ids []uint32, fields []string, filter string, limit int) ([]map[string]interface{}, error)
	Search(table string, field string, query string, limit int) ([]map[string]interface{}, error)
	ForeignKey(table string, field string, value uint32, limit int) ([]map[string]interface{}, error)
	Stream(table string, fields []string, filter string, limit int) ([]map[string]interface{}, error)
}

type FileEntry struct {
	FileDataID uint32 `json:"fileDataID"`
	Filename   string `json:"filename"`
}

type FileStore interface {
	Lookup(fileDataID uint32) (string, bool)
	Search(query string, limit int) []FileEntry
	SearchCount(query string) int
	Extension(extension string, limit int) []FileEntry
	ExtensionCount(extension string) int
	ExistsByID(fileDataID uint32) bool
	ExistsByName(filename string) bool
	EncodingInfo(fileDataID uint32) (interface{}, error)
	ReadByID(fileDataID uint32) ([]byte, error)
	ReadByName(filename string) ([]byte, error)
}

type IconStore interface {
	ReadByID(fileDataID uint32) ([]byte, error)
}
