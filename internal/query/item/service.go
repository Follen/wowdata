package item

import "wowdata/internal/wowdata"

type Service struct {
	service *wowdata.ItemService
}

func NewService(service *wowdata.ItemService) *Service {
	return &Service{service: service}
}

func (s *Service) WowDataService() *wowdata.ItemService {
	if s == nil {
		return nil
	}
	return s.service
}
