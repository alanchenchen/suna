package chat

import tea "charm.land/bubbletea/v2"

// ModelPickerRow 是 /model 浮层的展示项；Ref 是稳定业务标识。
type ModelPickerRow struct {
	Ref     string
	Summary string
	Mark    string
}

// OpenModelPicker 使用原生 Bubbles list 打开真正的浮层，而不是将列表写入 transcript。
func (m *Model) OpenModelPicker(rows []ModelPickerRow, activeRef string) {
	m.ModelPickerOpen = true
	m.ModelList.Reset()
	m.ModelList.SetItems(modelItems(rows))
	m.ModelList.SelectKey(activeRef)
}

// SetModelPickerModels 刷新浮层模型列表，保留当前过滤输入状态。
// 异步拉取结果到达时使用：用户可能已在浮层内输入过滤文本，
// Reset 会清掉输入，SetItems 则保留 Filtering 状态。
func (m *Model) SetModelPickerModels(rows []ModelPickerRow) {
	m.ModelList.SetItems(modelItems(rows))
}

func (m *Model) CloseModelPicker() { m.ModelPickerOpen = false }

func (m *Model) UpdateModelPicker(msg tea.Msg) tea.Cmd { return m.UpdateModelList(msg) }

func (m Model) ModelPickerFiltering() bool { return m.ModelList.Filtering() }

func (m Model) SelectedModelRef() (string, bool) {
	item, ok := m.ModelList.Selected()
	if !ok {
		return "", false
	}
	row, ok := item.(modelItem)
	if !ok || row.row.Ref == "" {
		return "", false
	}
	return row.row.Ref, true
}
