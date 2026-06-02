package runtime

import (
	"fmt"
	"sort"
	"strings"

	"wowdata/internal/casc"
	"wowdata/internal/listfile"
)

type FileDataReader interface {
	ReadFileData(fileDataID uint32) ([]byte, error)
}

type FileMetadataReader interface {
	FileExists(fileDataID uint32) bool
	GetFileEncodingInfo(fileDataID uint32) (*casc.FileInfo, error)
}

type CASCFileStore struct {
	listfile *listfile.Listfile
	files    *casc.FileService
	reader   FileDataReader
}

func NewCASCFileStore(lf *listfile.Listfile, fs *casc.FileService, reader FileDataReader) *CASCFileStore {
	return &CASCFileStore{listfile: lf, files: fs, reader: reader}
}

func (s *CASCFileStore) Ready() bool {
	return s != nil && ((s.listfile != nil && s.listfile.IsLoaded()) || s.files != nil || s.reader != nil)
}

func (s *CASCFileStore) Lookup(fileDataID uint32) (string, bool) {
	if s.listfile == nil {
		return "", false
	}
	name := s.listfile.GetByID(fileDataID)
	return name, name != ""
}

func (s *CASCFileStore) Search(query string, limit int) []FileEntry {
	if s.listfile == nil {
		return nil
	}
	return entriesFromListfileEntries(s.listfile.GetFilteredEntriesOrdered(query), limit)
}

func (s *CASCFileStore) SearchCount(query string) int {
	if s.listfile == nil {
		return 0
	}
	return len(s.listfile.GetFilteredEntriesOrdered(query))
}

func (s *CASCFileStore) Extension(extension string, limit int) []FileEntry {
	if s.listfile == nil {
		return nil
	}
	names := s.listfile.GetFilenamesByExtension(extension)
	return s.entriesFromNames(names, limit)
}

func (s *CASCFileStore) ExtensionCount(extension string) int {
	if s.listfile == nil {
		return 0
	}
	return len(s.listfile.GetFilenamesByExtension(extension))
}

func (s *CASCFileStore) ExistsByID(fileDataID uint32) bool {
	if s.files != nil && s.files.Exists(fileDataID) {
		return true
	}
	if reader, ok := s.reader.(FileMetadataReader); ok && reader.FileExists(fileDataID) {
		return true
	}
	return s.listfile != nil && s.listfile.ExistsByID(fileDataID)
}

func (s *CASCFileStore) ExistsByName(filename string) bool {
	return s.listfile != nil && s.listfile.GetByFilename(filename) > 0
}

func (s *CASCFileStore) EncodingInfo(fileDataID uint32) (interface{}, error) {
	if s.files != nil {
		return s.files.GetEncodingInfo(fileDataID)
	}
	if reader, ok := s.reader.(FileMetadataReader); ok {
		return reader.GetFileEncodingInfo(fileDataID)
	}
	return nil, fmt.Errorf("file metadata is not initialized")
}

func (s *CASCFileStore) ReadByID(fileDataID uint32) ([]byte, error) {
	if s.reader == nil {
		return nil, fmt.Errorf("CASC reader is not initialized")
	}
	return s.reader.ReadFileData(fileDataID)
}

func (s *CASCFileStore) ReadByName(filename string) ([]byte, error) {
	if s.listfile == nil {
		return nil, fmt.Errorf("listfile is not initialized")
	}
	fileDataID := s.listfile.GetByFilename(filename)
	if fileDataID == 0 {
		return nil, fmt.Errorf("file not found: %s", filename)
	}
	return s.ReadByID(fileDataID)
}

func (s *CASCFileStore) entriesFromNames(names []string, limit int) []FileEntry {
	entries := make([]FileEntry, 0, len(names))
	for _, name := range names {
		filename, fileDataID := parseFormattedListfileEntry(name)
		if fileDataID == 0 && s.listfile != nil {
			fileDataID = s.listfile.GetByFilename(filename)
		}
		entries = append(entries, FileEntry{FileDataID: fileDataID, Filename: filename})
		if limit > 0 && len(entries) >= limit {
			break
		}
	}
	return entries
}

func parseFormattedListfileEntry(entry string) (string, uint32) {
	entry = strings.TrimSpace(entry)
	open := strings.LastIndex(entry, " [")
	if open < 0 || !strings.HasSuffix(entry, "]") {
		return entry, 0
	}
	idText := entry[open+2 : len(entry)-1]
	var id uint32
	if _, err := fmt.Sscanf(idText, "%d", &id); err != nil {
		return entry, 0
	}
	return strings.TrimSpace(entry[:open]), id
}

func entriesFromMap(values map[uint32]string, limit int) []FileEntry {
	entries := make([]FileEntry, 0, len(values))
	for id, name := range values {
		entries = append(entries, FileEntry{FileDataID: id, Filename: name})
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Filename == entries[j].Filename {
			return entries[i].FileDataID < entries[j].FileDataID
		}
		return entries[i].Filename < entries[j].Filename
	})
	if limit > 0 && len(entries) > limit {
		return entries[:limit]
	}
	return entries
}

func entriesFromListfileEntries(values []listfile.FileEntry, limit int) []FileEntry {
	entries := make([]FileEntry, 0, len(values))
	for _, entry := range values {
		entries = append(entries, FileEntry{FileDataID: entry.FileDataID, Filename: entry.Filename})
		if limit > 0 && len(entries) >= limit {
			break
		}
	}
	return entries
}
