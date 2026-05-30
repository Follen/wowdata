package casc

type ContentFlag uint32

const (
	ContentLowViolence  ContentFlag = 1 << 0
	ContentNoNameHash   ContentFlag = 1 << 1
	ContentEncrypted    ContentFlag = 1 << 2
)
