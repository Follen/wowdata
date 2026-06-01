package encounter

import "wowdata/internal/wowdata"

type Service struct {
	service *wowdata.EncounterService
}

func NewService(service *wowdata.EncounterService) *Service {
	return &Service{service: service}
}
