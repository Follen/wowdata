package file

import appruntime "wowdata/internal/local/runtime"

type Service struct {
	store appruntime.FileStore
}

func NewService(store appruntime.FileStore) *Service {
	return &Service{store: store}
}
