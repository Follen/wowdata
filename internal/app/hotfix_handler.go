package app

import (
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"wowdata/internal/dbd"
	"wowdata/internal/hotfix"

	"github.com/spf13/cobra"
)

type HotfixService struct{ Remote hotfix.Source }

func NewHotfixHandler(remote hotfix.Source) func(*cobra.Command, []string) error {
	return (&HotfixService{Remote: remote}).handle
}

func (s *HotfixService) handle(cmd *cobra.Command, args []string) error {
	q, err := hotfixQueryFromCommand(cmd)
	if err != nil {
		return writeJSON(cmd.OutOrStdout(), NewErrorResponse("hotfix query", "hotfix_invalid_query", err.Error()))
	}
	sourceName, _ := cmd.Flags().GetString("source")
	var src hotfix.Source
	var closers []io.Closer
	defer func() {
		for _, c := range closers {
			_ = c.Close()
		}
	}()
	if strings.EqualFold(sourceName, "dbcache") || strings.TrimSpace(mustString(cmd, "dbcache")) != "" {
		path := mustString(cmd, "dbcache")
		if path == "" {
			return writeJSON(cmd.OutOrStdout(), NewErrorResponse("hotfix query", "hotfix_invalid_query", "--dbcache is required for source=dbcache"))
		}
		r, e := hotfix.OpenDBCache(path)
		if e != nil {
			return writeHotfixError(cmd, e)
		}
		closers = append(closers, r)
		sidecarPath := path + ".sidecar"
		if _, e = os.Stat(sidecarPath); os.IsNotExist(e) {
			e = hotfix.WriteSidecar(sidecarPath, r)
		}
		if e != nil {
			return writeHotfixError(cmd, e)
		}
		sidecar, e := hotfix.OpenSidecar(sidecarPath)
		if e != nil {
			return writeHotfixError(cmd, e)
		}
		closers = append(closers, sidecar)
		src = dbCacheSource{r: r, sidecar: sidecar}
	}
	raidbotsPath := mustString(cmd, "raidbots")
	if strings.EqualFold(sourceName, "raidbots") || raidbotsPath != "" {
		if raidbotsPath == "" {
			return writeJSON(cmd.OutOrStdout(), NewErrorResponse("hotfix query", "hotfix_invalid_query", "--raidbots is required for source=raidbots"))
		}
		r, e := hotfix.OpenDBCache(raidbotsPath)
		if e != nil {
			return writeHotfixError(cmd, e)
		}
		closers = append(closers, r)
		src = hotfix.SnapshotSource{Reader: r, Product: q.Product, Build: q.Build, Region: q.Region, Locale: q.Locale, CapturedAt: snapshotTime(raidbotsPath), MaxAge: 30 * 24 * time.Hour}
	}
	if src == nil {
		src = s.Remote
		if raidbotsPath != "" { // attach a recent snapshot only as fallback to Wago.
			r, e := hotfix.OpenDBCache(raidbotsPath)
			if e != nil {
				return writeHotfixError(cmd, e)
			}
			closers = append(closers, r)
			src = hotfix.FallbackSource{Primary: src, Recent: hotfix.SnapshotSource{Reader: r, Product: q.Product, Build: q.Build, Region: q.Region, Locale: q.Locale, CapturedAt: snapshotTime(raidbotsPath), MaxAge: 30 * 24 * time.Hour}}
		}
	}
	if src == nil {
		return writeJSON(cmd.OutOrStdout(), NewErrorResponse("hotfix query", "hotfix_unavailable", "no Hotfix source configured"))
	}
	res, err := src.Query(cmd.Context(), q)
	if err != nil {
		return writeHotfixError(cmd, err)
	}
	raw, _ := cmd.Flags().GetBool("raw")
	decoded, _ := cmd.Flags().GetBool("decoded")
	if decoded {
		if err := decodeHotfixRecords(&res, q, mustString(cmd, "dbd")); err != nil {
			return writeHotfixError(cmd, err)
		}
	}
	for i := range res.Records {
		if !raw {
			res.Records[i].RawData = nil
		}
		if !decoded {
			res.Records[i].Data = nil
		}
	}
	format, _ := cmd.Flags().GetString("format")
	switch strings.ToLower(format) {
	case "", "json":
		return writeJSON(cmd.OutOrStdout(), NewSuccessResponse("hotfix query", res))
	case "jsonl":
		for _, row := range res.Records {
			if err := writeJSONLine(cmd.OutOrStdout(), NewSuccessResponse("hotfix row", row)); err != nil {
				return err
			}
		}
		return nil
	case "csv":
		return writeHotfixCSV(cmd.OutOrStdout(), res)
	default:
		return writeJSON(cmd.OutOrStdout(), NewErrorResponse("hotfix query", "hotfix_invalid_query", "--format must be json, jsonl, or csv"))
	}
}

func snapshotTime(path string) time.Time {
	st, err := os.Stat(path)
	if err != nil {
		return time.Time{}
	}
	return st.ModTime().UTC()
}

type dbCacheSource struct {
	r       *hotfix.Reader
	sidecar *hotfix.Sidecar
}

func (s dbCacheSource) Query(ctx context.Context, q hotfix.Query) (hotfix.Result, error) {
	return s.r.QuerySidecar(s.sidecar, q)
}

func decodeHotfixRecords(res *hotfix.Result, q hotfix.Query, dbdPath string) error {
	needDefinition := false
	for _, record := range res.Records {
		if len(record.RawData) > 0 {
			needDefinition = true
			break
		}
		if _, ok := record.Data.([]any); ok {
			needDefinition = true
			break
		}
	}
	if !needDefinition {
		return nil
	}
	if strings.TrimSpace(dbdPath) == "" {
		return &hotfix.Error{Code: "hotfix_schema_mismatch", Field: "dbd", Message: "--dbd is required to decode positional/raw Hotfix data"}
	}
	f, err := os.Open(filepath.Clean(dbdPath))
	if err != nil {
		return &hotfix.Error{Code: "hotfix_schema_mismatch", Field: "dbd", Message: err.Error()}
	}
	defer f.Close()
	parser, err := dbd.Parse(f)
	if err != nil {
		return &hotfix.Error{Code: "hotfix_schema_mismatch", Field: "dbd", Message: err.Error()}
	}
	entry := parser.GetStructure(q.Build, "")
	if entry == nil {
		parts := strings.Split(q.Build, ".")
		entry = parser.GetStructure(parts[len(parts)-1], "")
	}
	if entry == nil {
		return &hotfix.Error{Code: "hotfix_schema_mismatch", Field: "build", Message: "DBD has no structure for Build " + q.Build}
	}
	for i := range res.Records {
		if len(res.Records[i].RawData) > 0 {
			res.Records[i].Data, err = hotfix.DecodePayload(res.Records[i].RawData, entry)
		} else {
			res.Records[i].Data, err = hotfix.DecodePositionalData(res.Records[i].Data, entry)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

func hotfixQueryFromCommand(cmd *cobra.Command) (hotfix.Query, error) {
	q := hotfix.Query{}
	q.Product, _ = cmd.Flags().GetString("product")
	q.Build, _ = cmd.Flags().GetString("build")
	q.Region, _ = cmd.Flags().GetUint32("region")
	q.Locale, _ = cmd.Flags().GetString("locale")
	q.Table, _ = cmd.Flags().GetString("table")
	if cmd.Flags().Changed("table-hash") {
		v, e := cmd.Flags().GetUint32("table-hash")
		if e != nil {
			return q, e
		}
		q.TableHash = &v
	}
	if cmd.Flags().Changed("record") {
		v, e := cmd.Flags().GetUint32("record")
		if e != nil {
			return q, e
		}
		q.RecordID = &v
	}
	if cmd.Flags().Changed("push") {
		v, e := cmd.Flags().GetInt("push")
		if e != nil {
			return q, e
		}
		x := int32(v)
		q.PushID = &x
	}
	if cmd.Flags().Changed("status") {
		v, e := cmd.Flags().GetInt("status")
		if e != nil || v < 0 || v > 255 {
			if e != nil {
				return q, e
			}
			return q, fmt.Errorf("status must be 0..255")
		}
		x := uint8(v)
		q.Status = &x
	}
	q.Search, _ = cmd.Flags().GetString("search")
	q.Latest, _ = cmd.Flags().GetBool("latest")
	q.Page, _ = cmd.Flags().GetInt("page")
	q.Limit, _ = cmd.Flags().GetInt("limit")
	for name, dst := range map[string]**time.Time{"from": &q.From, "to": &q.To} {
		v, _ := cmd.Flags().GetString(name)
		if strings.TrimSpace(v) != "" {
			t, e := parseHotfixTime(v)
			if e != nil {
				return q, fmt.Errorf("--%s: %w", name, e)
			}
			*dst = &t
		}
	}
	return q, q.Validate()
}
func parseHotfixTime(v string) (time.Time, error) {
	if t, e := time.Parse(time.RFC3339, v); e == nil {
		return t, nil
	}
	return time.Parse("2006-01-02 15:04:05", v)
}
func mustString(cmd *cobra.Command, name string) string {
	v, _ := cmd.Flags().GetString(name)
	return v
}
func writeHotfixError(cmd *cobra.Command, err error) error {
	if e, ok := err.(*hotfix.Error); ok {
		return writeJSON(cmd.OutOrStdout(), NewErrorResponse("hotfix query", e.Code, e.Error()))
	}
	return writeJSON(cmd.OutOrStdout(), NewErrorResponse("hotfix query", "hotfix_query_error", err.Error()))
}
func writeHotfixCSV(w io.Writer, res hotfix.Result) error {
	cw := csv.NewWriter(w)
	if err := cw.Write([]string{"id", "push_id", "record_id", "table_name", "status", "build", "region_id", "locale", "payload_length"}); err != nil {
		return err
	}
	for _, r := range res.Records {
		if err := cw.Write([]string{strconv.FormatUint(r.ID, 10), strconv.FormatInt(int64(r.PushID), 10), strconv.FormatUint(uint64(r.RecordID), 10), r.TableName, strconv.Itoa(int(r.Status)), r.Build, strconv.FormatUint(uint64(r.Region), 10), r.Locale, strconv.Itoa(r.PayloadLength)}); err != nil {
			return err
		}
	}
	cw.Flush()
	return cw.Error()
}
