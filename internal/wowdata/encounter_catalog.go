package wowdata

import (
	"fmt"
	"sort"
	"strings"
)

type EncounterCatalogStore interface {
	Search(table string, field string, query string, limit int) ([]map[string]interface{}, error)
	ForeignKey(table string, field string, value uint32, limit int) ([]map[string]interface{}, error)
}

type ResolvedEncounter struct {
	JournalInstanceID  uint32 `json:"journalInstanceID"`
	JournalEncounterID uint32 `json:"journalEncounterID"`
	InstanceName       string `json:"instanceName"`
	EncounterName      string `json:"encounterName"`
	BossIndex          int    `json:"bossIndex"`
}

func ResolveJournalEncounter(store EncounterCatalogStore, instanceName string, bossIndex int) (*ResolvedEncounter, error) {
	instanceName = strings.TrimSpace(instanceName)
	if instanceName == "" {
		return nil, fmt.Errorf("instance name is required")
	}
	if bossIndex < 1 {
		return nil, fmt.Errorf("boss index must be at least 1")
	}
	instances, err := store.Search("JournalInstance", "Name_lang", instanceName, 32)
	if err != nil {
		return nil, err
	}
	var instance map[string]interface{}
	for _, row := range instances {
		if strings.EqualFold(strings.TrimSpace(rowString(row, "Name_lang")), instanceName) {
			instance = row
			break
		}
	}
	if instance == nil && len(instances) > 0 {
		instance = instances[0]
	}
	instanceID := rowUint32(instance, "ID")
	if instanceID == 0 {
		return nil, fmt.Errorf("journal instance not found: %s", instanceName)
	}
	encounters, err := store.ForeignKey("JournalEncounter", "JournalInstanceID", instanceID, 0)
	if err != nil {
		return nil, err
	}
	sort.Slice(encounters, func(i, j int) bool {
		left, right := rowUint32(encounters[i], "OrderIndex"), rowUint32(encounters[j], "OrderIndex")
		if left == right {
			return rowUint32(encounters[i], "ID") < rowUint32(encounters[j], "ID")
		}
		return left < right
	})
	if bossIndex > len(encounters) {
		return nil, fmt.Errorf("boss index %d exceeds encounter count %d", bossIndex, len(encounters))
	}
	encounter := encounters[bossIndex-1]
	return &ResolvedEncounter{
		JournalInstanceID: instanceID, JournalEncounterID: rowUint32(encounter, "ID"),
		InstanceName: rowString(instance, "Name_lang"), EncounterName: rowString(encounter, "Name_lang"), BossIndex: bossIndex,
	}, nil
}
