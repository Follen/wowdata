package casc

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"

	"wowdata/internal/blte"
)

type CASCLocal struct {
	*CASCSource
	Dir          string
	DataDir      string
	StorageDir   string
	Builds       []VersionEntry
	Build        *VersionEntry
	LocalIndexes map[string]LocalIndexEntry
	indexPaths   []string
	indexMu      sync.Mutex
	loadMu       sync.Mutex
	buildIndex   int
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
		buildIndex:   -1,
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
		if strings.TrimSpace(build.Product) != "" {
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
		products = append(products, Product{
			Label: label, BuildIndex: i, Product: build.Product, Region: build.Region,
			Version: buildVersion(build), BuildID: buildID(build), BuildConfigKey: build.BuildConfig,
			CDNConfigKey: build.CDNConfig, Branch: build.Branch, Locales: LocaleNames(),
		})
	}
	return products
}

func (l *CASCLocal) Load(buildIndex int) error {
	l.loadMu.Lock()
	defer l.loadMu.Unlock()
	return l.load(buildIndex)
}

func (l *CASCLocal) load(buildIndex int) error {
	if err := l.LoadMetadata(buildIndex); err != nil {
		return err
	}
	// Local indexes are probed on demand. A point query normally needs only
	// the encoding, root, and requested payload keys; retaining every index
	// entry would turn a small query into a large process-local allocation.
	if err := l.discoverIndexPaths(); err != nil {
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

// LoadSelected loads only the root and encoding records required to resolve
// fileDataIDs. The underlying BLTE objects stay in the installed CASC
// archives and are decoded by logical range, so point queries do not retain
// the complete root and encoding files in memory.
func (l *CASCLocal) LoadSelected(buildIndex int, fileDataIDs []uint32) error {
	l.loadMu.Lock()
	defer l.loadMu.Unlock()
	return l.loadSelected(buildIndex, fileDataIDs)
}

func (l *CASCLocal) loadSelected(buildIndex int, fileDataIDs []uint32) error {
	if len(fileDataIDs) == 0 {
		return l.load(buildIndex)
	}
	if err := l.LoadMetadata(buildIndex); err != nil {
		return err
	}
	if err := l.discoverIndexPaths(); err != nil {
		return err
	}
	encKeys := strings.Fields(l.BuildConfig["encoding"])
	if len(encKeys) < 2 {
		return fmt.Errorf("encoding key not found in build config")
	}
	encodingReader, err := l.localRangeReader(encKeys[1])
	if err != nil {
		return fmt.Errorf("encoding: %w", err)
	}
	rootKey, err := hex.DecodeString(l.BuildConfig["root"])
	if err != nil {
		return fmt.Errorf("invalid root content key: %w", err)
	}
	workers := runtime.GOMAXPROCS(0)
	if workers < 1 {
		workers = 1
	}
	if err := l.parseEncodingReaderSelected(encodingReader, map[string]struct{}{string(rootKey): {}}, workers); err != nil {
		return fmt.Errorf("encoding root lookup: %w", err)
	}
	rootEntry, ok := l.EncodingEntries[l.BuildConfig["root"]]
	if !ok {
		return fmt.Errorf("no encoding entry found for root key")
	}
	rootReader, err := l.localRangeReader(rootEntry.Key)
	if err != nil {
		return fmt.Errorf("root: %w", err)
	}
	if _, err := l.parseRootReaderSelectedForLocale(rootReader, fileDataIDs, workers); err != nil {
		return fmt.Errorf("root: %w", err)
	}
	wanted := make(map[string]struct{}, len(fileDataIDs))
	for _, fileDataID := range fileDataIDs {
		for _, entry := range l.RootEntries[fileDataID] {
			key, decodeErr := hex.DecodeString(entry.ContentKey)
			if decodeErr != nil {
				return fmt.Errorf("invalid content key %q: %w", entry.ContentKey, decodeErr)
			}
			wanted[string(key)] = struct{}{}
		}
	}
	if len(wanted) == 0 {
		return nil
	}
	if err := l.parseEncodingReaderSelected(encodingReader, wanted, workers); err != nil {
		return fmt.Errorf("encoding: %w", err)
	}
	return nil
}

// EnsureFiles incrementally extends the selected local root and encoding
// metadata for the currently loaded Build. Domain commands can discover a
// second wave of FileDataIDs without falling back to a full local preload.
func (l *CASCLocal) EnsureFiles(fileDataIDs []uint32) error {
	l.loadMu.Lock()
	defer l.loadMu.Unlock()
	if l.buildIndex < 0 || l.Build == nil {
		return fmt.Errorf("local CASC build is not loaded")
	}
	missing := make([]uint32, 0, len(fileDataIDs))
	seen := make(map[uint32]struct{}, len(fileDataIDs))
	for _, fileDataID := range fileDataIDs {
		if _, ok := seen[fileDataID]; ok {
			continue
		}
		seen[fileDataID] = struct{}{}
		if _, _, err := l.ResolveFileKeys(fileDataID); err != nil {
			missing = append(missing, fileDataID)
		}
	}
	if len(missing) == 0 {
		return nil
	}
	return l.loadSelected(l.buildIndex, missing)
}

func (l *CASCLocal) localRangeReader(encodingKey string) (*blte.RangeReader, error) {
	entry, ok := l.localIndexEntry(encodingKey)
	if !ok {
		return nil, fmt.Errorf("requested file does not exist in local data: %s", encodingKey)
	}
	if entry.Size < 0x1e {
		return nil, fmt.Errorf("local data entry too small: %s", encodingKey)
	}
	path := l.FormatDataPath(entry.Index)
	base := int64(entry.Offset + 0x1e)
	size := entry.Size - 0x1e
	return blte.NewRangeReader(func(offset, length int) ([]byte, error) {
		if offset < 0 || length < 0 || offset > size || length > size-offset {
			return nil, fmt.Errorf("local BLTE range %d+%d exceeds %d", offset, length, size)
		}
		return readFileRange(path, base+int64(offset), length)
	})
}

func (l *CASCLocal) discoverIndexPaths() error {
	entries, err := os.ReadDir(l.StorageDir)
	if err != nil {
		return err
	}
	paths := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.Type().IsRegular() && strings.HasSuffix(strings.ToLower(entry.Name()), ".idx") {
			paths = append(paths, filepath.Join(l.StorageDir, entry.Name()))
		}
	}
	sort.Strings(paths)
	l.indexPaths = paths
	return nil
}

func (l *CASCLocal) LoadMetadata(buildIndex int) error {
	if buildIndex < 0 || buildIndex >= len(l.Builds) {
		return fmt.Errorf("build index %d out of range", buildIndex)
	}
	l.Build = &l.Builds[buildIndex]
	buildCfg, err := l.readConfig(l.Build.BuildKey)
	if err != nil {
		return fmt.Errorf("build config: %w", err)
	}
	l.SetBuildConfig(buildCfg)
	l.buildIndex = buildIndex
	cdnCfg, err := l.readConfig(l.Build.CDNConfig)
	if err != nil {
		if os.IsNotExist(err) {
			// Some installed clients retain the Build config but omit the CDN
			// config after an update. Metadata-only inspection does not need it;
			// data loading will still fail later if the required local indexes are
			// unavailable, allowing the auto source path to use CDN fallback.
			return nil
		}
		return fmt.Errorf("cdn config: %w", err)
	}
	l.SetCDNConfig(cdnCfg)
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
	raw, err := l.ReadEncodingData(encodingKey)
	if err != nil {
		return nil, err
	}
	return DecodeCASCData(raw)
}

func (l *CASCLocal) ReadFileDataPartial(fileDataID uint32) ([]byte, error) {
	_, encodingKey, err := l.ResolveFileKeys(fileDataID)
	if err != nil {
		return nil, err
	}
	raw, err := l.ReadEncodingData(encodingKey)
	if err != nil {
		return nil, err
	}
	return DecodeCASCDataPartial(raw)
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
	entry, ok := l.localIndexEntry(encodingKey)
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

func (l *CASCLocal) localIndexEntry(encodingKey string) (LocalIndexEntry, bool) {
	key := keyPrefix18(strings.ToLower(encodingKey))
	l.indexMu.Lock()
	if entry, ok := l.LocalIndexes[key]; ok {
		l.indexMu.Unlock()
		return entry, true
	}
	paths := append([]string(nil), l.indexPaths...)
	l.indexMu.Unlock()
	if len(paths) == 0 {
		_ = l.discoverIndexPaths()
		paths = append([]string(nil), l.indexPaths...)
	}
	if len(paths) == 0 {
		return LocalIndexEntry{}, false
	}
	workers := runtime.GOMAXPROCS(0)
	if workers < 1 {
		workers = 1
	}
	if workers > len(paths) {
		workers = len(paths)
	}
	type result struct {
		entry LocalIndexEntry
		ok    bool
		err   error
	}
	jobs := make(chan string, len(paths))
	results := make(chan result, workers)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for path := range jobs {
				entry, ok, err := findLocalIndexEntry(path, key)
				if ok || err != nil {
					results <- result{entry: entry, ok: ok, err: err}
				}
			}
		}()
	}
	for _, path := range paths {
		jobs <- path
	}
	close(jobs)
	go func() { wg.Wait(); close(results) }()
	var matched *LocalIndexEntry
	for found := range results {
		if matched != nil || found.err != nil || !found.ok {
			continue
		}
		entry := found.entry
		matched = &entry
	}
	if matched == nil {
		return LocalIndexEntry{}, false
	}
	l.indexMu.Lock()
	l.LocalIndexes[key] = *matched
	l.indexMu.Unlock()
	return *matched, true
}

func findLocalIndexEntry(path, wanted string) (LocalIndexEntry, bool, error) {
	wantedBytes, err := hex.DecodeString(wanted)
	if err != nil || len(wantedBytes) != 9 {
		return LocalIndexEntry{}, false, nil
	}
	file, err := os.Open(path)
	if err != nil {
		return LocalIndexEntry{}, false, err
	}
	defer file.Close()
	header := make([]byte, 0x18)
	if _, err := file.ReadAt(header, 0); err != nil {
		return LocalIndexEntry{}, false, nil
	}
	headerHashSize := int(int32(binary.LittleEndian.Uint32(header[0:])))
	pos := (8 + headerHashSize + 0x0f) & 0xfffffff0
	lengthHeader := make([]byte, 8)
	if _, err := file.ReadAt(lengthHeader, int64(pos)); err != nil {
		return LocalIndexEntry{}, false, nil
	}
	dataLength := int(int32(binary.LittleEndian.Uint32(lengthHeader)))
	pos += 8
	info, err := file.Stat()
	if err != nil || dataLength < 0 || int64(pos+dataLength) > info.Size() || dataLength%18 != 0 {
		return LocalIndexEntry{}, false, nil
	}
	low, high := 0, dataLength/18
	key := make([]byte, 9)
	for low < high {
		mid := low + (high-low)/2
		if _, err := file.ReadAt(key, int64(pos+mid*18)); err != nil {
			return LocalIndexEntry{}, false, err
		}
		if bytes.Compare(key, wantedBytes) < 0 {
			low = mid + 1
		} else {
			high = mid
		}
	}
	if low >= dataLength/18 {
		return LocalIndexEntry{}, false, nil
	}
	block := make([]byte, 18)
	if _, err := file.ReadAt(block, int64(pos+low*18)); err != nil {
		return LocalIndexEntry{}, false, err
	}
	if !bytes.Equal(block[:9], wantedBytes) {
		return LocalIndexEntry{}, false, nil
	}
	idxHigh := int(block[9])
	idxLow := binary.BigEndian.Uint32(block[10:])
	return LocalIndexEntry{Index: (idxHigh << 2) | int((idxLow&0xc0000000)>>30), Offset: int(idxLow & 0x3fffffff), Size: int(int32(binary.LittleEndian.Uint32(block[14:])))}, true, nil
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
	case "wow_beta":
		return "Beta: World of Warcraft"
	case "wow_classic":
		return "World of Warcraft Classic"
	case "wow_classic_ptr":
		return "PTR: World of Warcraft Classic"
	case "wow_classic_beta":
		return "Beta: World of Warcraft Classic"
	case "wow_classic_titan":
		return "World of Warcraft Classic Titan Reforged"
	case "wow_classic_era":
		return "World of Warcraft Classic Era"
	case "wow_classic_era_ptr":
		return "PTR: World of Warcraft Classic Era"
	case "wow_anniversary":
		return "World of Warcraft Classic Anniversary"
	default:
		return ""
	}
}
