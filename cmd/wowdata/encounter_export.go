package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"unicode"

	"wowdata/internal/app"
	wexport "wowdata/internal/export"
	"wowdata/internal/resource"
	appruntime "wowdata/internal/runtime"
	"wowdata/internal/storage"
	"wowdata/internal/wowdata"

	"github.com/spf13/cobra"
)

type encounterExportItem struct {
	Status       string `json:"status"`
	SemanticType string `json:"semanticType"`
	SemanticID   uint32 `json:"semanticID"`
	SectionID    uint32 `json:"sectionID,omitempty"`
	SpellID      uint32 `json:"spellID"`
	Name         string `json:"name"`
	FileDataID   uint32 `json:"fileDataID"`
	Path         string `json:"path,omitempty"`
	Format       string `json:"format"`
	Width        int    `json:"width,omitempty"`
	Height       int    `json:"height,omitempty"`
	Bytes        int    `json:"bytes,omitempty"`
	SHA256       string `json:"sha256,omitempty"`
	Error        string `json:"error,omitempty"`
}

type encounterExportManifest struct {
	Schema                string                `json:"schema"`
	Source                string                `json:"source"`
	Region                string                `json:"region"`
	Product               string                `json:"product"`
	Build                 string                `json:"build"`
	BuildKey              string                `json:"buildKey"`
	Locale                string                `json:"locale"`
	InstanceName          string                `json:"instanceName"`
	EncounterName         string                `json:"encounterName"`
	JournalInstanceID     uint32                `json:"journalInstanceID,omitempty"`
	JournalEncounterID    uint32                `json:"journalEncounterID"`
	BossIndex             int                   `json:"bossIndex"`
	LogicalItemCount      int                   `json:"logicalItemCount"`
	UniqueFileDataIDCount int                   `json:"uniqueFileDataIDCount"`
	Items                 []encounterExportItem `json:"items"`
}

type encounterLogicalSkill struct {
	sectionID uint32
	spellID   uint32
	name      string
	fileID    uint32
}

func newEncounterRuntimeHandler(rt *Runtime, encounterSvc *wowdata.EncounterService, spellSvc *wowdata.SpellService, icons appruntime.IconStore) func(*cobra.Command, []string) error {
	getHandler := app.NewEncounterHandler(encounterSvc)
	return func(cmd *cobra.Command, args []string) error {
		if cmd.Name() != "export" {
			return getHandler(cmd, args)
		}
		return exportEncounter(cmd, rt, encounterSvc, spellSvc, icons)
	}
}

func exportEncounter(cmd *cobra.Command, rt *Runtime, encounterSvc *wowdata.EncounterService, spellSvc *wowdata.SpellService, icons appruntime.IconStore) error {
	outputDir, _ := cmd.Flags().GetString("output")
	instanceName, _ := cmd.Flags().GetString("instance")
	bossIndex, _ := cmd.Flags().GetInt("boss")
	encounterID, _ := cmd.Flags().GetUint32("journal-encounter-id")
	maxDepth, _ := cmd.Flags().GetInt("max-depth")
	if strings.TrimSpace(outputDir) == "" {
		return app.NewResponseWriter(cmd.OutOrStdout()).Error("encounter export", "missing_argument", "--output is required")
	}
	resolved := &wowdata.ResolvedEncounter{JournalEncounterID: encounterID, InstanceName: instanceName, BossIndex: bossIndex}
	if encounterID == 0 {
		var err error
		resolved, err = wowdata.ResolveJournalEncounter(rt.DB2, instanceName, bossIndex)
		if err != nil {
			return app.NewResponseWriter(cmd.OutOrStdout()).Error("encounter export", "encounter_not_found", err.Error())
		}
	}
	discoveryStage := resource.StartStage("resource-discovery", resource.StageOptions{Wave: resource.StageWaveResourceSecondWave})
	encounter := encounterSvc.GetEncounter(resolved.JournalEncounterID)
	spellInfo := spellSvc.GetSpellInfoBatch(encounter.SpellIDs, maxDepth)
	logical := collectEncounterLogicalSkills(encounter.Sections, spellInfo)
	if len(logical) == 0 {
		resource.FinishStage(discoveryStage, fmt.Errorf("no icon-bearing skills"))
		return app.NewResponseWriter(cmd.OutOrStdout()).Error("encounter export", "no_skills", "encounter contains no icon-bearing skills")
	}
	absOutput, err := filepath.Abs(outputDir)
	if err != nil {
		return app.NewResponseWriter(cmd.OutOrStdout()).Error("encounter export", "output_error", err.Error())
	}
	if err := os.MkdirAll(absOutput, 0o755); err != nil {
		return app.NewResponseWriter(cmd.OutOrStdout()).Error("encounter export", "output_error", err.Error())
	}
	allFileIDs := encounterFileDataIDs(logical)
	resource.FinishStage(discoveryStage, nil)
	manifest := buildEncounterManifest(rt, resolved, len(allFileIDs), nil)
	planned := planEncounterIconOutputs(absOutput, logical)
	reused, pendingFileIDs := reusableEncounterIconOutputs(planned, loadEncounterExportManifest(absOutput), manifest)
	prepareErrors, ensureStage := prepareEncounterFileDataIDsTrace(rt, pendingFileIDs, discoveryStage)
	renderIDs := make([]uint32, 0, len(pendingFileIDs))
	for _, fileID := range pendingFileIDs {
		if prepareErrors[fileID] == nil {
			renderIDs = append(renderIDs, fileID)
		}
	}
	imageWorkers := rt.Config.Workers
	var scheduler *resource.Scheduler
	if rt.CASC != nil {
		plan := rt.CASC.ResourcePlan()
		imageWorkers = plan.ImageWorkers
		scheduler = rt.CASC.ResourceScheduler()
	}
	imageBatchStage := resource.StartStage("image-read-decode-batch", resource.StageOptions{ParentID: discoveryStage, Wave: resource.StageWaveResourceSecondWave, DependsOn: []resource.StageDependency{resource.Dependency(ensureStage, resource.StageRelationHard)}})
	rendered, renderErrors := renderEncounterIconsWithStage(icons, renderIDs, imageWorkers, scheduler, imageBatchStage)
	resource.FinishStage(imageBatchStage, nil)
	for fileID, prepareErr := range prepareErrors {
		renderErrors[fileID] = prepareErr
	}
	writeBatchStage := resource.StartStage("image-write-batch", resource.StageOptions{ParentID: discoveryStage, Wave: resource.StageWaveResourceSecondWave, DependsOn: []resource.StageDependency{resource.Dependency(imageBatchStage, resource.StageRelationHard)}})
	items, failures := writePlannedEncounterIconOutputsWithStage(planned, rendered, renderErrors, reused, writeBatchStage)
	resource.FinishStage(writeBatchStage, nil)
	manifest.Items = items
	manifest.LogicalItemCount = len(items)
	manifestPath := filepath.Join(absOutput, "manifest.json")
	manifestStage := resource.StartStage("manifest-write", resource.StageOptions{ParentID: discoveryStage, Wave: resource.StageWaveResourceSecondWave, DependsOn: []resource.StageDependency{resource.Dependency(writeBatchStage, resource.StageRelationHard)}})
	if err := storage.AtomicWriteJSON(manifestPath, manifest, 0o644); err != nil {
		resource.FinishStage(manifestStage, err)
		return app.NewResponseWriter(cmd.OutOrStdout()).Error("encounter export", "manifest_error", err.Error())
	}
	resource.FinishStage(manifestStage, nil)
	data := map[string]interface{}{
		"encounter": encounter, "spells": spellInfo.Spells, "manifest": manifest,
		"manifestPath": manifestPath, "outputDir": absOutput,
	}
	if err := app.NewResponseWriter(cmd.OutOrStdout()).Success("encounter export", data); err != nil {
		return err
	}
	if failures > 0 {
		return &app.CommandError{Code: "partial_failure"}
	}
	return nil
}

func encounterFileDataIDs(logical []encounterLogicalSkill) []uint32 {
	fileIDs := make([]uint32, 0, len(logical))
	for _, skill := range logical {
		if skill.fileID != 0 {
			fileIDs = append(fileIDs, skill.fileID)
		}
	}
	return uniqueUint32(fileIDs)
}

func prepareEncounterFileDataIDs(rt *Runtime, fileIDs []uint32) map[uint32]error {
	return prepareEncounterFileDataIDsWithStage(rt, fileIDs, 0)
}

func prepareEncounterFileDataIDsWithStage(rt *Runtime, fileIDs []uint32, parent resource.StageID) map[uint32]error {
	errorsByID, _ := prepareEncounterFileDataIDsTrace(rt, fileIDs, parent)
	return errorsByID
}

func prepareEncounterFileDataIDsTrace(rt *Runtime, fileIDs []uint32, parent resource.StageID) (map[uint32]error, resource.StageID) {
	errorsByID := make(map[uint32]error)
	if len(fileIDs) == 0 {
		complete := resource.StartStage("ensure-files-complete", resource.StageOptions{ParentID: parent, Wave: resource.StageWaveResourceSecondWave, DependsOn: []resource.StageDependency{resource.Dependency(parent, resource.StageRelationHard)}})
		resource.FinishStage(complete, nil)
		return errorsByID, complete
	}
	batchStage := resource.StartStage("ensure-files-batch-request", resource.StageOptions{ParentID: parent, Wave: resource.StageWaveResourceSecondWave, DependsOn: []resource.StageDependency{resource.Dependency(parent, resource.StageRelationHard)}})
	if err := rt.EnsureFileDataIDsWithStage(fileIDs, batchStage); err == nil {
		resource.FinishStage(batchStage, nil)
		complete := resource.StartStage("ensure-files-complete", resource.StageOptions{ParentID: parent, Wave: resource.StageWaveResourceSecondWave, DependsOn: []resource.StageDependency{resource.Dependency(batchStage, resource.StageRelationJoin)}})
		resource.FinishStage(complete, nil)
		return errorsByID, complete
	} else {
		resource.FinishStage(batchStage, err)
	}
	retries := make([]resource.StageID, 0, len(fileIDs))
	for _, fileID := range fileIDs {
		retryStage := resource.StartStage("ensure-files-single-retry", resource.StageOptions{ParentID: parent, Wave: resource.StageWaveResourceSecondWave, Instance: fmt.Sprint(fileID), Condition: "batch-failed", DependsOn: []resource.StageDependency{resource.Dependency(batchStage, resource.StageRelationFallback)}})
		retries = append(retries, retryStage)
		if err := rt.EnsureFileDataIDsWithStage([]uint32{fileID}, retryStage); err != nil {
			resource.FinishStage(retryStage, err)
			errorsByID[fileID] = err
		} else {
			resource.FinishStage(retryStage, nil)
		}
	}
	dependencies := make([]resource.StageDependency, 0, len(retries))
	for _, retry := range retries {
		dependencies = append(dependencies, resource.Dependency(retry, resource.StageRelationJoin))
	}
	complete := resource.StartStage("ensure-files-complete", resource.StageOptions{ParentID: parent, Wave: resource.StageWaveResourceSecondWave, Condition: "batch-failed", DependsOn: dependencies})
	resource.FinishStage(complete, nil)
	return errorsByID, complete
}

func collectEncounterLogicalSkills(sections []*wowdata.EncounterSection, spellInfo *wowdata.SpellInfo) []encounterLogicalSkill {
	result := make([]encounterLogicalSkill, 0)
	seenSpellIDs := make(map[uint32]bool)
	var visit func([]*wowdata.EncounterSection)
	visit = func(current []*wowdata.EncounterSection) {
		for _, section := range current {
			if section.SpellID != 0 && !seenSpellIDs[section.SpellID] {
				seenSpellIDs[section.SpellID] = true
				spell := spellInfo.Spells[section.SpellID]
				name := ""
				if spell.Name != nil {
					name = strings.TrimSpace(fmt.Sprint(spell.Name))
				}
				if name == "" {
					name = strings.TrimSpace(section.Title)
				}
				if name == "" {
					name = fmt.Sprintf("spell-%d", section.SpellID)
				}
				result = append(result, encounterLogicalSkill{sectionID: section.ID, spellID: section.SpellID, name: name, fileID: spellIconFileDataID(spell)})
			}
			visit(section.Children)
		}
	}
	visit(sections)
	return result
}

func spellIconFileDataID(spell wowdata.DetailedSpellInfo) uint32 {
	misc, _ := spell.Misc.(map[string]interface{})
	switch value := misc["SpellIconFileDataID"].(type) {
	case int:
		return uint32(value)
	case int32:
		return uint32(value)
	case int64:
		return uint32(value)
	case uint32:
		return value
	case uint64:
		return uint32(value)
	case float64:
		return uint32(value)
	default:
		return 0
	}
}

func renderEncounterIcons(store appruntime.IconStore, fileIDs []uint32, workers int, scheduler *resource.Scheduler) (map[uint32]*wexport.RenderedIcon, map[uint32]error) {
	return renderEncounterIconsWithStage(store, fileIDs, workers, scheduler, 0)
}

func renderEncounterIconsWithStage(store appruntime.IconStore, fileIDs []uint32, workers int, scheduler *resource.Scheduler, parent resource.StageID) (map[uint32]*wexport.RenderedIcon, map[uint32]error) {
	rendered := make(map[uint32]*wexport.RenderedIcon, len(fileIDs))
	errorsByID := make(map[uint32]error)
	if len(fileIDs) == 0 {
		return rendered, errorsByID
	}
	if workers < 1 {
		workers = 1
	}
	if workers > len(fileIDs) {
		workers = len(fileIDs)
	}
	jobs := make(chan uint32)
	var mu sync.Mutex
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for fileID := range jobs {
				imageStage := resource.StartStage("image-decode-png-encode", resource.StageOptions{ParentID: parent, Wave: resource.StageWaveResourceSecondWave, Instance: fmt.Sprint(fileID), DependsOn: []resource.StageDependency{resource.Dependency(parent, resource.StageRelationHard)}})
				release, err := scheduler.Acquire(context.Background(), resource.ImageDecodeWritePool)
				if err != nil {
					resource.FinishStage(imageStage, err)
					mu.Lock()
					errorsByID[fileID] = err
					mu.Unlock()
					continue
				}
				data, err := store.ReadByID(fileID)
				var icon *wexport.RenderedIcon
				if err == nil {
					icon, err = wexport.RenderIconPNG(data, 0)
				}
				release()
				resource.FinishStage(imageStage, err)
				mu.Lock()
				if err != nil {
					errorsByID[fileID] = err
				} else {
					rendered[fileID] = icon
				}
				mu.Unlock()
			}
		}()
	}
	for _, fileID := range fileIDs {
		jobs <- fileID
	}
	close(jobs)
	wg.Wait()
	return rendered, errorsByID
}

func writeEncounterIconOutputs(outputDir string, logical []encounterLogicalSkill, rendered map[uint32]*wexport.RenderedIcon, renderErrors map[uint32]error) ([]encounterExportItem, int) {
	return writePlannedEncounterIconOutputs(planEncounterIconOutputs(outputDir, logical), rendered, renderErrors, nil)
}

func planEncounterIconOutputs(outputDir string, logical []encounterLogicalSkill) []encounterExportItem {
	items := make([]encounterExportItem, 0, len(logical))
	usedNames := make(map[string]bool)
	for _, skill := range logical {
		item := encounterExportItem{Status: "success", SemanticType: "journal-section", SemanticID: skill.sectionID, SectionID: skill.sectionID, SpellID: skill.spellID, Name: skill.name, FileDataID: skill.fileID, Format: "png"}
		base := fmt.Sprintf("%d-%s", skill.spellID, sanitizeExportName(skill.name))
		if usedNames[strings.ToLower(base)] {
			base = fmt.Sprintf("%s-section-%d", base, skill.sectionID)
		}
		usedNames[strings.ToLower(base)] = true
		item.Path = filepath.Join(outputDir, base+".png")
		items = append(items, item)
	}
	return items
}

func writePlannedEncounterIconOutputs(planned []encounterExportItem, rendered map[uint32]*wexport.RenderedIcon, renderErrors map[uint32]error, reused map[int]encounterExportItem) ([]encounterExportItem, int) {
	return writePlannedEncounterIconOutputsWithStage(planned, rendered, renderErrors, reused, 0)
}

func writePlannedEncounterIconOutputsWithStage(planned []encounterExportItem, rendered map[uint32]*wexport.RenderedIcon, renderErrors map[uint32]error, reused map[int]encounterExportItem, parent resource.StageID) ([]encounterExportItem, int) {
	items := make([]encounterExportItem, 0, len(planned))
	failures := 0
	for index, item := range planned {
		if existing, ok := reused[index]; ok {
			items = append(items, existing)
			continue
		}
		if item.FileDataID == 0 {
			item.Status, item.Path, item.Error = "error", "", "spell has no icon FileDataID"
			failures++
			items = append(items, item)
			continue
		}
		if err := renderErrors[item.FileDataID]; err != nil {
			item.Status, item.Path, item.Error = "error", "", err.Error()
			failures++
			items = append(items, item)
			continue
		}
		icon := rendered[item.FileDataID]
		if icon == nil {
			item.Status, item.Path, item.Error = "error", "", "icon was not rendered"
			failures++
			items = append(items, item)
			continue
		}
		status := "success"
		writeStage := resource.StartStage("image-write", resource.StageOptions{ParentID: parent, Wave: resource.StageWaveResourceSecondWave, Instance: fmt.Sprint(item.FileDataID), DependsOn: []resource.StageDependency{resource.Dependency(parent, resource.StageRelationHard)}})
		if existing, err := resource.ReadFile(item.Path); err == nil && fmt.Sprintf("%x", resource.SumSHA256(existing)) == icon.SHA256 {
			status = "reused"
			resource.FinishStage(writeStage, nil)
		} else if err := storage.AtomicWriteFile(item.Path, icon.Data, 0o644); err != nil {
			resource.FinishStage(writeStage, err)
			item.Status, item.Path, item.Error = "error", "", err.Error()
			failures++
			items = append(items, item)
			continue
		} else {
			resource.FinishStage(writeStage, nil)
		}
		item.Status, item.Width, item.Height, item.Bytes, item.SHA256 = status, icon.Width, icon.Height, len(icon.Data), icon.SHA256
		items = append(items, item)
	}
	return items, failures
}

func loadEncounterExportManifest(outputDir string) *encounterExportManifest {
	data, err := resource.ReadFile(filepath.Join(outputDir, "manifest.json"))
	if err != nil {
		return nil
	}
	var manifest encounterExportManifest
	if json.Unmarshal(data, &manifest) != nil {
		return nil
	}
	return &manifest
}

func reusableEncounterIconOutputs(planned []encounterExportItem, previous *encounterExportManifest, current encounterExportManifest) (map[int]encounterExportItem, []uint32) {
	reused := make(map[int]encounterExportItem)
	pending := make([]uint32, 0, len(planned))
	if previous == nil || !sameEncounterManifestIdentity(*previous, current) {
		return reused, encounterFileDataIDsFromItems(planned)
	}
	bySemanticID := make(map[uint32]encounterExportItem, len(previous.Items))
	for _, item := range previous.Items {
		if item.SemanticType == "journal-section" {
			bySemanticID[item.SemanticID] = item
		}
	}
	for index, item := range planned {
		prior, ok := bySemanticID[item.SemanticID]
		if ok && reusableEncounterItem(prior, item) {
			prior.Status = "reused"
			reused[index] = prior
			continue
		}
		if item.FileDataID != 0 {
			pending = append(pending, item.FileDataID)
		}
	}
	return reused, uniqueUint32(pending)
}

func reusableEncounterItem(previous, planned encounterExportItem) bool {
	if previous.Status != "success" && previous.Status != "reused" {
		return false
	}
	if previous.SemanticType != planned.SemanticType || previous.SemanticID != planned.SemanticID || previous.SectionID != planned.SectionID || previous.SpellID != planned.SpellID || previous.Name != planned.Name || previous.FileDataID != planned.FileDataID || previous.Format != planned.Format || filepath.Clean(previous.Path) != filepath.Clean(planned.Path) || previous.SHA256 == "" {
		return false
	}
	data, err := resource.ReadFile(planned.Path)
	return err == nil && len(data) == previous.Bytes && fmt.Sprintf("%x", resource.SumSHA256(data)) == previous.SHA256
}

func encounterFileDataIDsFromItems(items []encounterExportItem) []uint32 {
	fileIDs := make([]uint32, 0, len(items))
	for _, item := range items {
		if item.FileDataID != 0 {
			fileIDs = append(fileIDs, item.FileDataID)
		}
	}
	return uniqueUint32(fileIDs)
}

func sameEncounterManifestIdentity(left, right encounterExportManifest) bool {
	return left.Schema == right.Schema && left.Source == right.Source && left.Region == right.Region && left.Product == right.Product && left.Build == right.Build && left.BuildKey == right.BuildKey && left.Locale == right.Locale && left.InstanceName == right.InstanceName && left.EncounterName == right.EncounterName && left.JournalInstanceID == right.JournalInstanceID && left.JournalEncounterID == right.JournalEncounterID && left.BossIndex == right.BossIndex
}

func buildEncounterManifest(rt *Runtime, resolved *wowdata.ResolvedEncounter, uniqueCount int, items []encounterExportItem) encounterExportManifest {
	rt.mu.Lock()
	target := rt.Target
	remote := rt.CASC
	local := rt.Local
	rt.mu.Unlock()
	build, buildKey := "", ""
	if remote != nil {
		build, buildKey = remote.GetBuildName(), remote.GetBuildKey()
	} else if local != nil {
		build, buildKey = local.GetBuildName(), local.GetBuildKey()
	}
	return encounterExportManifest{Schema: "wowdata.encounter-export.v1", Source: target.Source, Region: target.Region, Product: target.Product, Build: build, BuildKey: buildKey, Locale: target.Locale, InstanceName: resolved.InstanceName, EncounterName: resolved.EncounterName, JournalInstanceID: resolved.JournalInstanceID, JournalEncounterID: resolved.JournalEncounterID, BossIndex: resolved.BossIndex, LogicalItemCount: len(items), UniqueFileDataIDCount: uniqueCount, Items: items}
}

func sanitizeExportName(value string) string {
	value = strings.Map(func(r rune) rune {
		if r < 32 || strings.ContainsRune(`<>:"/\|?*`, r) || unicode.IsControl(r) {
			return '-'
		}
		return r
	}, strings.TrimSpace(value))
	value = strings.Trim(value, " .-")
	if value == "" {
		return "unnamed"
	}
	return value
}
