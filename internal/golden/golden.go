package golden

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
)

type CompareResult struct {
	Fixture string `json:"fixture"`
	Equal   bool   `json:"equal"`
	Reason  string `json:"reason,omitempty"`
}

type Manifest struct {
	Version               int             `json:"version"`
	Source                string          `json:"source,omitempty"`
	RequiredGroups        []string        `json:"requiredGroups,omitempty"`
	RequiredSuccessGroups []string        `json:"requiredSuccessGroups,omitempty"`
	Fixtures              []ManifestEntry `json:"fixtures"`
}

type ManifestEntry struct {
	Name      string                `json:"name"`
	Group     string                `json:"group,omitempty"`
	Kind      string                `json:"kind,omitempty"`
	Command   string                `json:"command,omitempty"`
	Args      []string              `json:"args,omitempty"`
	Source    string                `json:"source,omitempty"`
	Region    string                `json:"region,omitempty"`
	Product   string                `json:"product,omitempty"`
	Build     string                `json:"build,omitempty"`
	Fixture   string                `json:"fixture"`
	Actual    string                `json:"actual"`
	Artifacts []ArtifactExpectation `json:"artifacts,omitempty"`
}

type ArtifactExpectation struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256,omitempty"`
	MD5    string `json:"md5,omitempty"`
	Size   int64  `json:"size,omitempty"`
}

func FixturePath(group string, name string) string {
	return filepath.ToSlash(filepath.Join("fixtures", "golden", group, name+".json"))
}

func ParseManifest(data []byte) (*Manifest, error) {
	var manifest Manifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return nil, err
	}
	return &manifest, nil
}

func (m *Manifest) MissingRequiredGroups() []string {
	if m == nil || len(m.RequiredGroups) == 0 {
		return nil
	}
	present := make(map[string]bool)
	for _, entry := range m.Fixtures {
		group := entry.Group
		if group == "" {
			group = groupFromName(entry.Name)
		}
		if group != "" {
			present[group] = true
		}
	}
	missing := make([]string, 0)
	for _, group := range m.RequiredGroups {
		if !present[group] {
			missing = append(missing, group)
		}
	}
	return missing
}

func (m *Manifest) MissingRequiredSuccessGroups() []string {
	if m == nil || len(m.RequiredSuccessGroups) == 0 {
		return nil
	}
	present := make(map[string]bool)
	for _, entry := range m.Fixtures {
		if entry.Kind != "success" {
			continue
		}
		group := entry.Group
		if group == "" {
			group = groupFromName(entry.Name)
		}
		if group != "" {
			present[group] = true
		}
	}
	missing := make([]string, 0)
	for _, group := range m.RequiredSuccessGroups {
		if !present[group] {
			missing = append(missing, group)
		}
	}
	return missing
}

func groupFromName(name string) string {
	for i, r := range name {
		if r == '/' || r == '\\' {
			return name[:i]
		}
	}
	return name
}

func CompareJSON(expected []byte, actual []byte) (CompareResult, error) {
	var expectedValue interface{}
	if err := json.Unmarshal(expected, &expectedValue); err != nil {
		return CompareResult{Equal: false, Reason: "invalid expected JSON"}, err
	}
	var actualValue interface{}
	if err := json.Unmarshal(actual, &actualValue); err != nil {
		return CompareResult{Equal: false, Reason: "invalid actual JSON"}, err
	}
	if expectedCapture, ok := capturePayload(expectedValue); ok {
		if actualCapture, ok := capturePayload(actualValue); ok {
			return compareCapturedPayload(expectedCapture, actualCapture), nil
		}
	}
	if expectedWrapper, ok := mcpResultWrapperPayload(expectedValue); ok {
		if actualCapture, ok := capturePayload(actualValue); ok {
			return compareBusinessPayloads(expectedWrapper, capturedStdoutPayload(actualCapture)), nil
		}
	}
	if expectedCapture, ok := capturePayload(expectedValue); ok {
		if actualWrapper, ok := mcpResultWrapperPayload(actualValue); ok {
			return compareBusinessPayloads(capturedStdoutPayload(expectedCapture), actualWrapper), nil
		}
	}
	if expectedBusiness, ok := businessPayload(expectedValue); ok {
		if actualBusiness, ok := businessPayload(actualValue); ok {
			return compareBusinessPayloads(expectedBusiness, actualBusiness), nil
		}
	}
	if reflect.DeepEqual(expectedValue, actualValue) {
		return CompareResult{Equal: true}, nil
	}
	return CompareResult{
		Equal:  false,
		Reason: fmt.Sprintf("JSON differs: expected %s, got %s", canonicalJSON(expectedValue), canonicalJSON(actualValue)),
	}, nil
}

func compareBusinessPayloads(expected interface{}, actual interface{}) CompareResult {
	expectedComparable := normalizeComparable(expected)
	actualComparable := normalizeComparable(actual)
	if reflect.DeepEqual(expectedComparable, actualComparable) || payloadContains(actualComparable, expectedComparable) {
		return CompareResult{Equal: true}
	}
	return CompareResult{
		Equal:  false,
		Reason: fmt.Sprintf("business payload differs: expected %s, got %s", canonicalJSON(expected), canonicalJSON(actual)),
	}
}

func payloadContains(actual interface{}, expected interface{}) bool {
	expectedMap, expectedIsMap := expected.(map[string]interface{})
	actualMap, actualIsMap := actual.(map[string]interface{})
	if expectedIsMap && actualIsMap {
		for key, expectedValue := range expectedMap {
			actualValue, ok := actualMap[key]
			if !ok || !payloadContains(actualValue, expectedValue) {
				return false
			}
		}
		return true
	}
	return reflect.DeepEqual(actual, expected)
}

func comparablePayload(value interface{}) interface{} {
	if business, ok := businessPayload(value); ok {
		return business
	}
	return value
}

func normalizeComparable(value interface{}) interface{} {
	switch v := value.(type) {
	case map[string]interface{}:
		out := make(map[string]interface{}, len(v))
		for key, item := range v {
			out[key] = normalizeComparable(item)
		}
		return out
	case []interface{}:
		out := make([]interface{}, len(v))
		for i, item := range v {
			out[i] = normalizeComparable(item)
		}
		return out
	case string:
		return strings.ReplaceAll(v, "\\", "/")
	default:
		return value
	}
}

func mcpResultWrapperPayload(value interface{}) (interface{}, bool) {
	obj, ok := value.(map[string]interface{})
	if !ok {
		return nil, false
	}
	result, ok := obj["result"]
	if !ok {
		return nil, false
	}
	return businessPayload(result)
}

func capturedStdoutPayload(capture captureData) interface{} {
	stdout := strings.TrimSpace(capture.Stdout)
	if stdout == "" {
		return nil
	}
	if streamPayload, ok := capturedJSONLinesPayload(stdout); ok {
		return streamPayload
	}
	var payload interface{}
	if err := json.Unmarshal([]byte(stdout), &payload); err == nil {
		if business, ok := businessPayload(payload); ok {
			return business
		}
		return payload
	}
	return stdout
}

func capturedJSONLinesPayload(stdout string) (interface{}, bool) {
	lines := strings.Split(stdout, "\n")
	rows := make([]interface{}, 0, len(lines))
	table := ""
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var payload interface{}
		if err := json.Unmarshal([]byte(line), &payload); err != nil {
			return nil, false
		}
		business, ok := businessPayload(payload)
		if !ok {
			return nil, false
		}
		businessMap, ok := business.(map[string]interface{})
		if !ok {
			return nil, false
		}
		mode, _ := businessMap["mode"].(string)
		row, hasRow := businessMap["row"]
		if mode != "stream" || !hasRow {
			return nil, false
		}
		if table == "" {
			table, _ = businessMap["table"].(string)
		}
		rows = append(rows, row)
	}
	if len(rows) == 0 {
		return nil, false
	}
	return map[string]interface{}{"table": table, "mode": "stream", "count": float64(len(rows)), "rows": rows}, true
}

func businessPayload(value interface{}) (interface{}, bool) {
	obj, ok := value.(map[string]interface{})
	if !ok {
		return nil, false
	}

	if rawOK, hasOK := obj["ok"]; hasOK {
		okValue, _ := rawOK.(bool)
		if okValue {
			return obj["data"], true
		}
		if errObj, ok := obj["error"].(map[string]interface{}); ok {
			message, _ := errObj["message"].(string)
			return map[string]interface{}{"error": message}, true
		}
		return map[string]interface{}{"error": nil}, true
	}

	content, ok := obj["content"].([]interface{})
	if !ok || len(content) == 0 {
		return nil, false
	}
	first, ok := content[0].(map[string]interface{})
	if !ok {
		return nil, false
	}
	text, ok := first["text"].(string)
	if !ok {
		return nil, false
	}
	if isError, _ := obj["isError"].(bool); isError {
		return map[string]interface{}{"error": strings.TrimPrefix(text, "Error: ")}, true
	}
	var payload interface{}
	if err := json.Unmarshal([]byte(text), &payload); err == nil {
		return payload, true
	}
	return text, true
}

func CompareArtifacts(baseDir string, entry ManifestEntry) []CompareResult {
	results := make([]CompareResult, 0, len(entry.Artifacts))
	for _, artifact := range entry.Artifacts {
		result := CompareResult{Fixture: artifact.Path, Equal: true}
		if artifact.Path == "" {
			result.Equal = false
			result.Reason = "artifact path is required"
			results = append(results, result)
			continue
		}
		path := artifact.Path
		if !filepath.IsAbs(path) {
			path = filepath.Join(baseDir, path)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			result.Equal = false
			result.Reason = fmt.Sprintf("artifact read failed: %v", err)
			results = append(results, result)
			continue
		}
		if artifact.Size > 0 && int64(len(data)) != artifact.Size {
			result.Equal = false
			result.Reason = fmt.Sprintf("artifact size differs: expected %d, got %d", artifact.Size, len(data))
			results = append(results, result)
			continue
		}
		if artifact.SHA256 != "" {
			sum := sha256.Sum256(data)
			got := fmt.Sprintf("%x", sum)
			if !strings.EqualFold(got, artifact.SHA256) {
				result.Equal = false
				result.Reason = fmt.Sprintf("artifact sha256 differs: expected %s, got %s", artifact.SHA256, got)
			}
		}
		results = append(results, result)
	}
	return results
}

type captureData struct {
	ExitCode int
	Stdout   string
	Stderr   string
}

func capturePayload(value interface{}) (captureData, bool) {
	obj, ok := value.(map[string]interface{})
	if !ok {
		return captureData{}, false
	}
	stdout, hasStdout := obj["stdout"].(string)
	if !hasStdout {
		return captureData{}, false
	}
	exitCode := 0
	if raw, ok := obj["exitCode"]; ok {
		switch v := raw.(type) {
		case float64:
			exitCode = int(v)
		case int:
			exitCode = v
		}
	}
	stderr, _ := obj["stderr"].(string)
	return captureData{ExitCode: exitCode, Stdout: stdout, Stderr: stderr}, true
}

func compareCapturedPayload(expected captureData, actual captureData) CompareResult {
	if expected.ExitCode != actual.ExitCode {
		return CompareResult{Equal: false, Reason: fmt.Sprintf("exitCode differs: expected %d, got %d", expected.ExitCode, actual.ExitCode)}
	}
	expectedStdout := strings.TrimSpace(expected.Stdout)
	actualStdout := strings.TrimSpace(actual.Stdout)
	if expectedStdout == "" && actualStdout == "" {
		return CompareResult{Equal: true}
	}
	var expectedJSON interface{}
	expectedJSONErr := json.Unmarshal([]byte(expectedStdout), &expectedJSON)
	var actualJSON interface{}
	actualJSONErr := json.Unmarshal([]byte(actualStdout), &actualJSON)
	if expectedJSONErr == nil && actualJSONErr == nil {
		expectedComparable := normalizeComparable(comparablePayload(expectedJSON))
		actualComparable := normalizeComparable(comparablePayload(actualJSON))
		if reflect.DeepEqual(expectedComparable, actualComparable) {
			return CompareResult{Equal: true}
		}
		return CompareResult{Equal: false, Reason: fmt.Sprintf("captured stdout JSON differs: expected %s, got %s", canonicalJSON(expectedJSON), canonicalJSON(actualJSON))}
	}
	if expectedStdout == actualStdout {
		return CompareResult{Equal: true}
	}
	return CompareResult{Equal: false, Reason: fmt.Sprintf("captured stdout differs: expected %q, got %q", expectedStdout, actualStdout)}
}

func canonicalJSON(value interface{}) string {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(value)
	return string(bytes.TrimSpace(buf.Bytes()))
}
