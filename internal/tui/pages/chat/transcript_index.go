package chat

// heightIndex 只保存 Fenwick 树；原值由调用方元数据或树的单点查询取得。
type heightIndex struct {
	tree []int
}

func (h *heightIndex) reset(values []int) {
	h.tree = make([]int, len(values)+1)
	for i, v := range values {
		j := i + 1
		h.tree[j] += v
		if p := j + (j & -j); p < len(h.tree) {
			h.tree[p] += h.tree[j]
		}
	}
}

func (h *heightIndex) append(v int) {
	i := len(h.tree)
	if i == 0 {
		h.tree = append(h.tree, 0)
		i = 1
	}
	base := h.prefix(i-1) - h.prefix(i-(i&-i))
	h.tree = append(h.tree, base+v)
}

func (h *heightIndex) set(i, v int) {
	h.add(i, v-h.value(i))
}

func (h heightIndex) value(i int) int {
	return h.prefix(i+1) - h.prefix(i)
}

func (h *heightIndex) add(i, delta int) {
	for j := i + 1; j < len(h.tree); j += j & -j {
		h.tree[j] += delta
	}
}

func (h heightIndex) prefix(end int) int {
	total := 0
	for j := min(end, len(h.tree)-1); j > 0; j -= j & -j {
		total += h.tree[j]
	}
	return total
}

// lowerBound 返回包含目标行的槽位，跳过所有零高度槽位。
func (h heightIndex) lowerBound(line int) int {
	i, sum := 0, 0
	step := 1
	for step < len(h.tree) {
		step <<= 1
	}
	for ; step > 0; step >>= 1 {
		next := i + step
		if next < len(h.tree) && sum+h.tree[next] <= line {
			i = next
			sum += h.tree[next]
		}
	}
	return i
}

func (m *Model) transcriptRange(start, end int) (int, int, int) {
	if m.transcript.ready && len(m.transcript.heights.tree) == len(m.TranscriptBlocks)+1 {
		i := m.transcript.heights.lowerBound(start)
		return i, min(len(m.TranscriptBlocks), m.transcript.heights.lowerBound(end-1)+1), m.transcript.heights.prefix(i)
	}
	return 0, len(m.TranscriptBlocks), 0
}

// nextTranscriptSlot 跳过零高度消息，避免大量不渲染消息使窗口扫描退化为全历史扫描。
func (m *Model) nextTranscriptSlot(slot, cursor int) int {
	if m.transcript.ready {
		return max(slot+1, m.transcript.heights.lowerBound(cursor))
	}
	return slot + 1
}
