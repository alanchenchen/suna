package memory

import "strings"

const (
	toolSummaryRecentLimit   = 4
	toolSummaryFailuresLimit = 2
	toolSummaryTextMaxRunes  = 180
)

type ToolSummary struct {
	Total    int               `json:"total"`
	Success  int               `json:"success"`
	Failed   int               `json:"failed"`
	Changes  []ToolChangeItem  `json:"changes,omitempty"`
	Failures []ToolSummaryItem `json:"failures,omitempty"`
	Recent   []ToolSummaryItem `json:"recent,omitempty"`
	Omitted  int               `json:"omitted,omitempty"`
}

type ToolChangeItem struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

type ToolSummaryItem struct {
	Name    string `json:"name"`
	Status  string `json:"status"`
	Summary string `json:"summary"`
}

func (s ToolSummary) Empty() bool {
	return s.Total <= 0 && s.Success <= 0 && s.Failed <= 0 && len(s.Recent) == 0 && len(s.Failures) == 0 && len(s.Changes) == 0
}

func (s ToolSummary) Normalize() ToolSummary {
	if s.Total < 0 {
		s.Total = 0
	}
	if s.Success < 0 {
		s.Success = 0
	}
	if s.Failed < 0 {
		s.Failed = 0
	}
	if s.Total == 0 && (s.Success > 0 || s.Failed > 0) {
		s.Total = s.Success + s.Failed
	}
	if s.Success+s.Failed > s.Total {
		s.Total = s.Success + s.Failed
	}
	s.Recent = normalizeToolSummaryItems(s.Recent, toolSummaryRecentLimit)
	s.Failures = normalizeToolSummaryItems(s.Failures, toolSummaryFailuresLimit)
	s.Changes = normalizeToolChanges(s.Changes)
	omitted := s.Total - len(s.Recent)
	if omitted < 0 {
		omitted = 0
	}
	s.Omitted = omitted
	return s
}

func (s ToolSummary) Add(item ToolSummaryItem) ToolSummary {
	item = normalizeToolSummaryItem(item)
	if item.Name == "" {
		return s.Normalize()
	}
	s.Total++
	if isToolSummaryFailure(item.Status) {
		s.Failed++
		s.Failures = appendBoundedToolItems(s.Failures, item, toolSummaryFailuresLimit)
	} else {
		s.Success++
	}
	if name := canonicalToolSummaryName(item.Name); isToolSummaryChangeTool(name) {
		s.Changes = incrementToolChange(s.Changes, name)
	}
	s.Recent = appendBoundedToolItems(s.Recent, item, toolSummaryRecentLimit)
	return s.Normalize()
}

func BuildToolSummary(items []ToolSummaryItem) ToolSummary {
	var out ToolSummary
	for _, item := range items {
		out = out.Add(item)
	}
	return out.Normalize()
}

func normalizeToolSummaryItems(items []ToolSummaryItem, limit int) []ToolSummaryItem {
	if len(items) == 0 || limit <= 0 {
		return nil
	}
	start := len(items) - limit
	if start < 0 {
		start = 0
	}
	out := make([]ToolSummaryItem, 0, len(items)-start)
	for _, item := range items[start:] {
		item = normalizeToolSummaryItem(item)
		if item.Name == "" {
			continue
		}
		out = append(out, item)
	}
	return out
}

func normalizeToolSummaryItem(item ToolSummaryItem) ToolSummaryItem {
	item.Name = canonicalToolSummaryName(item.Name)
	item.Status = strings.TrimSpace(item.Status)
	if item.Status == "" {
		item.Status = "success"
	}
	item.Summary = strings.TrimSpace(item.Summary)
	if len([]rune(item.Summary)) > toolSummaryTextMaxRunes {
		item.Summary = truncateRunes(item.Summary, toolSummaryTextMaxRunes)
	}
	return item
}

func normalizeToolChanges(items []ToolChangeItem) []ToolChangeItem {
	if len(items) == 0 {
		return nil
	}
	out := make([]ToolChangeItem, 0, len(items))
	for _, item := range items {
		name := canonicalToolSummaryName(item.Name)
		if name == "" || item.Count <= 0 || !isToolSummaryChangeTool(name) {
			continue
		}
		out = incrementToolChangeBy(out, name, item.Count)
	}
	return out
}

func appendBoundedToolItems(items []ToolSummaryItem, item ToolSummaryItem, limit int) []ToolSummaryItem {
	if limit <= 0 {
		return nil
	}
	items = append(items, item)
	if len(items) <= limit {
		return items
	}
	cut := len(items) - limit
	for i := 0; i < cut; i++ {
		items[i] = ToolSummaryItem{}
	}
	return append([]ToolSummaryItem(nil), items[cut:]...)
}

func incrementToolChange(items []ToolChangeItem, name string) []ToolChangeItem {
	return incrementToolChangeBy(items, name, 1)
}

func incrementToolChangeBy(items []ToolChangeItem, name string, n int) []ToolChangeItem {
	if n <= 0 {
		return items
	}
	for i := range items {
		if items[i].Name == name {
			items[i].Count += n
			return items
		}
	}
	return append(items, ToolChangeItem{Name: name, Count: n})
}

func isToolSummaryFailure(status string) bool {
	status = strings.ToLower(strings.TrimSpace(status))
	return strings.Contains(status, "error") || strings.Contains(status, "fail")
}

func canonicalToolSummaryName(name string) string {
	name = strings.TrimSpace(name)
	if i := strings.LastIndex(name, "."); i >= 0 && i < len(name)-1 {
		name = name[i+1:]
	}
	return name
}

func isToolSummaryChangeTool(name string) bool {
	switch strings.ToLower(name) {
	case "editfile", "writefile", "filesystem":
		return true
	default:
		return false
	}
}
