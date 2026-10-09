package chat

import "github.com/alanchenchen/suna/internal/tui/components/toolview"

// markToolDirty 与 FindTool 解析同一所有者：优先当前块，其次为重复编号的首个历史所有者。
func (m *Model) markToolDirty(id string) {
	if _, block := m.findToolOwner(id); block != nil {
		m.markBlockDirty(block)
		return
	}
	if _, i := m.findSkillLoadOwner(id); i >= 0 {
		m.MarkMessageDirty(i)
		return
	}
	if !m.transcript.ready {
		m.displayBudget = displayBudget{}
	}
}

func (m *Model) indexTranscriptMessage(i int) {
	msg := &m.Messages[i]
	previous := m.transcript.owners[i]
	block, _ := msg.Content.(*toolview.Block)
	skill := ""
	if view, ok := msg.Content.(*SkillLoadView); ok && view != nil {
		skill = view.ID
	}
	id := uint64(0)
	if msg.Role == "reasoning" || msg.Role == "tool" {
		id = msg.ID
	}
	owner := transcriptOwner{block: block, skill: skill, id: id}
	if owner != (transcriptOwner{}) {
		if m.transcript.owners == nil {
			m.transcript.owners = make(map[int]transcriptOwner)
		}
		m.transcript.owners[i] = owner
	} else {
		delete(m.transcript.owners, i)
	}
	replaced := (previous.block != nil && previous.block != block) || (previous.skill != "" && previous.skill != skill) || (previous.id != 0 && previous.id != id)
	if replaced {
		// 替换或删除很少发生；重建索引，避免删除仍属于另一条历史消息的重复编号。
		m.rebuildTranscriptOwners()
		return
	}
	m.addTranscriptOwner(i)
}

func (m *Model) addTranscriptOwner(i int) {
	source := m.transcript.owners[i]
	if source.block != nil {
		if old, ok := m.transcript.blocks[source.block]; !ok || i < old {
			m.transcript.blocks[source.block] = i
		}
		for id, entry := range source.block.Entries {
			if entry == nil {
				continue
			}
			if old, ok := m.transcript.tools[id]; !ok || i < old {
				m.transcript.tools[id] = i
			}
		}
	}
	if source.skill != "" {
		// 技能查找始终优先最近的匹配视图。
		if old, ok := m.transcript.skills[source.skill]; !ok || i > old {
			m.transcript.skills[source.skill] = i
		}
	}
	if source.id != 0 {
		m.transcript.ids[source.id] = i
	}
}

func (m *Model) rebuildTranscriptOwners() {
	m.transcript.blocks = make(map[*toolview.Block]int)
	m.transcript.tools = make(map[string]int)
	m.transcript.skills = make(map[string]int)
	m.transcript.ids = make(map[uint64]int)
	for i := range m.transcript.owners {
		// 就地删除条目后应从当前消息读取，不能使用旧所有权副本。
		msg := &m.Messages[i]
		owner := transcriptOwner{}
		owner.block, _ = msg.Content.(*toolview.Block)
		if view, ok := msg.Content.(*SkillLoadView); ok && view != nil {
			owner.skill = view.ID
		}
		if msg.Role == "reasoning" || msg.Role == "tool" {
			owner.id = msg.ID
		}
		m.transcript.owners[i] = owner
		m.addTranscriptOwner(i)
	}
}

func (m *Model) findToolOwner(id string) (*toolview.Entry, *toolview.Block) {
	if id == "" {
		return nil, nil
	}
	if block := m.CurrentToolBlock; block != nil {
		if entry := block.Entries[id]; entry != nil {
			return entry, block
		}
	}
	if m.transcript.ready {
		if i, ok := m.transcript.tools[id]; ok && i < len(m.Messages) {
			if block, ok := m.Messages[i].Content.(*toolview.Block); ok && block != nil {
				if entry := block.Entries[id]; entry != nil {
					return entry, block
				}
			}
			// 条目被就地删除，不属于常规更新路径。
			m.rebuildTranscriptOwners()
			if i, ok := m.transcript.tools[id]; ok {
				block, _ := m.Messages[i].Content.(*toolview.Block)
				return block.Entries[id], block
			}
		}
		for i := len(m.transcript.sources); i < len(m.Messages); i++ {
			if block, ok := m.Messages[i].Content.(*toolview.Block); ok && block != nil {
				if entry := block.Entries[id]; entry != nil {
					return entry, block
				}
			}
		}
		return nil, nil
	}
	for i := range m.Messages {
		if block, ok := m.Messages[i].Content.(*toolview.Block); ok && block != nil {
			if entry := block.Entries[id]; entry != nil {
				return entry, block
			}
		}
	}
	return nil, nil
}

func (m *Model) findSkillLoadOwner(id string) (*SkillLoadView, int) {
	first := 0
	if m.transcript.ready {
		first = len(m.transcript.sources)
	}
	for i := len(m.Messages) - 1; i >= first; i-- {
		if view, ok := m.Messages[i].Content.(*SkillLoadView); ok && view != nil && view.ID == id {
			return view, i
		}
	}
	if m.transcript.ready {
		if i, ok := m.transcript.skills[id]; ok && i < len(m.Messages) {
			if view, ok := m.Messages[i].Content.(*SkillLoadView); ok && view != nil && view.ID == id {
				return view, i
			}
		}
	}
	return nil, -1
}

// 当前块关闭后，ActiveTools 可能指向较晚的重复条目；标记实际修改的条目，而非编号的首个所有者。
func (m *Model) markToolEntryDirty(entry *toolview.Entry) {
	if entry == nil {
		return
	}
	if _, block := m.findToolOwner(entry.ID); block != nil && block.Entries[entry.ID] == entry {
		m.markBlockDirty(block)
		return
	}
	matches := func(i int) bool {
		if i < 0 || i >= len(m.Messages) {
			return false
		}
		block, ok := m.Messages[i].Content.(*toolview.Block)
		if ok && block != nil && block.Entries[entry.ID] == entry {
			m.MarkMessageDirty(i)
			return true
		}
		return false
	}
	if m.transcript.ready {
		for i := range m.transcript.dynamic {
			if matches(i) {
				return
			}
		}
		for i := range m.transcript.dirty {
			if matches(i) {
				return
			}
		}
		for i := len(m.transcript.sources); i < len(m.Messages); i++ {
			if matches(i) {
				return
			}
		}
	}
	// 仅未索引的新建消息或已关闭且非动态的重复条目需要回退扫描；普通工具事件使用所有权和动态索引。
	for i := range m.Messages {
		if matches(i) {
			return
		}
	}
	m.markToolDirty(entry.ID)
}
