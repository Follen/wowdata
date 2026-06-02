package creature

import "wowdata/internal/shared/wowdata"

type Service struct {
	service *wowdata.CreatureService
}

func NewService(service *wowdata.CreatureService) *Service {
	return &Service{service: service}
}
