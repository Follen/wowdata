package casc

import "strings"

type VersionEntry struct {
	Product      string
	Region       string
	BuildConfig  string
	CDNConfig    string
	BuildKey     string
	Version      string
	VersionsName string
	Branch       string
}

func ParseVersionConfig(data string) []VersionEntry {
	lines := strings.Split(data, "\n")
	if len(lines) < 2 {
		return nil
	}

	headers := strings.Split(lines[0], "|")
	fields := make([]string, len(headers))
	for i, h := range headers {
		fields[i] = strings.ReplaceAll(strings.SplitN(h, "!", 2)[0], " ", "")
	}

	var entries []VersionEntry
	for _, line := range lines[1:] {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.Split(line, "|")
		e := VersionEntry{}
		for i, field := range fields {
			val := ""
			if i < len(parts) {
				val = parts[i]
			}
			setField(&e, field, val)
		}
		entries = append(entries, e)
	}
	return entries
}

func setField(e *VersionEntry, field, val string) {
	switch field {
	case "Product", "Name":
		e.Product = val
	case "Region":
		e.Region = val
	case "BuildConfig":
		e.BuildConfig = val
	case "CDNConfig":
		e.CDNConfig = val
	case "BuildKey":
		e.BuildKey = val
	case "Version":
		e.Version = val
	case "VersionsName":
		e.VersionsName = val
	case "Branch":
		e.Branch = val
	}
}
