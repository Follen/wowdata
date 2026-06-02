package item

import "wowdata/internal/shared/wowdata"

type Service struct {
	service *wowdata.ItemService
}

func NewService(service *wowdata.ItemService) *Service {
	return &Service{service: service}
}
