package spell

import "wowdata/internal/shared/wowdata"

type Service struct {
	service *wowdata.SpellService
}

func NewService(service *wowdata.SpellService) *Service {
	return &Service{service: service}
}
