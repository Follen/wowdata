package creature

import "wowdata/internal/wowdata"

type Service struct {
	service *wowdata.CreatureService
}

func NewService(service *wowdata.CreatureService) *Service {
	return &Service{service: service}
}

func (s *Service) WowDataService() *wowdata.CreatureService {
	if s == nil {
		return nil
	}
	return s.service
}
