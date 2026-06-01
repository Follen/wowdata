package spell

import "wowdata/internal/wowdata"

type Service struct {
	service *wowdata.SpellService
}

func NewService(service *wowdata.SpellService) *Service {
	return &Service{service: service}
}
