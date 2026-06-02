package casc

import "strings"

type VersionEntry struct {
	Product      string
	Name         string
	Path         string
	Hosts        string
	Servers      string
	ConfigPath   string
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
	case "Product":
		e.Product = val
	case "Name":
		e.Product = val
		e.Name = val
	case "Path":
		e.Path = val
	case "Hosts":
		e.Hosts = val
	case "Servers":
		e.Servers = val
	case "ConfigPath":
		e.ConfigPath = val
	case "Region":
		e.Region = val
	case "BuildConfig":
		e.BuildConfig = val
	case "CDNConfig":
		e.CDNConfig = val
	case "BuildKey":
		e.BuildKey = val
		if e.BuildConfig == "" {
			e.BuildConfig = val
		}
	case "CDNKey":
		if e.CDNConfig == "" {
			e.CDNConfig = val
		}
	case "Version":
		e.Version = val
	case "VersionsName":
		e.VersionsName = val
	case "Branch":
		e.Branch = val
	}
}
