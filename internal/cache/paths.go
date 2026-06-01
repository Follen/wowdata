package cache

import (
	"errors"
	"path/filepath"
	"strings"
)

var ErrPathOutsideRoot = errors.New("path outside cache root")

func DB2ParquetPath(root, region, product, buildKey, locale, table string) string {
	return filepath.Join(root, "db2", region, product, buildKey, locale, table+".parquet")
}

func RawCASCPath(root, region, product, buildKey string) string {
	return filepath.Join(root, "raw", "casc", region, product, buildKey)
}

func filepathSlash(path string) string {
	return filepath.ToSlash(path)
}

func EnsureUnderRoot(root, child string) (string, error) {
	if strings.TrimSpace(root) == "" {
		return "", ErrPathOutsideRoot
	}
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	var childAbs string
	if filepath.IsAbs(child) {
		childAbs = filepath.Clean(child)
	} else {
		childAbs = filepath.Join(rootAbs, child)
	}
	childAbs, err = filepath.Abs(childAbs)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(rootAbs, childAbs)
	if err != nil {
		return "", err
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return "", ErrPathOutsideRoot
	}
	return childAbs, nil
}
