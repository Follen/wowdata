package icon

import appruntime "wowdata/internal/runtime"

type Service struct {
	store appruntime.IconStore
}

func NewService(store appruntime.IconStore) *Service {
	return &Service{store: store}
}

func (s *Service) Store() appruntime.IconStore {
	if s == nil {
		return nil
	}
	return s.store
}
