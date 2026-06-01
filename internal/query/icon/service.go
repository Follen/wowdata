package icon

import appruntime "wowdata/internal/runtime"

type Service struct {
	store appruntime.IconStore
}

func NewService(store appruntime.IconStore) *Service {
	return &Service{store: store}
}
