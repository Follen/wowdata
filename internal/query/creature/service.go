package creature

import "wowdata/internal/wowdata"

type Service struct {
	service *wowdata.CreatureService
}

func NewService(service *wowdata.CreatureService) *Service {
	return &Service{service: service}
}
