package model

import "fmt"

// BindingErrorKind 描述创建模型绑定时可由调用方恢复的配置状态。
// 它不表示一次模型请求的上游错误，不能与 ModelError 混用。
type BindingErrorKind string

const (
	BindingErrorRouterUnavailable BindingErrorKind = "router_unavailable"
	BindingErrorModelNotFound     BindingErrorKind = "model_not_found"
)

// BindingError 保留模型引用，供上层转换成稳定的运行或协议错误。
type BindingError struct {
	Kind BindingErrorKind
	Ref  string
}

func (e *BindingError) Error() string {
	if e == nil {
		return ""
	}
	switch e.Kind {
	case BindingErrorRouterUnavailable:
		return "model router is not configured"
	case BindingErrorModelNotFound:
		return fmt.Sprintf("model %q not found", e.Ref)
	default:
		return "model binding failed"
	}
}
