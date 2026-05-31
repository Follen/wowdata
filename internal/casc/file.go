package casc

import "fmt"

type FileInfo struct {
	FileDataID  uint32           `json:"fileDataID"`
	Filename    string           `json:"filename,omitempty"`
	ContentKey  string           `json:"contentKey,omitempty"`
	EncodingKey string           `json:"encodingKey,omitempty"`
	Enc         string           `json:"enc,omitempty"`
	Archive     *FileArchiveInfo `json:"arc,omitempty"`
	Size        int64            `json:"size,omitempty"`
}

type FileArchiveInfo struct {
	Key    string `json:"key"`
	Offset int32  `json:"ofs"`
	Length int32  `json:"len"`
}

type FileService struct {
	encoding map[string]FileInfo
	roots    map[uint32][]string
}

func NewFileService() *FileService {
	return &FileService{
		encoding: make(map[string]FileInfo),
		roots:    make(map[uint32][]string),
	}
}

func (fs *FileService) Reset() {
	fs.encoding = make(map[string]FileInfo)
	fs.roots = make(map[uint32][]string)
}

func (fs *FileService) AddRootEntry(fileDataID uint32, contentKey string) {
	fs.roots[fileDataID] = append(fs.roots[fileDataID], contentKey)
}

func (fs *FileService) AddEncodingEntry(contentKey, encodingKey string, size int64) {
	fs.encoding[contentKey] = FileInfo{
		ContentKey:  contentKey,
		EncodingKey: encodingKey,
		Enc:         encodingKey,
		Size:        size,
	}
}

func (fs *FileService) AddEncodingArchive(contentKey string, archive ArchiveEntry) {
	info := fs.encoding[contentKey]
	info.Archive = &FileArchiveInfo{Key: archive.Key, Offset: archive.Offset, Length: archive.Size}
	fs.encoding[contentKey] = info
}

func (fs *FileService) Exists(fileDataID uint32) bool {
	_, ok := fs.roots[fileDataID]
	return ok
}

func (fs *FileService) GetEncodingInfo(fileDataID uint32) (*FileInfo, error) {
	contentKeys, ok := fs.roots[fileDataID]
	if !ok || len(contentKeys) == 0 {
		return nil, fmt.Errorf("file not found: %d", fileDataID)
	}
	enc, ok := fs.encoding[contentKeys[0]]
	if !ok {
		return nil, fmt.Errorf("encoding not found for file %d", fileDataID)
	}
	info := enc
	info.FileDataID = fileDataID
	if info.Enc == "" {
		info.Enc = info.EncodingKey
	}
	return &info, nil
}
