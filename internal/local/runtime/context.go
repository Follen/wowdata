package runtime

type Context struct {
	Source      string
	Path        string
	Region      string
	Product     string
	BuildName   string
	BuildKey    string
	BuildIndex  int
	Locale      string
	CacheRoot   string
	CASCReady   bool
	DBDReady    bool
	Listfile    bool
	TablesReady map[string]bool
}

func (c *Context) Ready() bool {
	return c != nil && c.CASCReady && c.DBDReady && c.TablesReady != nil
}

func (c *Context) HasTable(table string) bool {
	return c != nil && c.TablesReady != nil && c.TablesReady[table]
}
