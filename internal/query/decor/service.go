package decor

import "wowdata/internal/wowdata"

type Service struct {
	service *wowdata.DecorService
}

func NewService(service *wowdata.DecorService) *Service {
	return &Service{service: service}
}
