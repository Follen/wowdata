package listfile

import (
	"bufio"
	"fmt"
	"io"
	"strconv"
	"strings"
)

type Listfile struct {
	idToName map[uint32]string
	nameToID map[string]uint32
}

func New() *Listfile { return &Listfile{} }

func (l *Listfile) IsLoaded() bool { return l.idToName != nil }

func (l *Listfile) Load(r io.Reader) error {
	l.idToName = make(map[uint32]string)
	l.nameToID = make(map[string]uint32)

	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, ";", 2)
		if len(parts) != 2 {
			continue
		}
		id, err := strconv.ParseUint(parts[0], 10, 32)
		if err != nil {
			continue
		}
		name := strings.TrimSpace(parts[1])
		if name == "" {
			continue
		}
		l.idToName[uint32(id)] = name
		l.nameToID[strings.ToLower(name)] = uint32(id)
	}
	return scanner.Err()
}

func (l *Listfile) GetByID(id uint32) string { return l.idToName[id] }

func (l *Listfile) GetByFilename(name string) uint32 { return l.nameToID[strings.ToLower(name)] }

func (l *Listfile) ExistsByID(id uint32) bool { _, ok := l.idToName[id]; return ok }

func (l *Listfile) AddEntry(id uint32, name string) {
	if l.idToName == nil {
		l.idToName = make(map[uint32]string)
		l.nameToID = make(map[string]uint32)
	}
	l.idToName[id] = name
	l.nameToID[strings.ToLower(name)] = id
}

func (l *Listfile) GetFilenamesByExtension(ext string) []string {
	ext = "." + strings.TrimPrefix(ext, ".")
	var result []string
	for id, name := range l.idToName {
		if strings.HasSuffix(strings.ToLower(name), strings.ToLower(ext)) {
			result = append(result, fmt.Sprintf("%s [%d]", name, id))
		}
	}
	return result
}

func (l *Listfile) GetFilteredEntries(query string) map[uint32]string {
	result := make(map[uint32]string)
	q := strings.ToLower(query)
	for id, name := range l.idToName {
		if strings.Contains(strings.ToLower(name), q) {
			result[id] = name
		}
	}
	return result
}

func (l *Listfile) GetAll() map[uint32]string {
	result := make(map[uint32]string, len(l.idToName))
	for k, v := range l.idToName {
		result[k] = v
	}
	return result
}
