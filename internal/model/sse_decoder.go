package model

import (
	"bufio"
	"bytes"
	"io"
	"strings"

	"github.com/openai/openai-go/v3/packages/ssestream"
)

func init() {
	ssestream.RegisterDecoder("text/event-stream", newCompatibleSSEDecoder)
}

type compatibleSSEDecoder struct {
	evt ssestream.Event
	rc  io.ReadCloser
	scn *bufio.Scanner
	err error
}

func newCompatibleSSEDecoder(rc io.ReadCloser) ssestream.Decoder {
	scn := bufio.NewScanner(rc)
	scn.Buffer(nil, bufio.MaxScanTokenSize<<9)
	return &compatibleSSEDecoder{rc: rc, scn: scn}
}

func (d *compatibleSSEDecoder) Next() bool {
	if d.err != nil {
		return false
	}

	event := ""
	data := bytes.NewBuffer(nil)
	for d.scn.Scan() {
		line := d.scn.Bytes()
		if len(line) == 0 {
			// 兼容部分中转发送的 heartbeat/comment-only SSE 事件；这些事件没有 JSON payload，
			// openai-go 默认 decoder 会继续交给 json.Unmarshal 并报 unexpected end of JSON input。
			if strings.TrimSpace(data.String()) == "" {
				event = ""
				data.Reset()
				continue
			}
			d.evt = ssestream.Event{Type: event, Data: data.Bytes()}
			return true
		}

		name, value, _ := bytes.Cut(line, []byte(":"))
		if len(value) > 0 && value[0] == ' ' {
			value = value[1:]
		}

		switch string(name) {
		case "":
			continue
		case "event":
			event = string(value)
		case "data":
			_, d.err = data.Write(value)
			if d.err != nil {
				return false
			}
			_, d.err = data.WriteRune('\n')
			if d.err != nil {
				return false
			}
		}
	}

	if d.scn.Err() != nil {
		d.err = d.scn.Err()
	}
	return false
}

func (d *compatibleSSEDecoder) Event() ssestream.Event { return d.evt }

func (d *compatibleSSEDecoder) Close() error { return d.rc.Close() }

func (d *compatibleSSEDecoder) Err() error { return d.err }
