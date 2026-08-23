package protocol

import "fmt"

// ErrorCode 是公开 JSON-RPC response error 的稳定数字分类。
type ErrorCode int

const (
	ErrorCodeParse          ErrorCode = -32700
	ErrorCodeInvalidRequest ErrorCode = -32600
	ErrorCodeMethodNotFound ErrorCode = -32601
	ErrorCodeInvalidParams  ErrorCode = -32602
	ErrorCodeInternal       ErrorCode = -32603
	ErrorCodeHandshake      ErrorCode = -32010
)

// ErrorKind 是客户端进行恢复分支时应依赖的稳定错误大类。
type ErrorKind string

const (
	ErrorKindParse                 ErrorKind = "parse_error"
	ErrorKindInvalidRequest        ErrorKind = "invalid_request"
	ErrorKindUnsupportedMethod     ErrorKind = "unsupported_method"
	ErrorKindUnsupportedCapability ErrorKind = "unsupported_capability"
	ErrorKindHandshakeRequired     ErrorKind = "handshake_required"
	ErrorKindRuntimeUnavailable    ErrorKind = "runtime_unavailable"
	ErrorKindSessionRequired       ErrorKind = "session_required"
	ErrorKindSessionBusy           ErrorKind = "session_busy"
	ErrorKindInternal              ErrorKind = "internal_error"
)

// ErrorReason 仅包含客户端需要采取不同恢复动作的稳定细分原因。
type ErrorReason string

const (
	ErrorReasonRuntimeStarting     ErrorReason = "starting"
	ErrorReasonRuntimeStopping     ErrorReason = "stopping"
	ErrorReasonInteractionNotFound ErrorReason = "interaction_not_found"
	ErrorReasonInteractionPending  ErrorReason = "interaction_pending"
	ErrorReasonRunNotSteerable     ErrorReason = "run_not_steerable"
	ErrorReasonSteeringNotFound    ErrorReason = "steering_not_found"
	ErrorReasonSteeringQueueFull   ErrorReason = "steering_queue_full"
	ErrorReasonClientMsgConflict   ErrorReason = "client_msg_conflict"
)

// ProtocolErrorData 是 RequestError 在线协议中的机器可读信息。
type ProtocolErrorData struct {
	Kind       ErrorKind   `json:"kind"`
	Reason     ErrorReason `json:"reason,omitempty"`
	Retryable  bool        `json:"retryable,omitempty"`
	StatusCode int         `json:"status_code,omitempty"`
}

// ErrorDataForCode 为尚未迁移的内部 coded error 提供统一默认 kind；新公开错误应使用语义构造函数。
func ErrorDataForCode(code int) ProtocolErrorData {
	switch ErrorCode(code) {
	case ErrorCodeParse:
		return ProtocolErrorData{Kind: ErrorKindParse}
	case ErrorCodeMethodNotFound:
		return ProtocolErrorData{Kind: ErrorKindUnsupportedMethod}
	case ErrorCodeInvalidRequest, ErrorCodeInvalidParams:
		return ProtocolErrorData{Kind: ErrorKindInvalidRequest}
	case ErrorCodeHandshake:
		return ProtocolErrorData{Kind: ErrorKindHandshakeRequired}
	default:
		return ProtocolErrorData{Kind: ErrorKindInternal}
	}
}

// RequestError 是 daemon/transport 对客户端返回的统一同步请求错误。
type RequestError struct {
	code    ErrorCode
	message string
	data    ProtocolErrorData
}

func (e *RequestError) Error() string {
	if e == nil {
		return ""
	}
	return e.message
}
func (e *RequestError) Code() int { return int(e.code) }
func (e *RequestError) Data() any { return e.data }

func newRequestError(code ErrorCode, kind ErrorKind, message string, reason ErrorReason) *RequestError {
	return &RequestError{code: code, message: message, data: ProtocolErrorData{Kind: kind, Reason: reason}}
}

func ParseError(message string) *RequestError {
	return newRequestError(ErrorCodeParse, ErrorKindParse, message, "")
}
func InvalidRPCRequest(message string) *RequestError {
	return newRequestError(ErrorCodeInvalidRequest, ErrorKindInvalidRequest, message, "")
}
func InvalidRequest(message string) *RequestError {
	return newRequestError(ErrorCodeInvalidParams, ErrorKindInvalidRequest, message, "")
}
func InvalidRequestReason(message string, reason ErrorReason) *RequestError {
	return newRequestError(ErrorCodeInvalidParams, ErrorKindInvalidRequest, message, reason)
}
func UnsupportedMethod(method string) *RequestError {
	return UnsupportedMethodMessage(fmt.Sprintf("method not found: %s", method))
}
func UnsupportedMethodMessage(message string) *RequestError {
	return newRequestError(ErrorCodeMethodNotFound, ErrorKindUnsupportedMethod, message, "")
}
func HandshakeRequired(message string) *RequestError {
	return newRequestError(ErrorCodeHandshake, ErrorKindHandshakeRequired, message, "")
}
func RuntimeUnavailable(reason ErrorReason, retryable bool) *RequestError {
	err := newRequestError(ErrorCodeInternal, ErrorKindRuntimeUnavailable, "runtime is "+string(reason), reason)
	err.data.Retryable = retryable
	return err
}
func SessionRequired(message string) *RequestError {
	return newRequestError(ErrorCodeInvalidParams, ErrorKindSessionRequired, message, "")
}
func SessionBusy(message string) *RequestError {
	return newRequestError(ErrorCodeInvalidParams, ErrorKindSessionBusy, message, "")
}
func SessionBusyReason(message string, reason ErrorReason) *RequestError {
	return newRequestError(ErrorCodeInvalidParams, ErrorKindSessionBusy, message, reason)
}
func InternalError(message string) *RequestError {
	return newRequestError(ErrorCodeInternal, ErrorKindInternal, message, "")
}
