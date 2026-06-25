package model

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"

	anthropic "github.com/anthropics/anthropic-sdk-go"
	openai "github.com/openai/openai-go/v3"
)

type ModelErrorKind string

const (
	ModelErrorUnknown   ModelErrorKind = "unknown"
	ModelErrorHTTP      ModelErrorKind = "http"
	ModelErrorNetwork   ModelErrorKind = "network"
	ModelErrorCancelled ModelErrorKind = "cancelled"
	ModelErrorInternal  ModelErrorKind = "internal"
)

type ModelError struct {
	Kind       ModelErrorKind
	Message    string
	StatusCode int
	Code       string
	Type       string
	Provider   string
	Model      string
}

func (e *ModelError) Error() string {
	if e == nil {
		return ""
	}
	if e.Message != "" {
		return e.Message
	}
	if e.StatusCode > 0 {
		return fmt.Sprintf("HTTP %d %s", e.StatusCode, http.StatusText(e.StatusCode))
	}
	return string(e.Kind)
}

func (e *ModelError) WithRoute(provider, model string) *ModelError {
	if e == nil {
		return nil
	}
	cp := *e
	if cp.Provider == "" {
		cp.Provider = provider
	}
	if cp.Model == "" {
		cp.Model = model
	}
	return &cp
}

func NewModelError(err error) *ModelError {
	return modelErrorFromProvider(err, "", "")
}

func modelErrorFromProvider(err error, provider, modelName string) *ModelError {
	if err == nil {
		return nil
	}
	var me *ModelError
	if errors.As(err, &me) {
		return me.WithRoute(provider, modelName)
	}
	if errors.Is(err, context.Canceled) {
		return &ModelError{Kind: ModelErrorCancelled, Message: "cancelled", Provider: provider, Model: modelName}
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return &ModelError{Kind: ModelErrorNetwork, Message: err.Error(), Provider: provider, Model: modelName}
	}
	var openaiErr *openai.Error
	if errors.As(err, &openaiErr) {
		return &ModelError{Kind: ModelErrorHTTP, Message: firstNonEmpty(openaiErr.Message, http.StatusText(openaiErr.StatusCode), err.Error()), StatusCode: openaiErr.StatusCode, Code: openaiErr.Code, Type: openaiErr.Type, Provider: provider, Model: modelName}
	}
	var anthropicErr *anthropic.Error
	if errors.As(err, &anthropicErr) {
		return &ModelError{Kind: ModelErrorHTTP, Message: firstNonEmpty(http.StatusText(anthropicErr.StatusCode), err.Error()), StatusCode: anthropicErr.StatusCode, Type: string(anthropicErr.Type()), Provider: provider, Model: modelName}
	}
	if isNetworkError(err) {
		return &ModelError{Kind: ModelErrorNetwork, Message: err.Error(), Provider: provider, Model: modelName}
	}
	return &ModelError{Kind: ModelErrorUnknown, Message: err.Error(), Provider: provider, Model: modelName}
}

func isNetworkError(err error) bool {
	var netErr net.Error
	if errors.As(err, &netErr) {
		return true
	}
	var opErr *net.OpError
	return errors.As(err, &opErr)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
