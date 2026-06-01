package runtime

type Runtime struct {
	ServiceName string
}

func New() *Runtime {
	return &Runtime{ServiceName: "wowdata-server"}
}
