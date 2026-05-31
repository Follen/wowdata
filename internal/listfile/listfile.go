package listfile

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

type Listfile struct {
	idToName  map[uint32]string
	nameToID  map[string]uint32
	order     []uint32
	treeNodes []byte
	binary    bool
}

func New() *Listfile { return &Listfile{} }

func (l *Listfile) IsLoaded() bool { return l.idToName != nil }

func (l *Listfile) Load(r io.Reader) error {
	l.idToName = make(map[uint32]string)
	l.nameToID = make(map[string]uint32)
	l.order = nil
	l.treeNodes = nil
	l.binary = false

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
		l.addNormalized(uint32(id), name)
	}
	return scanner.Err()
}

const (
	componentIDIndex    = "listfile-id-index.dat"
	componentStrings    = "listfile-strings.dat"
	componentTreeNodes  = "listfile-tree-nodes.dat"
	componentPFModels   = "listfile-pf-models.dat"
	componentPFTextures = "listfile-pf-textures.dat"
	componentPFSounds   = "listfile-pf-sounds.dat"
	componentPFVideos   = "listfile-pf-videos.dat"
	componentPFText     = "listfile-pf-text.dat"
	componentPFFonts    = "listfile-pf-fonts.dat"
)

var binaryStringComponents = []string{
	componentStrings,
	componentPFModels,
	componentPFTextures,
	componentPFSounds,
	componentPFVideos,
	componentPFText,
	componentPFFonts,
}

// LoadBinaryDir loads the componentized binary listfile format used by the
// legacy implementation. The ID index uses 9-byte entries:
// big-endian fileDataID, big-endian string offset, and one pf/string file index.
func (l *Listfile) LoadBinaryDir(dir string) error {
	index, err := os.ReadFile(filepath.Join(dir, componentIDIndex))
	if err != nil {
		return err
	}
	if len(index)%9 != 0 {
		return fmt.Errorf("binary listfile id index has invalid length %d", len(index))
	}

	stringFiles := make([][]byte, len(binaryStringComponents))
	for i, name := range binaryStringComponents {
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			return err
		}
		stringFiles[i] = data
	}

	treeNodes, err := os.ReadFile(filepath.Join(dir, componentTreeNodes))
	if err != nil && !os.IsNotExist(err) {
		return err
	}

	l.idToName = make(map[uint32]string, len(index)/9)
	l.nameToID = make(map[string]uint32, len(index)/9)
	l.order = nil
	l.treeNodes = treeNodes
	l.binary = true

	for offset := 0; offset < len(index); offset += 9 {
		id := binary.BigEndian.Uint32(index[offset : offset+4])
		stringOffset := binary.BigEndian.Uint32(index[offset+4 : offset+8])
		pfIndex := int(index[offset+8])
		if pfIndex < 0 || pfIndex >= len(stringFiles) {
			return fmt.Errorf("binary listfile id %d references invalid pf index %d", id, pfIndex)
		}
		name, err := readCStringAt(stringFiles[pfIndex], int(stringOffset))
		if err != nil {
			return fmt.Errorf("binary listfile id %d: %w", id, err)
		}
		l.addNormalized(id, name)
	}

	return nil
}

func (l *Listfile) GetByID(id uint32) string { return l.idToName[id] }

func (l *Listfile) GetByFilename(name string) uint32 {
	normalized := normalizeFilename(name)
	if l.binary && len(l.treeNodes) > 0 {
		if id, ok := l.lookupBinaryTree(normalized); ok {
			if l.ExistsByID(id) {
				return id
			}
		}
	}
	if id := l.nameToID[normalized]; id != 0 {
		return id
	}
	if strings.HasSuffix(normalized, ".mdl") || strings.HasSuffix(normalized, ".mdx") {
		return l.GetByFilename(normalized[:len(normalized)-4] + ".m2")
	}
	return 0
}

func (l *Listfile) ExistsByID(id uint32) bool { _, ok := l.idToName[id]; return ok }

func (l *Listfile) AddEntry(id uint32, name string) {
	if l.idToName == nil {
		l.idToName = make(map[uint32]string)
		l.nameToID = make(map[string]uint32)
	}
	l.addNormalized(id, name)
}

func (l *Listfile) ReplaceFrom(src *Listfile) {
	l.idToName = make(map[uint32]string)
	l.nameToID = make(map[string]uint32)
	l.order = nil
	if src == nil {
		return
	}
	for _, id := range src.order {
		if name, ok := src.idToName[id]; ok {
			l.addNormalized(id, name)
		}
	}
	l.treeNodes = append([]byte(nil), src.treeNodes...)
	l.binary = src.binary
}

func (l *Listfile) FilterIDs(valid map[uint32]bool) {
	if l == nil || l.idToName == nil || len(valid) == 0 {
		return
	}
	nextIDToName := make(map[uint32]string)
	nextNameToID := make(map[string]uint32)
	nextOrder := make([]uint32, 0, len(l.order))
	for _, id := range l.order {
		name, ok := l.idToName[id]
		if !ok || !valid[id] {
			continue
		}
		nextIDToName[id] = name
		nextNameToID[normalizeFilename(name)] = id
		nextOrder = append(nextOrder, id)
	}
	l.idToName = nextIDToName
	l.nameToID = nextNameToID
	l.order = nextOrder
}

func (l *Listfile) GetFilenamesByExtension(ext string) []string {
	ext = "." + strings.TrimPrefix(ext, ".")
	var result []string
	for id, name := range l.idToName {
		if strings.HasSuffix(strings.ToLower(name), strings.ToLower(ext)) {
			result = append(result, fmt.Sprintf("%s [%d]", name, id))
		}
	}
	sort.Strings(result)
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

func (l *Listfile) GetFilteredEntriesOrdered(query string) []FileEntry {
	q := strings.ToLower(query)
	result := make([]FileEntry, 0)
	for _, id := range l.order {
		name, ok := l.idToName[id]
		if !ok {
			continue
		}
		if strings.Contains(strings.ToLower(name), q) {
			result = append(result, FileEntry{FileDataID: id, Filename: name})
		}
	}
	return result
}

type FileEntry struct {
	FileDataID uint32
	Filename   string
}

func (l *Listfile) addNormalized(id uint32, name string) {
	normalized := normalizeFilename(name)
	if _, exists := l.idToName[id]; !exists {
		l.order = append(l.order, id)
	}
	l.idToName[id] = normalized
	l.nameToID[normalized] = id
}

func normalizeFilename(name string) string {
	return strings.ToLower(strings.ReplaceAll(strings.TrimSpace(name), "\\", "/"))
}

func readCStringAt(data []byte, offset int) (string, error) {
	if offset < 0 || offset >= len(data) {
		return "", fmt.Errorf("string offset %d out of range", offset)
	}
	end := offset
	for end < len(data) && data[end] != 0 {
		end++
	}
	if end >= len(data) {
		return "", fmt.Errorf("unterminated string at offset %d", offset)
	}
	return string(data[offset:end]), nil
}

func (l *Listfile) lookupBinaryTree(filename string) (uint32, bool) {
	parts := strings.Split(filename, "/")
	if len(parts) == 0 {
		return 0, false
	}
	nodeOffset := uint32(0)
	for _, component := range parts[:len(parts)-1] {
		next, ok := l.findComponentChild(nodeOffset, component)
		if !ok {
			return 0, false
		}
		nodeOffset = next
	}
	return l.findFileInNode(nodeOffset, parts[len(parts)-1])
}

func (l *Listfile) nodeHeader(nodeOffset uint32) (childCount uint32, fileCount uint32, large bool, ok bool) {
	if int(nodeOffset)+9 > len(l.treeNodes) {
		return 0, 0, false, false
	}
	node := l.treeNodes[nodeOffset:]
	return binary.BigEndian.Uint32(node[0:4]), binary.BigEndian.Uint32(node[4:8]), node[8] == 1, true
}

func (l *Listfile) findComponentChild(nodeOffset uint32, component string) (uint32, bool) {
	childCount, _, _, ok := l.nodeHeader(nodeOffset)
	if !ok {
		return 0, false
	}
	target := XXHash64String(component)
	left, right := 0, int(childCount)-1
	for left <= right {
		mid := left + (right-left)/2
		entry := int(nodeOffset) + 9 + mid*12
		if entry+12 > len(l.treeNodes) {
			return 0, false
		}
		hash := binary.BigEndian.Uint64(l.treeNodes[entry : entry+8])
		if hash == target {
			return binary.BigEndian.Uint32(l.treeNodes[entry+8 : entry+12]), true
		}
		if hash < target {
			left = mid + 1
		} else {
			right = mid - 1
		}
	}
	return 0, false
}

func (l *Listfile) findFileInNode(nodeOffset uint32, filename string) (uint32, bool) {
	childCount, fileCount, large, ok := l.nodeHeader(nodeOffset)
	if !ok || fileCount == 0 {
		return 0, false
	}
	pos := int(nodeOffset) + 9 + int(childCount)*12
	if large {
		target := XXHash64String(filename)
		left, right := 0, int(fileCount)-1
		for left <= right {
			mid := left + (right-left)/2
			entry := pos + mid*12
			if entry+12 > len(l.treeNodes) {
				return 0, false
			}
			hash := binary.BigEndian.Uint64(l.treeNodes[entry : entry+8])
			if hash == target {
				return binary.BigEndian.Uint32(l.treeNodes[entry+8 : entry+12]), true
			}
			if hash < target {
				left = mid + 1
			} else {
				right = mid - 1
			}
		}
		return 0, false
	}

	for i := uint32(0); i < fileCount; i++ {
		if pos+2 > len(l.treeNodes) {
			return 0, false
		}
		nameLen := int(binary.BigEndian.Uint16(l.treeNodes[pos : pos+2]))
		pos += 2
		if pos+nameLen+4 > len(l.treeNodes) {
			return 0, false
		}
		name := string(l.treeNodes[pos : pos+nameLen])
		pos += nameLen
		id := binary.BigEndian.Uint32(l.treeNodes[pos : pos+4])
		pos += 4
		if name == filename {
			return id, true
		}
	}
	return 0, false
}

func (l *Listfile) GetAll() map[uint32]string {
	result := make(map[uint32]string, len(l.idToName))
	for k, v := range l.idToName {
		result[k] = v
	}
	return result
}
