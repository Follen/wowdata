package query

import (
	"wowdata/internal/query/creature"
	"wowdata/internal/query/db2"
	"wowdata/internal/query/decor"
	"wowdata/internal/query/encounter"
	"wowdata/internal/query/file"
	"wowdata/internal/query/icon"
	"wowdata/internal/query/item"
	"wowdata/internal/query/spell"
	"wowdata/internal/query/video"
)

type Services struct {
	DB2       *db2.Service
	File      *file.Service
	Icon      *icon.Service
	Item      *item.Service
	Spell     *spell.Service
	Creature  *creature.Service
	Encounter *encounter.Service
	Decor     *decor.Service
	Video     *video.Service
}
