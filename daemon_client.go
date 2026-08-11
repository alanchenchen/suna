package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strconv"
	"time"

	"github.com/alanchenchen/suna/internal/config"
	"github.com/alanchenchen/suna/internal/protocol"
	"github.com/alanchenchen/suna/internal/transport/local"
)

type daemonProbeError struct {
	DialErr   error
	InvokeErr error
}

func (e daemonProbeError) Error() string {
	if e.InvokeErr != nil {
		return "invoke daemon status: " + e.InvokeErr.Error()
	}
	if e.DialErr != nil {
		return "dial daemon endpoint: " + e.DialErr.Error()
	}
	return "daemon probe failed"
}

func (e daemonProbeError) Unwrap() error {
	if e.InvokeErr != nil {
		return e.InvokeErr
	}
	return e.DialErr
}

func isDaemonDialFailure(err error) bool {
	var probe daemonProbeError
	return errors.As(err, &probe) && probe.DialErr != nil && probe.InvokeErr == nil
}

func queryDaemonStatus(ctx context.Context) (protocol.DaemonStatusParams, error) {
	var status protocol.DaemonStatusParams
	raw, err := invokeLocal(ctx, protocol.MethodDaemonStatus, protocol.DaemonStatusRequest{Detail: false})
	if err != nil {
		return status, err
	}
	if err := json.Unmarshal(raw, &status); err != nil {
		return status, err
	}
	return status, nil
}

func requestDaemonStop(ctx context.Context) error {
	_, err := invokeLocal(ctx, protocol.MethodDaemonStop, nil)
	return err
}

func invokeLocal(ctx context.Context, method string, params any) (json.RawMessage, error) {
	client, err := local.DialDefault(time.Second)
	if err != nil {
		return nil, daemonProbeError{DialErr: err}
	}
	defer client.Close()
	raw, err := client.InvokeRaw(ctx, method, params)
	if err != nil {
		return nil, daemonProbeError{InvokeErr: err}
	}
	return raw, nil
}

func readPID() (int, error) {
	data, err := os.ReadFile(config.DefaultPIDPath())
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(string(data))
}

func removePID() {
	_ = os.Remove(config.DefaultPIDPath())
}
