package casc

type ContentFlag uint32

const (
	ContentLowViolence ContentFlag = 0x80
	ContentNoNameHash  ContentFlag = 0x10000000
	ContentEncrypted   ContentFlag = 0x08000000
)
