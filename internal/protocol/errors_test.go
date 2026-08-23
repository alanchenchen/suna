package protocol

import "testing"

func TestRequestErrorConstructorsKeepCodeKindMappings(t *testing.T) {
	tests := []struct {
		name   string
		err    *RequestError
		code   ErrorCode
		kind   ErrorKind
		reason ErrorReason
	}{
		{name: "parse", err: ParseError("parse error"), code: ErrorCodeParse, kind: ErrorKindParse},
		{name: "invalid rpc", err: InvalidRPCRequest("invalid request"), code: ErrorCodeInvalidRequest, kind: ErrorKindInvalidRequest},
		{name: "invalid params", err: InvalidRequestReason("invalid", ErrorReasonInteractionNotFound), code: ErrorCodeInvalidParams, kind: ErrorKindInvalidRequest, reason: ErrorReasonInteractionNotFound},
		{name: "unsupported method", err: UnsupportedMethod("unknown"), code: ErrorCodeMethodNotFound, kind: ErrorKindUnsupportedMethod},
		{name: "handshake", err: HandshakeRequired("hello first"), code: ErrorCodeHandshake, kind: ErrorKindHandshakeRequired},
		{name: "session required", err: SessionRequired("attach first"), code: ErrorCodeInvalidParams, kind: ErrorKindSessionRequired},
		{name: "session busy", err: SessionBusyReason("busy", ErrorReasonInteractionPending), code: ErrorCodeInvalidParams, kind: ErrorKindSessionBusy, reason: ErrorReasonInteractionPending},
		{name: "internal", err: InternalError("failed"), code: ErrorCodeInternal, kind: ErrorKindInternal},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			data, ok := tt.err.Data().(ProtocolErrorData)
			if !ok || tt.err.Code() != int(tt.code) || data.Kind != tt.kind || data.Reason != tt.reason {
				t.Fatalf("error = code %d data %#v, want code %d kind %q reason %q", tt.err.Code(), tt.err.Data(), tt.code, tt.kind, tt.reason)
			}
		})
	}
}

func TestRuntimeUnavailableCarriesStateAndRetryability(t *testing.T) {
	starting := RuntimeUnavailable(ErrorReasonRuntimeStarting, true)
	data := starting.Data().(ProtocolErrorData)
	if starting.Code() != int(ErrorCodeInternal) || data.Kind != ErrorKindRuntimeUnavailable || data.Reason != ErrorReasonRuntimeStarting || !data.Retryable {
		t.Fatalf("starting error = code %d data %#v", starting.Code(), data)
	}
}
