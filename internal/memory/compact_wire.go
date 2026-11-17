package memory

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

type toolGroupWire struct {
	ID       int       `json:"id"`
	Messages []Message `json:"messages"`
}

func compactToolGroups(groups []ToolGroup) []toolGroupWire {
	out := make([]toolGroupWire, len(groups))
	for i, group := range groups {
		out[i] = toolGroupWire{ID: i, Messages: compactSources(group.Messages)}
	}
	return out
}
func restoreToolGroupIndexes(groups []ToolGroup, observations []ToolObservation) []ToolObservation {
	out := make([]ToolObservation, len(observations))
	for i, v := range observations {
		if v.ID >= 0 && v.ID < len(groups) {
			v.ID = groups[v.ID].Index
		} else {
			v.ID = -1
		}
		out[i] = v
	}
	return out
}

func compactPayload(plan *CompactPlan) ([]byte, error) {
	return json.Marshal(struct {
		Summary    string          `json:"previous_summary,omitempty"`
		History    []Message       `json:"history,omitempty"`
		Groups     []toolGroupWire `json:"groups,omitempty"`
		Background []Message       `json:"readonly_background,omitempty"`
	}{plan.Summary, compactSources(plan.Prefix), compactToolGroups(plan.Groups), compactSources(plan.Background)})
}

// decodeCompactOutput 允许完整围栏和无关字段，但不接受截断 JSON、尾随正文或第二个对象。
// 逐区域解码使一条错误观察不会废弃其余付费成果；不明确的编号一律保留原组。
func decodeCompactOutput(raw string, plan *CompactPlan) (CompactOutput, error) {
	var out CompactOutput
	raw = strings.TrimSpace(raw)
	if strings.HasPrefix(raw, "```") && strings.HasSuffix(raw, "```") {
		if newline := strings.IndexByte(raw, '\n'); newline >= 0 {
			raw = strings.TrimSpace(raw[newline+1 : len(raw)-3])
		}
	}
	var fields map[string]json.RawMessage
	decoder := json.NewDecoder(strings.NewReader(raw))
	if err := decoder.Decode(&fields); err != nil || fields == nil {
		return out, fmt.Errorf("decode compression output: invalid JSON object")
	}
	if decoder.Decode(new(any)) != io.EOF {
		return out, fmt.Errorf("compression returned extra content")
	}
	_ = json.Unmarshal(fields["summary"], &out.Summary)
	var items []json.RawMessage
	_ = json.Unmarshal(fields["observations"], &items)
	for _, raw := range items {
		var fields map[string]json.RawMessage
		if json.Unmarshal(raw, &fields) != nil {
			continue
		}
		var id *int
		if json.Unmarshal(fields["id"], &id) != nil || id == nil {
			continue
		}
		v := ToolObservation{ID: *id}
		if data, ok := fields["keep"]; ok {
			var keep *bool
			if json.Unmarshal(data, &keep) != nil || keep == nil {
				v.Keep = true
			} else {
				v.Keep = *keep
			}
		}
		if json.Unmarshal(fields["text"], &v.Text) != nil {
			v.Keep = true
		}
		out.Observations = append(out.Observations, v)
	}
	out.Observations = restoreToolGroupIndexes(plan.Groups, out.Observations)
	out = normalizeToolObservations(plan.Groups, out)
	valid := len(plan.Prefix) > 0 && strings.TrimSpace(out.Summary) != ""
	for _, v := range out.Observations {
		valid = valid || !v.Keep
	}
	if !valid && !(len(plan.Prefix) == 0 && len(plan.Groups) > 0 && len(items) > 0) {
		return CompactOutput{}, fmt.Errorf("compression returned no usable summary")
	}
	return out, nil
}
