package coze

import "github.com/Elysia-SHY/one-api-plus/relay/adaptor/coze/constant/event"

func event2StopReason(e *string) string {
	if e == nil || *e == event.Message {
		return ""
	}
	return "stop"
}
