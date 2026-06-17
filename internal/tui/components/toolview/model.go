package toolview

import "time"

// Status 描述工具调用在 TUI transcript 中的展示状态。
type Status int

const (
	StatusRunning Status = iota
	StatusDone
	StatusError
)

// GuardInfo 是工具调用的安全审核展示数据。
type GuardInfo struct {
	Risk          string
	Decision      string
	Source        string
	Reason        string
	Suggestion    string
	ReviewCode    string
	ReviewMessage string
}

// Entry 是 Chat transcript 中单个工具调用的展示模型。
type Entry struct {
	ID              string
	LocalID         string
	ParentID        string
	Name            string
	RawName         string
	Intent          string
	Params          string
	ParamsRaw       map[string]any
	Summary         string
	Status          Status
	StartedAt       time.Time
	EndedAt         time.Time
	Duration        time.Duration
	Result          string
	ResultTruncated bool
	ResultBytes     int
	Metadata        map[string]any
	Guard           *GuardInfo
}

// Block 聚合同一段连续工具调用。Order 保留 daemon 事件顺序，Entries 用于按 ID 更新状态。
type Block struct {
	Entries map[string]*Entry
	Order   []string
}

func (b *Block) Add(entry *Entry) {
	if b == nil || entry == nil {
		return
	}
	if b.Entries == nil {
		b.Entries = make(map[string]*Entry)
	}
	if _, exists := b.Entries[entry.ID]; !exists {
		b.Order = append(b.Order, entry.ID)
	}
	b.Entries[entry.ID] = entry
}
