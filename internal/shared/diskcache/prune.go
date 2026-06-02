package diskcache

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type fileEntry struct {
	path    string
	size    int64
	modTime int64
}

func PruneLRU(root string, limitBytes int64, targetBytes int64) error {
	if root == "" || limitBytes <= 0 {
		return nil
	}
	if targetBytes <= 0 || targetBytes >= limitBytes {
		targetBytes = limitBytes * 8 / 10
	}
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return err
	}

	var total int64
	var files []fileEntry
	err = filepath.WalkDir(rootAbs, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		if entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil || !info.Mode().IsRegular() {
			return nil
		}
		name := entry.Name()
		if strings.HasPrefix(name, ".tmp-") || name == "cache_integrity.json" || name == "build_manifest.json" {
			return nil
		}
		size := info.Size()
		total += size
		files = append(files, fileEntry{path: path, size: size, modTime: info.ModTime().UnixNano()})
		return nil
	})
	if err != nil {
		return err
	}
	if total <= limitBytes {
		return nil
	}

	sort.Slice(files, func(i, j int) bool {
		if files[i].modTime == files[j].modTime {
			return files[i].path < files[j].path
		}
		return files[i].modTime < files[j].modTime
	})
	for _, file := range files {
		if total <= targetBytes {
			break
		}
		if err := os.Remove(file.path); err != nil && !os.IsNotExist(err) {
			return err
		}
		total -= file.size
	}
	return nil
}
