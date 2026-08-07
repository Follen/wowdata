package hotfix

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"

	"wowdata/internal/dbd"
)

type Decoder struct{ MaxStringBytes int }

func (d Decoder) Decode(payload []byte, entry *dbd.DBDEntry) (map[string]any, error) {
	if entry == nil {
		return nil, errf("hotfix_schema_mismatch", "schema", "nil DBD structure")
	}
	if d.MaxStringBytes <= 0 {
		d.MaxStringBytes = 1 << 20
	}
	out := make(map[string]any, len(entry.Fields))
	off := 0
	for _, f := range entry.Fields {
		n := f.ArrayLen
		if n < 1 {
			n = 1
		}
		vals := make([]any, 0, n)
		for i := 0; i < n; i++ {
			v, used, err := d.value(payload[off:], f)
			if err != nil {
				return nil, err
			}
			off += used
			vals = append(vals, v)
		}
		if f.ArrayLen > 0 {
			out[f.Name] = vals
		} else if len(vals) == 1 {
			out[f.Name] = vals[0]
		}
	}
	if off > len(payload) {
		return nil, errf("hotfix_schema_mismatch", "payload", "decoder consumed beyond payload")
	}
	return out, nil
}
func (d Decoder) value(b []byte, f dbd.DBDField) (any, int, error) {
	typ := strings.ToLower(strings.TrimSpace(f.Type))
	switch typ {
	case "int":
		n := f.Size
		if n <= 0 {
			n = 4
		}
		if len(b) < n {
			return nil, 0, errf("hotfix_schema_mismatch", f.Name, "truncated int: need %d bytes", n)
		}
		var u uint64
		for i := 0; i < n && i < 8; i++ {
			u |= uint64(b[i]) << (8 * i)
		}
		if f.IsSigned {
			bits := uint(n * 8)
			if bits < 64 && u&(1<<(bits-1)) != 0 {
				u |= ^uint64(0) << bits
			}
			return int64(u), n, nil
		}
		return u, n, nil
	case "float":
		if len(b) < 4 {
			return nil, 0, errf("hotfix_schema_mismatch", f.Name, "truncated float")
		}
		return float64(math.Float32frombits(binary.LittleEndian.Uint32(b))), 4, nil
	case "string", "locstring":
		if len(b) < 4 {
			return nil, 0, errf("hotfix_schema_mismatch", f.Name, "truncated string length")
		}
		n := int(binary.LittleEndian.Uint32(b[:4]))
		if n < 0 || n > d.MaxStringBytes || len(b) < 4+n {
			return nil, 0, errf("hotfix_schema_mismatch", f.Name, "invalid string length %d", n)
		}
		return string(b[4 : 4+n]), 4 + n, nil
	default:
		return nil, 0, errf("hotfix_schema_mismatch", f.Name, "unsupported DBD type %q", f.Type)
	}
}
func DecodePayload(payload []byte, entry *dbd.DBDEntry) (map[string]any, error) {
	return (Decoder{}).Decode(payload, entry)
}

// DecodePositionalData names Wago's JSON positional array using the complete
// Build-specific DBD definition. It deliberately preserves nested arrays and
// reports surplus/missing fields instead of silently shifting columns.
func DecodePositionalData(data any, entry *dbd.DBDEntry) (map[string]any, error) {
	if data == nil {
		return nil, nil
	}
	if entry == nil {
		return nil, errf("hotfix_schema_mismatch", "schema", "nil DBD structure")
	}
	if obj, ok := data.(map[string]any); ok {
		return obj, nil
	}
	values, ok := data.([]any)
	if !ok {
		return nil, errf("hotfix_schema_mismatch", "data", "expected positional array or object, got %T", data)
	}
	if len(values) > len(entry.Fields) {
		return nil, errf("hotfix_schema_mismatch", "data", "%d values for %d DBD fields", len(values), len(entry.Fields))
	}
	out := make(map[string]any, len(entry.Fields))
	for i, value := range values {
		field := entry.Fields[i]
		converted, err := convertJSONValue(value, field)
		if err != nil {
			return nil, errf("hotfix_schema_mismatch", field.Name, "%v", err)
		}
		out[field.Name] = converted
	}
	return out, nil
}

func convertJSONValue(v any, field dbd.DBDField) (any, error) {
	if v == nil {
		return nil, nil
	}
	if arr, ok := v.([]any); ok {
		out := make([]any, len(arr))
		for i, item := range arr {
			x, err := convertJSONValue(item, dbd.DBDField{Name: field.Name, Type: field.Type, IsSigned: field.IsSigned, Size: field.Size})
			if err != nil {
				return nil, err
			}
			out[i] = x
		}
		return out, nil
	}
	typ := strings.ToLower(strings.TrimSpace(field.Type))
	switch typ {
	case "int", "uint", "integer":
		if n, ok := numberFromJSON(v); ok {
			if field.IsSigned {
				return int64(n), nil
			}
			return uint64(n), nil
		}
	case "float", "double":
		if n, ok := numberFromJSON(v); ok {
			return float64(n), nil
		}
	case "string", "locstring":
		if s, ok := v.(string); ok {
			return s, nil
		}
	}
	return v, nil
}

func numberFromJSON(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case json.Number:
		x, err := n.Float64()
		return x, err == nil
	case string:
		x, err := strconv.ParseFloat(n, 64)
		return x, err == nil
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	case uint64:
		return float64(n), true
	}
	return 0, false
}

var _ = fmt.Sprint
