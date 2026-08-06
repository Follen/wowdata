package resource

import (
	"crypto/sha256"
	"encoding/json"
	"io"
	"os"
)

func ReadFile(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	RecordFileRead(len(data))
	return data, err
}

func WriteFile(path string, data []byte, perm os.FileMode) error {
	err := os.WriteFile(path, data, perm)
	if err == nil {
		RecordFileWrite(len(data))
	}
	return err
}

func SumSHA256(data []byte) [sha256.Size]byte {
	result := sha256.Sum256(data)
	RecordSHA256(len(data))
	return result
}

func MarshalJSON(value any) ([]byte, error) {
	data, err := json.Marshal(value)
	if err == nil {
		RecordJSONEncode(len(data))
	}
	return data, err
}

func MarshalIndentJSON(value any, prefix, indent string) ([]byte, error) {
	data, err := json.MarshalIndent(value, prefix, indent)
	if err == nil {
		RecordJSONEncode(len(data))
	}
	return data, err
}

func EncodeJSON(w io.Writer, value any, prefix, indent string) error {
	counted := &countingWriter{writer: w}
	encoder := json.NewEncoder(counted)
	if prefix != "" || indent != "" {
		encoder.SetIndent(prefix, indent)
	}
	err := encoder.Encode(value)
	RecordJSONEncode(counted.bytes)
	return err
}

type countingWriter struct {
	writer io.Writer
	bytes  int
}

func (w *countingWriter) Write(data []byte) (int, error) {
	n, err := w.writer.Write(data)
	w.bytes += n
	return n, err
}
