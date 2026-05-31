package dbd

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
)

type ManifestEntry struct {
	TableName     string `json:"tableName"`
	DB2FileDataID uint32 `json:"db2FileDataID"`
}

type Manifest struct {
	tableToID map[string]uint32
	idToTable map[uint32]string
}

func ParseManifest(r io.Reader) (*Manifest, error) {
	var entries []ManifestEntry
	if err := json.NewDecoder(r).Decode(&entries); err != nil {
		return nil, err
	}
	m := &Manifest{
		tableToID: make(map[string]uint32),
		idToTable: make(map[uint32]string),
	}
	for _, entry := range entries {
		if entry.TableName == "" || entry.DB2FileDataID == 0 {
			continue
		}
		m.tableToID[entry.TableName] = entry.DB2FileDataID
		m.idToTable[entry.DB2FileDataID] = entry.TableName
	}
	if len(m.tableToID) == 0 {
		return nil, fmt.Errorf("DBD manifest contains no table mappings")
	}
	return m, nil
}

func (m *Manifest) GetByTableName(table string) (uint32, bool) {
	id, ok := m.tableToID[table]
	return id, ok
}

func (m *Manifest) GetByID(id uint32) (string, bool) {
	table, ok := m.idToTable[id]
	return table, ok
}

func (m *Manifest) TableNames() []string {
	names := make([]string, 0, len(m.tableToID))
	for name := range m.tableToID {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
