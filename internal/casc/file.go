package casc

import "fmt"

type FileInfo struct {
	FileDataID  uint32 `json:"fileDataID"`
	Filename    string `json:"filename,omitempty"`
	ContentKey  string `json:"contentKey,omitempty"`
	EncodingKey string `json:"encodingKey,omitempty"`
	Size        int64  `json:"size,omitempty"`
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

func (fs *FileService) AddRootEntry(fileDataID uint32, contentKey string) {
	fs.roots[fileDataID] = append(fs.roots[fileDataID], contentKey)
}

func (fs *FileService) AddEncodingEntry(contentKey, encodingKey string, size int64) {
	fs.encoding[contentKey] = FileInfo{
		ContentKey:  contentKey,
		EncodingKey: encodingKey,
		Size:        size,
	}
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
	return &info, nil
}
