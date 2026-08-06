package casc

import (
	"fmt"
	"regexp"
	"strings"

	"wowdata/internal/resource"
)

var cdnKeyPattern = regexp.MustCompile(`^([^\s]+)\s?=\s?(.*)`)

func normalizeKey(key string) string {
	parts := strings.Split(key, "-")
	if len(parts) <= 1 {
		return key
	}
	for i := 1; i < len(parts); i++ {
		p := parts[i]
		if len(p) > 0 {
			parts[i] = strings.ToUpper(p[:1]) + p[1:]
		}
	}
	return strings.Join(parts, "")
}

func ParseCDNConfig(data string) (result map[string]string, err error) {
	defer func() {
		if err == nil {
			resource.RecordCASCMetadata(len(data))
		}
	}()
	lines := strings.Split(data, "\n")

	hasHeader := len(lines) > 0 && strings.HasPrefix(strings.TrimSpace(lines[0]), "# ")
	if !hasHeader {
		return nil, fmt.Errorf("invalid CDN config: unexpected start of config")
	}

	entries := make(map[string]string)
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		m := cdnKeyPattern.FindStringSubmatch(line)
		if m == nil {
			return nil, fmt.Errorf("invalid token encountered parsing CDN config")
		}
		entries[normalizeKey(m[1])] = m[2]
	}
	return entries, nil
}
