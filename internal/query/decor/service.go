package decor

import "wowdata/internal/shared/wowdata"

type Service struct {
	service *wowdata.DecorService
}

func NewService(service *wowdata.DecorService) *Service {
	return &Service{service: service}
}
