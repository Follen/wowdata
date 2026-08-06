package dbd

import (
	"bufio"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"

	"wowdata/internal/resource"
)

var (
	reColumn = regexp.MustCompile(`^(int|float|locstring|string)(<[^:]+::[^>]+>)?\s([^\s]+)`)
	reBuild  = regexp.MustCompile(`^BUILD\s(.*)`)
	reRange  = regexp.MustCompile(`([^-]+)-(.*)`)
	reLayout = regexp.MustCompile(`^LAYOUT\s(.*)`)
	reField  = regexp.MustCompile(`^(\$([^$]+)\$)?([^<[]+)(<(u|)(\d+)>)?(\[(\d+)\])?$`)

	reBuildID = regexp.MustCompile(`(\d+)\.(\d+)\.(\d+)\.(\d+)`)
)

type BuildParts struct {
	Major, Minor, Patch, Rev int
}

type BuildRange struct {
	Min, Max string
}

type DBDField struct {
	Name       string
	Type       string
	IsSigned   bool
	IsID       bool
	IsInline   bool
	IsRelation bool
	ArrayLen   int
	Size       int
}

type DBDEntry struct {
	Builds       []string
	BuildRanges  []BuildRange
	LayoutHashes []string
	Fields       []DBDField
}

type Parser struct {
	Columns map[string]string
	Entries []DBDEntry
}

type countingReader struct {
	reader io.Reader
	bytes  int
}

func (r *countingReader) Read(p []byte) (int, error) {
	n, err := r.reader.Read(p)
	r.bytes += n
	return n, err
}

func NewDBDField(name, typ string) DBDField {
	return DBDField{
		Name:     name,
		Type:     typ,
		IsSigned: true,
		IsInline: true,
		ArrayLen: -1,
		Size:     -1,
	}
}

func Parse(r io.Reader) (*Parser, error) {
	p := &Parser{
		Columns: make(map[string]string),
	}

	var lines []string
	counted := &countingReader{reader: r}
	scanner := bufio.NewScanner(counted)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	resource.RecordDBDInput(counted.bytes, len(lines))
	if err := scanner.Err(); err != nil {
		return nil, err
	}

	if err := p.parse(lines); err != nil {
		return nil, err
	}
	return p, nil
}

func (p *Parser) parse(lines []string) error {
	var chunk []string
	for _, line := range lines {
		if strings.TrimSpace(line) != "" {
			chunk = append(chunk, line)
		} else {
			if err := p.parseChunk(chunk); err != nil {
				return err
			}
			chunk = nil
		}
	}
	if len(chunk) > 0 {
		if err := p.parseChunk(chunk); err != nil {
			return err
		}
	}
	if len(p.Columns) == 0 {
		return fmt.Errorf("Invalid DBD: No columns defined.")
	}
	return nil
}

func (p *Parser) parseChunk(chunk []string) error {
	if len(chunk) == 0 {
		return nil
	}
	if chunk[0] == "COLUMNS" {
		return p.parseColumns(chunk)
	}

	entry := DBDEntry{}
	for _, line := range chunk {
		if m := reBuild.FindStringSubmatch(line); m != nil {
			builds := strings.Split(m[1], ",")
			for _, build := range builds {
				build = strings.TrimSpace(build)
				if rm := reRange.FindStringSubmatch(build); rm != nil {
					entry.BuildRanges = append(entry.BuildRanges, BuildRange{Min: rm[1], Max: rm[2]})
				} else {
					entry.Builds = append(entry.Builds, build)
				}
			}
			continue
		}

		if strings.HasPrefix(line, "COMMENT ") {
			continue
		}

		if m := reLayout.FindStringSubmatch(line); m != nil {
			for _, h := range strings.Split(m[1], ",") {
				entry.LayoutHashes = append(entry.LayoutHashes, strings.TrimSpace(h))
			}
			continue
		}

		if m := reField.FindStringSubmatch(line); m != nil {
			fieldName := m[3]
			fieldType, ok := p.Columns[fieldName]
			if !ok {
				return fmt.Errorf("Invalid DBD: No field type defined for %s", fieldName)
			}
			field := NewDBDField(fieldName, fieldType)

			if m[2] != "" {
				for _, ann := range strings.Split(m[2], ",") {
					switch strings.TrimSpace(ann) {
					case "id":
						field.IsID = true
					case "noninline":
						field.IsInline = false
					case "relation":
						field.IsRelation = true
					}
				}
			}
			if m[5] == "u" {
				field.IsSigned = false
			}
			if m[6] != "" {
				field.Size, _ = strconv.Atoi(m[6])
			}
			if m[8] != "" {
				field.ArrayLen, _ = strconv.Atoi(m[8])
			}
			entry.Fields = append(entry.Fields, field)
		}
	}
	p.Entries = append(p.Entries, entry)
	resource.RecordDBDDefinition(len(entry.Fields))
	return nil
}

func (p *Parser) parseColumns(chunk []string) error {
	if len(chunk) < 2 {
		return fmt.Errorf("Invalid DBD: Missing column definitions.")
	}
	for _, line := range chunk[1:] {
		if m := reColumn.FindStringSubmatch(line); m != nil {
			name := strings.TrimSuffix(m[3], "?")
			p.Columns[name] = m[1]
		}
	}
	return nil
}

func (p *Parser) GetStructure(buildID, layoutHash string) *DBDEntry {
	for i := range p.Entries {
		if p.Entries[i].isValidFor(buildID, layoutHash) {
			return &p.Entries[i]
		}
	}
	return nil
}

func (e *DBDEntry) isValidFor(buildID, layoutHash string) bool {
	for _, h := range e.LayoutHashes {
		if h == layoutHash {
			return true
		}
	}
	for _, b := range e.Builds {
		if b == buildID {
			return true
		}
	}
	for _, r := range e.BuildRanges {
		if isBuildInRange(buildID, r.Min, r.Max) {
			return true
		}
	}
	return false
}

func parseBuildID(buildID string) BuildParts {
	parts := reBuildID.FindStringSubmatch(buildID)
	bp := BuildParts{}
	if parts != nil {
		bp.Major, _ = strconv.Atoi(parts[1])
		bp.Minor, _ = strconv.Atoi(parts[2])
		bp.Patch, _ = strconv.Atoi(parts[3])
		bp.Rev, _ = strconv.Atoi(parts[4])
	}
	return bp
}

func isBuildInRange(buildStr, minStr, maxStr string) bool {
	build := parseBuildID(buildStr)
	min := parseBuildID(minStr)
	max := parseBuildID(maxStr)
	return compareBuild(build, min) >= 0 && compareBuild(build, max) <= 0
}

func compareBuild(a, b BuildParts) int {
	for _, pair := range [][2]int{{a.Major, b.Major}, {a.Minor, b.Minor}, {a.Patch, b.Patch}, {a.Rev, b.Rev}} {
		if pair[0] < pair[1] {
			return -1
		}
		if pair[0] > pair[1] {
			return 1
		}
	}
	return 0
}
