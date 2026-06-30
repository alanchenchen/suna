package toolview

import (
	"fmt"
	"strings"
)

func ParseSubtaskID(id string) (string, string) {
	const prefix = "spawn:"
	if !strings.HasPrefix(id, prefix) {
		return "", id
	}
	rest := strings.TrimPrefix(id, prefix)
	parts := strings.SplitN(rest, ":", 2)
	if len(parts) != 2 {
		return "", id
	}
	return parts[0], parts[1]
}

func VisibleMainEntries(block *Block) []*Entry {
	if block == nil {
		return nil
	}
	entries := make([]*Entry, 0, len(block.Order))
	for _, id := range block.Order {
		te := block.Entries[id]
		if te == nil || IsSubtask(te) || hasValidSubtaskParent(block, te) {
			continue
		}
		entries = append(entries, te)
	}
	return entries
}

func VisibleEntries(block *Block) []*Entry {
	if block == nil {
		return nil
	}
	var entries []*Entry
	for _, id := range block.Order {
		te := block.Entries[id]
		if te == nil || te.ParentID != "" {
			continue
		}
		entries = append(entries, te)
		for _, childID := range block.Order {
			child := block.Entries[childID]
			if child == nil || child.ParentID != te.ID || !HasSubtaskParent(block, child.ParentID) {
				continue
			}
			entries = append(entries, child)
		}
	}
	for _, id := range block.Order {
		te := block.Entries[id]
		if te == nil || te.ParentID == "" || HasSubtaskParent(block, te.ParentID) {
			continue
		}
		entries = append(entries, te)
	}
	return entries
}

func ChangedFilePath(te *Entry) string {
	if !HasFileChange(te) {
		return ""
	}
	path, _ := te.Metadata["path"].(string)
	return strings.TrimSpace(path)
}

func ChangedFSPath(te *Entry) string {
	if !HasFSChange(te) {
		return ""
	}
	path, _ := te.Metadata["path"].(string)
	return strings.TrimSpace(path)
}

func HasFileChange(te *Entry) bool {
	if te == nil || te.Metadata == nil {
		return false
	}
	kind, _ := te.Metadata["kind"].(string)
	return kind == "file_change"
}

func HasFSChange(te *Entry) bool {
	if te == nil || te.Metadata == nil {
		return false
	}
	kind, _ := te.Metadata["kind"].(string)
	return kind == "fs_change"
}

func ShouldShowGuardSummary(info *GuardInfo) bool {
	if info == nil {
		return false
	}
	if strings.EqualFold(strings.TrimSpace(info.Risk), "low") && strings.EqualFold(strings.TrimSpace(info.Decision), "approve") {
		return false
	}
	return true
}

func IsSubtask(te *Entry) bool {
	return te != nil && te.RawName == "spawn" && te.ParentID == ""
}

func IsSubtaskChild(te *Entry) bool {
	return te != nil && te.ParentID != ""
}

func HasSubtaskParent(block *Block, parentID string) bool {
	if block == nil || parentID == "" || block.Entries == nil {
		return false
	}
	return IsSubtask(block.Entries[parentID])
}

func hasValidSubtaskParent(block *Block, te *Entry) bool {
	return te != nil && te.ParentID != "" && HasSubtaskParent(block, te.ParentID)
}

// SubtaskChildren 返回某个子任务的内部工具调用，顺序与工具块事件顺序一致。
func SubtaskChildren(block *Block, parentID string) []*Entry {
	if block == nil || parentID == "" {
		return nil
	}
	entries := make([]*Entry, 0)
	for _, childID := range block.Order {
		child := block.Entries[childID]
		if child == nil || child.ParentID != parentID || !HasSubtaskParent(block, parentID) {
			continue
		}
		entries = append(entries, child)
	}
	return entries
}

func BlockTitle(entries []*Entry, labels RenderLabels) string {
	parts := []string{labels.Tools}
	if len(entries) > 0 {
		parts = append(parts, countLabel(len(entries), labels.Actions))
	}
	changedFiles := make(map[string]struct{})
	fsChanges := 0
	guards := 0
	for _, te := range entries {
		if path := ChangedFilePath(te); path != "" {
			changedFiles[path] = struct{}{}
		}
		if path := ChangedFSPath(te); path != "" {
			fsChanges++
		}
		if ShouldShowGuardSummary(te.Guard) {
			guards++
		}
	}
	if len(changedFiles) > 0 {
		parts = append(parts, countLabel(len(changedFiles), labels.FilesChanged))
	}
	if fsChanges > 0 {
		parts = append(parts, countLabel(fsChanges, labels.FSChanges))
	}
	if guards > 0 {
		parts = append(parts, countLabel(guards, labels.Guarded))
	}
	return strings.Join(parts, " · ")
}

func countLabel(n int, label string) string {
	label = strings.TrimSpace(label)
	if label == "" {
		return fmt.Sprintf("%d", n)
	}
	if strings.HasPrefix(label, "个") {
		return fmt.Sprintf("%d%s", n, label)
	}
	return fmt.Sprintf("%d %s", n, label)
}
