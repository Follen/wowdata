package golden

import (
	"bytes"
	"encoding/json"
	"fmt"
	"path/filepath"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"time"

	"wowdata/internal/resource"
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
	Name       string                `json:"name"`
	Group      string                `json:"group,omitempty"`
	Kind       string                `json:"kind,omitempty"`
	Command    string                `json:"command,omitempty"`
	Args       []string              `json:"args,omitempty"`
	Source     string                `json:"source,omitempty"`
	Region     string                `json:"region,omitempty"`
	Product    string                `json:"product,omitempty"`
	Build      string                `json:"build,omitempty"`
	Comparison string                `json:"comparison,omitempty"`
	Fixture    string                `json:"fixture"`
	Actual     string                `json:"actual"`
	Artifacts  []ArtifactExpectation `json:"artifacts,omitempty"`
}

const (
	ComparisonRemoteProductsV1 = "remote-products-v1"
	ComparisonCacheStateV1     = "cache-state-v1"
	ComparisonProfileStateV1   = "profile-state-v1"
)

var hexKeyPattern = regexp.MustCompile(`^[0-9a-f]{32}$`)

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
	return CompareJSONWithMode(expected, actual, "")
}

func CompareJSONWithMode(expected []byte, actual []byte, mode string) (CompareResult, error) {
	var expectedValue interface{}
	if err := json.Unmarshal(expected, &expectedValue); err != nil {
		return CompareResult{Equal: false, Reason: "invalid expected JSON"}, err
	}
	var actualValue interface{}
	if err := json.Unmarshal(actual, &actualValue); err != nil {
		return CompareResult{Equal: false, Reason: "invalid actual JSON"}, err
	}
	if mode == ComparisonRemoteProductsV1 {
		return compareRemoteProducts(expectedValue, actualValue), nil
	}
	if mode == ComparisonCacheStateV1 {
		return compareStateCapture(expectedValue, actualValue, normalizeCacheState), nil
	}
	if mode == ComparisonProfileStateV1 {
		return compareStateCapture(expectedValue, actualValue, normalizeProfileState), nil
	}
	if mode != "" {
		return CompareResult{Equal: false, Reason: "unsupported comparison mode: " + mode}, nil
	}
	if expectedCapture, ok := capturePayload(expectedValue); ok {
		if actualCapture, ok := capturePayload(actualValue); ok {
			return compareCapturedPayload(expectedCapture, actualCapture), nil
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

func compareStateCapture(expected, actual interface{}, normalize func(interface{}) (interface{}, error)) CompareResult {
	expectedCapture, expectedOK := capturePayload(expected)
	actualCapture, actualOK := capturePayload(actual)
	if !expectedOK || !actualOK {
		return CompareResult{Equal: false, Reason: "state comparison requires capture envelopes"}
	}
	if expectedCapture.ExitCode != actualCapture.ExitCode {
		return CompareResult{Equal: false, Reason: fmt.Sprintf("exitCode differs: expected %d, got %d", expectedCapture.ExitCode, actualCapture.ExitCode)}
	}
	expectedStderr, err := comparableCapturedStderr(expectedCapture.Stderr)
	if err != nil {
		return CompareResult{Equal: false, Reason: "invalid expected stderr: " + err.Error()}
	}
	actualStderr, err := comparableCapturedStderr(actualCapture.Stderr)
	if err != nil {
		return CompareResult{Equal: false, Reason: "invalid actual stderr: " + err.Error()}
	}
	if expectedStderr != actualStderr {
		return CompareResult{Equal: false, Reason: fmt.Sprintf("stable stderr differs: expected %q, got %q", expectedStderr, actualStderr)}
	}
	expectedPayload, err := normalize(capturedStdoutPayload(expectedCapture))
	if err != nil {
		return CompareResult{Equal: false, Reason: "invalid expected state: " + err.Error()}
	}
	actualPayload, err := normalize(capturedStdoutPayload(actualCapture))
	if err != nil {
		return CompareResult{Equal: false, Reason: "invalid actual state: " + err.Error()}
	}
	if reflect.DeepEqual(expectedPayload, actualPayload) {
		return CompareResult{Equal: true}
	}
	return CompareResult{Equal: false, Reason: fmt.Sprintf("state payload differs: expected %s, got %s", canonicalJSON(expectedPayload), canonicalJSON(actualPayload))}
}

func normalizeProfileState(value interface{}) (interface{}, error) {
	return normalizeStateFields(value, false)
}

func normalizeCacheState(value interface{}) (interface{}, error) {
	root, ok := value.(map[string]interface{})
	if !ok {
		return normalizeStateFields(value, true)
	}
	if path, hasPath := root["path"]; hasPath {
		if _, ok := path.(string); !ok {
			return nil, fmt.Errorf("path must be a string")
		}
		if removed, ok := numberField(root, "removedBytes"); ok {
			if removed < 0 {
				return nil, fmt.Errorf("removedBytes must be non-negative")
			}
			root["removedBytes"] = float64(0)
		}
	}
	if before, beforeOK := numberField(root, "beforeBytes"); beforeOK {
		after, afterOK := numberField(root, "afterBytes")
		removed, removedOK := root["removed"].([]interface{})
		if !afterOK || !removedOK || before < 0 || after != before || len(removed) != 0 {
			return nil, fmt.Errorf("empty prune must preserve non-negative bytes and remove no entries")
		}
		root["beforeBytes"], root["afterBytes"] = float64(0), float64(0)
	}
	if size, sizeOK := numberField(root, "sizeBytes"); sizeOK {
		usage, usageOK := root["usage"].(map[string]interface{})
		total, totalOK := numberField(usage, "totalBytes")
		derived, derivedOK := numberField(usage, "derivedMetadataBytes")
		payload, payloadOK := numberField(usage, "payloadBytes")
		resume, resumeOK := numberField(usage, "resumeBytes")
		unique, uniqueOK := numberField(usage, "uniquePayloadBytes")
		duplicate, duplicateOK := numberField(usage, "duplicatePayloadBytes")
		maxBytes, maxOK := numberField(root, "maxBytes")
		if !usageOK || !totalOK || !derivedOK || !payloadOK || !resumeOK || !uniqueOK || !duplicateOK || !maxOK || size < 0 || size != total || total != derived || payload != 0 || resume != 0 || unique != 0 || duplicate != 0 || size > maxBytes {
			return nil, fmt.Errorf("empty cache status byte accounting is inconsistent")
		}
		root["sizeBytes"], usage["totalBytes"], usage["derivedMetadataBytes"] = float64(0), float64(0), float64(0)
	}
	return normalizeStateFields(root, true)
}

func normalizeStateFields(value interface{}, cache bool) (interface{}, error) {
	switch current := value.(type) {
	case map[string]interface{}:
		for key, item := range current {
			if !cache && key == "error" {
				text, ok := item.(string)
				if !ok {
					return nil, fmt.Errorf("profile error must be a string")
				}
				normalized := strings.ReplaceAll(text, "\\", "/")
				if marker := strings.Index(normalized, "/profiles/"); strings.HasPrefix(normalized, "open ") && marker >= 0 {
					normalized = "open <WOWDATA_HOME>" + normalized[marker:]
				}
				current[key] = normalized
				continue
			}
			if cache && (key == "path" || key == "clearedPath") {
				if _, ok := item.(string); !ok {
					return nil, fmt.Errorf("%s must be a string", key)
				}
				current[key] = "<CACHE_PATH>"
				continue
			}
			if (cache && key == "initializedAtUtc") || (!cache && key == "updatedAt") {
				text, ok := item.(string)
				if !ok {
					return nil, fmt.Errorf("%s must be an RFC3339 string", key)
				}
				if _, err := time.Parse(time.RFC3339, text); err != nil {
					return nil, fmt.Errorf("%s must be RFC3339: %w", key, err)
				}
				current[key] = "<TIMESTAMP>"
				continue
			}
			normalized, err := normalizeStateFields(item, cache)
			if err != nil {
				return nil, err
			}
			current[key] = normalized
		}
	case []interface{}:
		for index, item := range current {
			normalized, err := normalizeStateFields(item, cache)
			if err != nil {
				return nil, err
			}
			current[index] = normalized
		}
	}
	return value, nil
}

func numberField(value map[string]interface{}, key string) (float64, bool) {
	if value == nil {
		return 0, false
	}
	number, ok := value[key].(float64)
	return number, ok
}

func compareRemoteProducts(expected, actual interface{}) CompareResult {
	expectedCapture, expectedOK := capturePayload(expected)
	actualCapture, actualOK := capturePayload(actual)
	if !expectedOK || !actualOK {
		return CompareResult{Equal: false, Reason: "remote products capture envelope is required"}
	}
	if expectedCapture.ExitCode != actualCapture.ExitCode {
		return CompareResult{Equal: false, Reason: fmt.Sprintf("exitCode differs: expected %d, got %d", expectedCapture.ExitCode, actualCapture.ExitCode)}
	}
	expectedStderr, err := comparableCapturedStderr(expectedCapture.Stderr)
	if err != nil {
		return CompareResult{Equal: false, Reason: "invalid expected stderr: " + err.Error()}
	}
	actualStderr, err := comparableCapturedStderr(actualCapture.Stderr)
	if err != nil {
		return CompareResult{Equal: false, Reason: "invalid actual stderr: " + err.Error()}
	}
	if expectedStderr != actualStderr {
		return CompareResult{Equal: false, Reason: fmt.Sprintf("stable stderr differs: expected %q, got %q", expectedStderr, actualStderr)}
	}
	expectedProducts, expectedSource, err := remoteProductsPayload(expected)
	if err != nil {
		return CompareResult{Equal: false, Reason: "invalid expected remote products: " + err.Error()}
	}
	actualProducts, actualSource, err := remoteProductsPayload(actual)
	if err != nil {
		return CompareResult{Equal: false, Reason: "invalid actual remote products: " + err.Error()}
	}
	if expectedSource != actualSource {
		return CompareResult{Equal: false, Reason: fmt.Sprintf("remote products source differs: expected %q, got %q", expectedSource, actualSource)}
	}
	expectedShape, err := remoteProductsShape(expectedProducts)
	if err != nil {
		return CompareResult{Equal: false, Reason: "invalid expected remote products: " + err.Error()}
	}
	actualShape, err := remoteProductsShape(actualProducts)
	if err != nil {
		return CompareResult{Equal: false, Reason: "invalid actual remote products: " + err.Error()}
	}
	if !reflect.DeepEqual(expectedShape, actualShape) {
		return CompareResult{Equal: false, Reason: fmt.Sprintf("remote products contract differs: expected %s, got %s", canonicalJSON(expectedShape), canonicalJSON(actualShape))}
	}
	return CompareResult{Equal: true}
}

func remoteProductsPayload(value interface{}) ([]interface{}, string, error) {
	payload, ok := capturePayload(value)
	if !ok {
		return nil, "", fmt.Errorf("capture envelope is required")
	}
	business := capturedStdoutPayload(payload)
	obj, ok := business.(map[string]interface{})
	if !ok {
		return nil, "", fmt.Errorf("business payload must be an object")
	}
	source, _ := obj["source"].(string)
	products, ok := obj["products"].([]interface{})
	if source == "" || !ok || len(products) == 0 {
		return nil, "", fmt.Errorf("source and non-empty products are required")
	}
	return products, source, nil
}

func remoteProductsShape(products []interface{}) ([]interface{}, error) {
	shape := make([]interface{}, 0, len(products))
	seen := make(map[string]struct{}, len(products))
	for index, raw := range products {
		product, ok := raw.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("product %d must be an object", index)
		}
		name, _ := product["product"].(string)
		region, _ := product["region"].(string)
		version, _ := product["version"].(string)
		buildID, _ := product["buildId"].(string)
		label, _ := product["label"].(string)
		buildKey, _ := product["buildConfigKey"].(string)
		cdnKey, _ := product["cdnConfigKey"].(string)
		buildIndex, indexOK := product["buildIndex"].(float64)
		locales, localesOK := product["locales"].([]interface{})
		if name == "" || region == "" || version == "" || buildID == "" || label == "" || !indexOK || buildIndex != float64(index) || !localesOK || len(locales) == 0 {
			return nil, fmt.Errorf("product %d has incomplete identity", index)
		}
		if _, duplicate := seen[name]; duplicate {
			return nil, fmt.Errorf("duplicate product %q", name)
		}
		seen[name] = struct{}{}
		if !strings.HasSuffix(version, "."+buildID) || !strings.Contains(label, version) {
			return nil, fmt.Errorf("product %q version/build identity is inconsistent", name)
		}
		if !hexKeyPattern.MatchString(buildKey) || !hexKeyPattern.MatchString(cdnKey) {
			return nil, fmt.Errorf("product %q has invalid config key", name)
		}
		seenLocales := make(map[string]struct{}, len(locales))
		for _, rawLocale := range locales {
			locale, ok := rawLocale.(string)
			if !ok || locale == "" {
				return nil, fmt.Errorf("product %q has invalid locale", name)
			}
			if _, duplicate := seenLocales[locale]; duplicate {
				return nil, fmt.Errorf("product %q has duplicate locale %q", name, locale)
			}
			seenLocales[locale] = struct{}{}
		}
		shape = append(shape, map[string]interface{}{"product": name, "region": region, "locales": locales})
	}
	return shape, nil
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

	return nil, false
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
		data, err := resource.ReadFile(path)
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
			sum := resource.SumSHA256(data)
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
	expectedStderr, err := comparableCapturedStderr(expected.Stderr)
	if err != nil {
		return CompareResult{Equal: false, Reason: "invalid expected stderr: " + err.Error()}
	}
	actualStderr, err := comparableCapturedStderr(actual.Stderr)
	if err != nil {
		return CompareResult{Equal: false, Reason: "invalid actual stderr: " + err.Error()}
	}
	if expectedStderr != actualStderr {
		return CompareResult{Equal: false, Reason: fmt.Sprintf("stable stderr differs: expected %q, got %q", expectedStderr, actualStderr)}
	}
	expectedStdout := strings.TrimSpace(expected.Stdout)
	actualStdout := strings.TrimSpace(actual.Stdout)
	if expectedStdout == "" && actualStdout == "" {
		return CompareResult{Equal: true}
	}
	if expectedPayload, actualPayload := capturedStdoutPayload(expected), capturedStdoutPayload(actual); expectedPayload != nil && actualPayload != nil {
		return compareBusinessPayloads(expectedPayload, actualPayload)
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

func comparableCapturedStderr(stderr string) (string, error) {
	stable := make([]string, 0)
	for _, raw := range strings.Split(strings.ReplaceAll(stderr, "\r\n", "\n"), "\n") {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "download ") {
			if err := validateDownloadEvent(line); err != nil {
				return "", err
			}
			continue
		}
		stable = append(stable, line)
	}
	return strings.Join(stable, "\n"), nil
}

func validateDownloadEvent(line string) error {
	fields := strings.Fields(line)
	values := make(map[string]string, len(fields)-1)
	for _, field := range fields[1:] {
		key, value, ok := strings.Cut(field, "=")
		if !ok || key == "" || value == "" {
			return fmt.Errorf("malformed download event field %q", field)
		}
		values[key] = value
	}
	for _, key := range []string{"method", "url", "bytes", "workers", "duration"} {
		if values[key] == "" {
			return fmt.Errorf("download event is missing %s", key)
		}
	}
	if !strings.HasPrefix(values["url"], "https://") {
		return fmt.Errorf("download event URL must use HTTPS")
	}
	bytes, err := strconv.ParseInt(values["bytes"], 10, 64)
	if err != nil || bytes < 0 {
		return fmt.Errorf("download event bytes are invalid")
	}
	workers, err := strconv.Atoi(values["workers"])
	if err != nil || workers < 1 {
		return fmt.Errorf("download event workers are invalid")
	}
	duration, err := time.ParseDuration(values["duration"])
	if err != nil || duration < 0 {
		return fmt.Errorf("download event duration is invalid")
	}
	return nil
}

func canonicalJSON(value interface{}) string {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(value)
	resource.RecordJSONEncode(buf.Len())
	return string(bytes.TrimSpace(buf.Bytes()))
}
