package runner

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/alanchenchen/suna/internal/memory"
	"github.com/alanchenchen/suna/internal/model"
	"github.com/google/uuid"
)

// 水位、增长冷却与实际收益各自独立；相同数值不代表同一项策略。
const (
	compactPreparePercent            = 60 // 工具候选准备水位。
	compactAdoptPercent              = 75 // 工具采用及历史准备水位。
	compactGrowthPercent             = 8  // 普通周期结束后的新增量冷却。
	compactToolMinimumGainPercent    = 2  // 工具采用的实际净收益下限。
	compactHistoryMinimumGainPercent = 8  // 历史采用的实际净收益下限。
)

type CompactState struct {
	ID                                       string
	Kind                                     memory.CompactKind
	State                                    string
	Wait                                     string
	PrepareElapsed                           time.Duration
	WaitElapsed                              time.Duration
	BeforeTokens, AfterTokens, ContextWindow int
	Reason, Error                            string
}

type compactCandidate struct {
	plan        *memory.CompactPlan
	output      memory.CompactOutput
	err         error
	state       CompactState
	started     time.Time
	waitStarted time.Time
	done        chan struct{}
	cancel      context.CancelFunc
}

// ContextMemory 只持有一份候选；调用方串行执行历史修改，后台只生成不可变结果。
// 通知回调永不在内部锁内调用，网络与持久化也不持此锁。
type ContextMemory struct {
	mu             sync.Mutex
	notifyMu       sync.Mutex
	commitMu       sync.Mutex
	epoch          uint64
	lastRequest    CompactRequest
	candidate      *compactCandidate
	current        CompactState
	summary        string
	growth         int
	cooled         bool
	toolsUnhelpful bool
	manualID       string
	binding        *model.ModelBinding
	Notify         func(CompactState)
	Ready          func()
}

func NewContextMemory(summary string) *ContextMemory { return &ContextMemory{summary: summary} }
func (m *ContextMemory) Summary() string             { m.mu.Lock(); defer m.mu.Unlock(); return m.summary }
func (m *ContextMemory) Snapshot() CompactState {
	m.mu.Lock()
	defer m.mu.Unlock()
	state := m.current
	if c := m.candidate; c != nil {
		if state.State == "preparing" && !c.started.IsZero() {
			state.PrepareElapsed = time.Since(c.started)
		}
		if !c.waitStarted.IsZero() {
			state.WaitElapsed = time.Since(c.waitStarted)
		}
	}
	return state
}
func (m *ContextMemory) AddGrowth(messages []memory.Message) {
	m.mu.Lock()
	m.growth += memory.EstimateMessagesTokens(messages)
	m.mu.Unlock()
}
func (m *ContextMemory) emit(state CompactState) {
	// 生成、采用与取消可以并发完成；串行发布并复核当前状态，丢弃迟到快照。
	// 不持状态锁调用外围，避免通知反向读取会话状态时形成锁环。
	m.notifyMu.Lock()
	defer m.notifyMu.Unlock()
	m.mu.Lock()
	current := m.current
	valid := state.State != "" && current.ID == state.ID && current.State == state.State && current.Wait == state.Wait
	m.mu.Unlock()
	if valid && m.Notify != nil {
		m.Notify(state)
	}
}
func (m *ContextMemory) Cancel() { m.cancel(true) }
func (m *ContextMemory) Stop()   { m.cancel(false) }
func (m *ContextMemory) cancel(notify bool) {
	m.commitMu.Lock()
	m.mu.Lock()
	m.epoch++
	c := m.candidate
	if c == nil {
		m.lastRequest = CompactRequest{}
		m.manualID = ""
		m.mu.Unlock()
		m.commitMu.Unlock()
		return
	}
	c.cancel()
	m.candidate = nil
	m.lastRequest = CompactRequest{}
	state := c.state
	if state.State == "preparing" && !c.started.IsZero() {
		state.PrepareElapsed = time.Since(c.started)
	}
	if !c.waitStarted.IsZero() {
		state.WaitElapsed = time.Since(c.waitStarted)
	}
	state.State = "cancelled"
	state.Wait = ""
	m.current = state
	m.cooled = true
	m.growth = 0
	m.mu.Unlock()
	m.commitMu.Unlock()
	if notify {
		m.emit(state)
	}
}
func (m *ContextMemory) Bind(binding *model.ModelBinding) {
	m.mu.Lock()
	old := m.binding
	m.mu.Unlock()
	if old != nil && !old.SameRoute(binding) {
		m.Cancel()
	}
	m.mu.Lock()
	m.binding = binding
	m.mu.Unlock()
}

type CompactRequest struct {
	Binding     *model.ModelBinding
	Invocation  model.Invocation
	Working     *memory.WorkingMemory
	Request     model.CompletionRequest
	Coefficient float64
	Calibrated  bool
	// Project 复用本次已取得的只读背景，前后容量比较不重复触发画像使用副作用。
	Project   func([]memory.Message) []model.Message
	Commit    func(context.Context, string, []memory.Message) error
	Usage     func(*model.Usage)
	ReadyOnly bool
}

func (q CompactRequest) input(messages []memory.Message, summary string) int {
	request := q.Request
	request.SessionState = summary
	if q.Project != nil {
		request.Messages = q.Project(messages)
	} else {
		request.Messages = memory.ModelMessages(messages)
	}
	return estimateInputTokens(&request, q.Coefficient)
}
func (q CompactRequest) budget() int {
	usable := usableInputBudget(q.Binding.ContextWindow(), q.Request.MaxTokens)
	return max(1, usable-estimatorSafetyTokens(usable, q.Calibrated))
}

// Maintain 在下一物理请求前调用；普通准备不等待，hard/manual复用同一候选和期限。
func (m *ContextMemory) Maintain(ctx context.Context, compressor *memory.Compressor, q CompactRequest, manual bool) error {
	if compressor == nil {
		return nil
	}
	m.Bind(q.Binding)
	m.mu.Lock()
	// 手动预留复用的请求若已先失败，仍返回同一ID的结果，不偷换另一笔请求。
	terminal := m.current
	reuseTerminal := manual && m.candidate == nil && m.manualID != "" && terminal.ID == m.manualID
	if reuseTerminal || (manual && m.candidate != nil) {
		m.manualID = ""
	}
	m.mu.Unlock()
	if reuseTerminal {
		m.emit(terminal)
		return nil
	}
	m.mu.Lock()
	epoch := m.epoch
	if m.candidate != nil {
		m.lastRequest = q
	}
	m.mu.Unlock()
	// 普通周期只采用一次；硬缺口可继续处理尚未处理的来源，每次必须真正缩小上下文。
	// 已处理的历史边界随替换前移，避免为同一份新摘要反复付费。
	historyEnd, toolsEnd := 0, 0
	for {
		messages := q.Working.Messages()
		summary := m.Summary()
		before := q.input(messages, summary)
		budget := q.budget()
		hard := before >= budget
		wait := ""
		if manual {
			wait = "manual"
		} else if hard && !q.ReadyOnly {
			// 空闲采用不是前台等待，不能借通知反向等待 daemon 的 run 准入锁。
			wait = "hard_limit"
		}
		m.mu.Lock()
		c := m.candidate
		cool := m.cooled && model.ApplyCoefficient(m.growth, q.Coefficient) < budget*compactGrowthPercent/100
		m.mu.Unlock()
		if c == nil {
			if q.ReadyOnly {
				return nil
			}
			if err := ctx.Err(); err != nil {
				return err
			}
			if !manual && !hard && (before < budget*compactPreparePercent/100 || cool) {
				return nil
			}
			deadline := time.Now().Add(memory.CompactDeadline)
			var reservation *compactCandidate
			if wait != "" {
				acquireCtx, cancel := context.WithDeadline(ctx, deadline)
				reservation = &compactCandidate{done: make(chan struct{}), cancel: cancel, waitStarted: time.Now(), state: CompactState{ID: m.nextID(), State: "preparing", Wait: wait}}
				m.mu.Lock()
				if m.epoch != epoch || ctx.Err() != nil {
					m.mu.Unlock()
					cancel()
					return context.Canceled
				}
				m.candidate, m.current = reservation, reservation.state
				m.mu.Unlock()
				m.emit(reservation.state)
				err := compressor.Acquire(acquireCtx)
				cancel()
				if err != nil {
					status := "failed"
					if ctx.Err() != nil || err == context.Canceled {
						status = "cancelled"
					}
					m.emit(m.finish(reservation, status, "", err))
					return err
				}
			} else if !compressor.TryBackground() {
				return nil
			}
			plan, err := m.planCandidate(q, messages, summary, before, manual || hard, historyEnd, toolsEnd)
			if err != nil || plan == nil {
				compressor.ReleaseBackground()
				m.mu.Lock()
				m.cooled = true
				m.growth = 0
				m.mu.Unlock()
				if reservation != nil {
					status, reason := "skipped", "no_source"
					if err != nil || hard {
						status, reason = "failed", ""
						if err == nil {
							err = fmt.Errorf("context capacity exceeded: no safe compression source")
						}
					}
					m.emit(m.finish(reservation, status, reason, err))
					return err
				}
				if err != nil {
					state := CompactState{ID: m.nextID(), State: "failed", Error: err.Error()}
					m.mu.Lock()
					m.current = state
					m.mu.Unlock()
					m.emit(state)
					return nil
				}
				return nil
			}

			generationCtx, cancel := context.WithDeadline(context.WithoutCancel(ctx), deadline)
			id, waitStarted := "", time.Time{}
			if reservation != nil {
				id, waitStarted = reservation.state.ID, reservation.waitStarted
			} else {
				id = m.nextID()
			}
			c = &compactCandidate{plan: plan, started: time.Now(), done: make(chan struct{}), cancel: cancel, waitStarted: waitStarted, state: CompactState{ID: id, Kind: plan.Kind, State: "preparing", Wait: wait}}
			m.mu.Lock()
			if m.epoch != epoch || ctx.Err() != nil {
				m.mu.Unlock()
				cancel()
				compressor.ReleaseBackground()
				if reservation != nil {
					m.emit(m.finish(reservation, "cancelled", "", context.Canceled))
				}
				return context.Canceled
			}
			m.candidate = c
			m.lastRequest = q
			m.current = c.state
			m.mu.Unlock()
			m.emit(c.state)
			go m.generate(generationCtx, compressor, q, c)
		}
		if ready, err := m.waitCandidate(ctx, c, wait); err != nil || !ready {
			return err
		}
		if !manual && !hard && ((c.plan.Kind == memory.CompactTools && before < budget*compactAdoptPercent/100) || pendingUser(messages)) {
			return nil
		}
		replaced, newSummary, err := c.plan.Apply(messages, summary, c.output)
		if err != nil {
			// 真实原因必须落日志：候选来源、输出对齐与保存失败在旧版本里全被压成空错误。
			m.emit(m.finish(c, "skipped", "source_changed", err))
			if hard {
				return err
			}
			return nil
		}
		after := q.input(replaced, newSummary)
		required := budget * compactToolMinimumGainPercent / 100
		if c.plan.Kind == memory.CompactHistory {
			required = budget * compactHistoryMinimumGainPercent / 100
		}
		if after >= before || (!manual && !hard && before-after < required) {
			m.emit(m.finish(c, "skipped", "no_gain", nil))
			if hard {
				return fmt.Errorf("compression did not release enough context")
			}
			return nil
		}
		if err := m.commitCandidate(ctx, q, c, epoch, replaced, newSummary, before, after); err != nil {
			return err
		}
		removed := len(messages) - len(replaced)
		if c.plan.Kind == memory.CompactHistory {
			historyEnd = c.plan.HistoryStart + len(c.plan.Prefix) - removed
			toolsEnd = max(historyEnd, toolsEnd-removed)
		} else {
			// keep 的组也属于本次已处理来源，硬缺口不能马上再次为同一组付费。
			last := c.plan.Groups[len(c.plan.Groups)-1]
			toolsEnd = last.Index + len(last.Messages) - removed
		}
		if manual || q.ReadyOnly || after < budget {
			return nil
		}
	}
}

// planCandidate 仅选择本周期来源；仍先占共享槽，避免无槽时规划和保留历史副本。
func (m *ContextMemory) planCandidate(q CompactRequest, messages []memory.Message, summary string, before int, force bool, historyEnd, toolsEnd int) (*memory.CompactPlan, error) {
	budget := q.budget()
	limit := usableInputBudget(q.Binding.ContextWindow(), 0)
	limit -= estimatorSafetyTokens(limit, q.Calibrated)
	m.mu.Lock()
	if model.ApplyCoefficient(m.growth, q.Coefficient) >= budget*compactGrowthPercent/100 {
		m.toolsUnhelpful = false
	}
	skipTools := m.toolsUnhelpful && !force
	m.mu.Unlock()
	var plan *memory.CompactPlan
	var err error
	if !skipTools {
		start := max(historyEnd, toolsEnd)
		plan, err = memory.PlanCompact(messages, summary, memory.CompactTools, budget, limit, q.Binding.MaxOutputTokens(), q.Coefficient, force, start)
	}
	if err == nil && plan == nil && (force || before >= budget*compactAdoptPercent/100) {
		// 同一次 hard 接管中，已归并的 summary/原话不再作为新来源。
		// 剩余连续历史仍从其最早边界开始，旧 summary 仅作为只读背景。
		start := max(0, historyEnd)
		plan, err = memory.PlanCompact(messages[start:], summary, memory.CompactHistory, budget, limit, q.Binding.MaxOutputTokens(), q.Coefficient, force)
		if plan != nil {
			plan.HistoryStart = start
		}
	}
	return plan, err
}

// waitCandidate 只在 hard/manual 等待；普通后台未完成或失败时不阻塞前台。
func (m *ContextMemory) waitCandidate(ctx context.Context, c *compactCandidate, wait string) (bool, error) {
	if wait != "" {
		m.mu.Lock()
		if m.candidate != c {
			err := c.err
			m.mu.Unlock()
			if err != nil {
				return false, err
			}
			return false, context.Canceled
		}
		if c.state.Wait != wait {
			c.state.Wait = wait
			c.waitStarted = time.Now()
			m.current = c.state
		}
		state := c.state
		if !c.waitStarted.IsZero() {
			state.WaitElapsed = time.Since(c.waitStarted)
		}
		m.mu.Unlock()
		m.emit(state)
		select {
		case <-c.done:
		case <-ctx.Done():
			m.Cancel()
			return false, ctx.Err()
		}
	} else {
		select {
		case <-c.done:
		default:
			return false, nil
		}
	}
	m.mu.Lock()
	if m.candidate != c {
		err := c.err
		m.mu.Unlock()
		// 可选后台维护的失败不能变成主任务失败；真实 hard/manual 等待仍明确返回。
		if wait == "" {
			return false, ctx.Err()
		}
		if err != nil {
			return false, err
		}
		return false, context.Canceled
	}
	m.mu.Unlock()
	if c.err != nil {
		m.emit(m.finish(c, "failed", "", c.err))
		if wait != "" {
			return false, c.err
		}
		return false, nil
	}
	return true, nil
}

// commitCandidate 与取消共用提交锁；落库成功后才更新 RAM 并在锁外发布成果。
func (m *ContextMemory) commitCandidate(ctx context.Context, q CompactRequest, c *compactCandidate, epoch uint64, messages []memory.Message, summary string, before, after int) error {
	m.commitMu.Lock()
	m.mu.Lock()
	valid := m.candidate == c && m.epoch == epoch
	state := c.state
	m.mu.Unlock()
	if !valid || ctx.Err() != nil {
		m.commitMu.Unlock()
		return context.Canceled
	}
	if q.Commit != nil {
		if err := q.Commit(ctx, summary, messages); err != nil {
			m.commitMu.Unlock()
			m.emit(m.finish(c, "failed", "", err))
			return err
		}
	}
	q.Working.SetMessages(messages)
	m.mu.Lock()
	m.summary = summary
	state.BeforeTokens = before
	state.AfterTokens = after
	state.ContextWindow = q.Binding.ContextWindow()
	c.state = state
	m.mu.Unlock()
	state = m.finish(c, "applied", "", nil)
	m.commitMu.Unlock()
	m.emit(state)
	return nil
}

func pendingUser(messages []memory.Message) bool {
	for i := len(messages) - 1; i >= 0; i-- {
		if memory.IsUserMessage(messages[i]) {
			return !messages[i].Consumed
		}
	}
	return false
}
func (m *ContextMemory) generate(ctx context.Context, c *memory.Compressor, q CompactRequest, candidate *compactCandidate) {
	output, usage, err := c.Generate(ctx, q.Binding, q.Invocation, candidate.plan)
	candidate.cancel()
	if usage != nil && q.Usage != nil {
		q.Usage(usage)
	}
	m.mu.Lock()
	candidate.output = output
	candidate.err = err
	candidate.state.PrepareElapsed = time.Since(candidate.started)
	active := m.candidate == candidate
	if active {
		candidate.state.State = "ready"
		m.current = candidate.state
	}
	state := candidate.state
	close(candidate.done)
	m.mu.Unlock()
	if active {
		if err != nil {
			m.emit(m.finish(candidate, "failed", "", err))
			return
		}
		m.emit(state)
		if m.Ready != nil {
			m.Ready()
		}
	}
}
func (m *ContextMemory) finish(c *compactCandidate, status, reason string, err error) CompactState {
	m.mu.Lock()
	if m.candidate != c {
		m.mu.Unlock()
		return CompactState{}
	}
	state := c.state
	state.State = status
	state.Reason = reason
	state.Wait = ""
	if err != nil {
		state.Error = err.Error()
	}
	if !c.waitStarted.IsZero() {
		state.WaitElapsed = time.Since(c.waitStarted)
	}
	m.current = state
	m.candidate = nil
	m.cooled = true
	m.growth = 0
	if c.plan != nil && c.plan.Kind == memory.CompactTools && status == "skipped" && reason == "no_gain" {
		m.toolsUnhelpful = true
		// 下一安全边界允许改选历史；仍不在本周期串联第二次生成。
		m.cooled = false
	}
	if status == "applied" {
		m.toolsUnhelpful = false
	}
	m.lastRequest = CompactRequest{}
	m.mu.Unlock()
	return state
}

// ApplyReady 只更新已有候选，不在空闲回调里发起或等待新的付费请求。
func (m *ContextMemory) ApplyReady(ctx context.Context, c *memory.Compressor) error {
	m.mu.Lock()
	q := m.lastRequest
	ready := m.candidate != nil && m.current.State == "ready"
	m.mu.Unlock()
	if !ready {
		return nil
	}
	q.ReadyOnly = true
	return m.Maintain(ctx, c, q, false)
}

func (m *ContextMemory) ReserveManual() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.candidate != nil {
		m.manualID = m.candidate.state.ID
		return m.manualID
	}
	m.manualID = uuid.NewString()
	return m.manualID
}
func (m *ContextMemory) nextID() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.manualID != "" {
		id := m.manualID
		m.manualID = ""
		return id
	}
	return uuid.NewString()
}
