package item

import "wowdata/internal/wowdata"

type Service struct {
	service *wowdata.ItemService
}

func NewService(service *wowdata.ItemService) *Service {
	return &Service{service: service}
}
