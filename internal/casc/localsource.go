package casc

import (
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type CASCLocal struct {
	*CASCSource
	Dir          string
	DataDir      string
	StorageDir   string
	Builds       []VersionEntry
	Build        *VersionEntry
	LocalIndexes map[string]LocalIndexEntry
}

type LocalIndexEntry struct {
	Index  int
	Offset int
	Size   int
}

func NewCASCLocal(dir string) *CASCLocal {
	dataDir := filepath.Join(dir, "Data")
	return &CASCLocal{
		CASCSource:   NewCASCSource(),
		Dir:          dir,
		DataDir:      dataDir,
		StorageDir:   filepath.Join(dataDir, "data"),
		LocalIndexes: make(map[string]LocalIndexEntry),
	}
}

func (l *CASCLocal) Init() error {
	data, err := os.ReadFile(filepath.Join(l.Dir, ".build.info"))
	if err != nil {
		return err
	}
	builds := ParseVersionConfig(string(data))
	l.Builds = l.Builds[:0]
	for _, build := range builds {
		if knownProductTitle(build.Product) != "" {
			l.Builds = append(l.Builds, build)
		}
	}
	return nil
}

func (l *CASCLocal) GetProductList() []Product {
	products := make([]Product, 0, len(l.Builds))
	for i, build := range l.Builds {
		title := knownProductTitle(build.Product)
		if title == "" {
			title = build.Product
		}
		label := strings.TrimSpace(fmt.Sprintf("%s (%s) %s", title, strings.ToUpper(build.Branch), build.Version))
		products = append(products, Product{Label: label, BuildIndex: i})
	}
	return products
}

func (l *CASCLocal) Load(buildIndex int) error {
	if buildIndex < 0 || buildIndex >= len(l.Builds) {
		return fmt.Errorf("build index %d out of range", buildIndex)
	}
	l.Build = &l.Builds[buildIndex]
	buildCfg, err := l.readConfig(l.Build.BuildKey)
	if err != nil {
		return fmt.Errorf("build config: %w", err)
	}
	l.SetBuildConfig(buildCfg)
	cdnCfg, err := l.readConfig(l.Build.CDNConfig)
	if err != nil {
		return fmt.Errorf("cdn config: %w", err)
	}
	l.SetCDNConfig(cdnCfg)
	if err := l.LoadIndexes(); err != nil {
		return err
	}
	if err := l.loadEncoding(); err != nil {
		return err
	}
	if err := l.loadRoot(); err != nil {
		return err
	}
	return nil
}

func (l *CASCLocal) LoadIndexes() error {
	entries, err := os.ReadDir(l.StorageDir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.Type().IsRegular() && strings.HasSuffix(strings.ToLower(entry.Name()), ".idx") {
			if err := l.ParseIndex(filepath.Join(l.StorageDir, entry.Name())); err != nil {
				return err
			}
		}
	}
	return nil
}

func (l *CASCLocal) ParseIndex(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if len(data) < 0x18 {
		return fmt.Errorf("local index too short: %s", path)
	}
	headerHashSize := int(int32(binary.LittleEndian.Uint32(data[0:])))
	pos := (8 + headerHashSize + 0x0f) & 0xfffffff0
	if pos+8 > len(data) {
		return fmt.Errorf("local index header out of bounds: %s", path)
	}
	dataLength := int(int32(binary.LittleEndian.Uint32(data[pos:])))
	pos += 8
	if dataLength < 0 || pos+dataLength > len(data) {
		return fmt.Errorf("local index data out of bounds: %s", path)
	}
	nBlocks := dataLength / 18
	for i := 0; i < nBlocks; i++ {
		block := data[pos : pos+18]
		key := hex.EncodeToString(block[:9])
		if _, exists := l.LocalIndexes[key]; exists {
			pos += 18
			continue
		}
		idxHigh := int(block[9])
		idxLow := binary.BigEndian.Uint32(block[10:])
		l.LocalIndexes[key] = LocalIndexEntry{
			Index:  (idxHigh << 2) | int((idxLow&0xc0000000)>>30),
			Offset: int(idxLow & 0x3fffffff),
			Size:   int(int32(binary.LittleEndian.Uint32(block[14:]))),
		}
		pos += 18
	}
	return nil
}

func (l *CASCLocal) ReadFileData(fileDataID uint32) ([]byte, error) {
	_, encodingKey, err := l.ResolveFileKeys(fileDataID)
	if err != nil {
		return nil, err
	}
	return l.ReadEncodingData(encodingKey)
}

func (l *CASCLocal) ReadFileDataPartial(fileDataID uint32) ([]byte, error) {
	return l.ReadFileData(fileDataID)
}

func (l *CASCLocal) GetBuildName() string {
	if l.Build == nil {
		return ""
	}
	return l.Build.Version
}

func (l *CASCLocal) GetBuildKey() string {
	if l.Build == nil {
		return ""
	}
	return l.Build.BuildKey
}

func (l *CASCLocal) ReadEncodingData(encodingKey string) ([]byte, error) {
	entry, ok := l.LocalIndexes[keyPrefix18(encodingKey)]
	if !ok {
		return nil, fmt.Errorf("requested file does not exist in local data: %s", encodingKey)
	}
	if entry.Size < 0x1e {
		return nil, fmt.Errorf("local data entry too small: %s", encodingKey)
	}
	data, err := readFileRange(l.FormatDataPath(entry.Index), int64(entry.Offset+0x1e), entry.Size-0x1e)
	if err != nil {
		return nil, err
	}
	if isAllZero(data) {
		return nil, fmt.Errorf("requested data file is empty or missing: %s", encodingKey)
	}
	return data, nil
}

func (l *CASCLocal) FormatDataPath(id int) string {
	return filepath.Join(l.DataDir, "data", fmt.Sprintf("data.%03d", id))
}

func (l *CASCLocal) FormatConfigPath(key string) string {
	return filepath.Join(l.DataDir, "config", FormatCDNKey(key))
}

func (l *CASCLocal) readConfig(key string) (map[string]string, error) {
	data, err := os.ReadFile(l.FormatConfigPath(key))
	if err != nil {
		return nil, err
	}
	return ParseCDNConfig(string(data))
}

func (l *CASCLocal) loadEncoding() error {
	encKeys := strings.Fields(l.BuildConfig["encoding"])
	if len(encKeys) < 2 {
		return fmt.Errorf("encoding key not found in build config")
	}
	data, err := l.ReadEncodingData(encKeys[1])
	if err != nil {
		return err
	}
	return l.ParseEncodingFile(data)
}

func (l *CASCLocal) loadRoot() error {
	rootEntry, ok := l.EncodingEntries[l.BuildConfig["root"]]
	if !ok {
		return fmt.Errorf("no encoding entry found for root key")
	}
	data, err := l.ReadEncodingData(rootEntry.Key)
	if err != nil {
		return err
	}
	_, err = l.ParseRootFile(data)
	return err
}

func readFileRange(path string, offset int64, length int) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	buf := make([]byte, length)
	n, err := f.ReadAt(buf, offset)
	if err != nil && n == 0 {
		return nil, err
	}
	return buf[:n], nil
}

func keyPrefix18(key string) string {
	if len(key) < 18 {
		return key
	}
	return key[:18]
}

func isAllZero(data []byte) bool {
	for _, b := range data {
		if b != 0 {
			return false
		}
	}
	return len(data) > 0
}

func knownProductTitle(product string) string {
	switch product {
	case "wow":
		return "World of Warcraft"
	case "wowt":
		return "PTR: World of Warcraft"
	case "wowxptr":
		return "Beta: World of Warcraft"
	case "wow_classic":
		return "World of Warcraft Classic"
	case "wow_classic_titan":
		return "World of Warcraft Classic Titan Reforged"
	case "wow_classic_era":
		return "World of Warcraft Classic Era"
	default:
		return ""
	}
}
