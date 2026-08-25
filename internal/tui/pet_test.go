package tui

import (
	"strconv"
	"strings"
	"testing"
	"time"

	uipage "github.com/alanchenchen/suna/internal/tui/pages/page"
)

// 纯色块宠物：渲染结果不得包含边框字符。
func TestRenderPetNoBorder(t *testing.T) {
	for _, state := range []petState{petIdle, petWorking, petThinking} {
		for frame := 0; frame < 8; frame++ {
			big := renderPet(state, frame)
			mini := renderMiniPet(state, frame)
			for _, ch := range []string{"╭", "╮", "╰", "╯", "│"} {
				if strings.Contains(big, ch) {
					t.Fatalf("renderPet(%v, %d) contains border char %q: %q", state, frame, ch, big)
				}
				if strings.Contains(mini, ch) {
					t.Fatalf("renderMiniPet(%v, %d) contains border char %q: %q", state, frame, ch, mini)
				}
			}
		}
	}
}

// 尺寸不变：renderPet 固定 4 行，renderMiniPet 固定 3 行。
func TestRenderPetSizeStable(t *testing.T) {
	big := renderPet(petIdle, 0)
	if got, want := len(strings.Split(big, "\n")), 4; got != want {
		t.Fatalf("renderPet rows = %d, want %d", got, want)
	}
	mini := renderMiniPet(petIdle, 0)
	if got, want := len(strings.Split(mini, "\n")), 3; got != want {
		t.Fatalf("renderMiniPet rows = %d, want %d", got, want)
	}
}

// 帧切换：每个状态的帧序列内至少出现两种不同渲染（动画生效），且都包含色块填充。
// 注意 idle 眨眼动画包含重复的睁眼帧，因此不能要求相邻帧必不相同。
func TestRenderPetFrameSwitches(t *testing.T) {
	for _, state := range []petState{petIdle, petWorking, petThinking} {
		seen := map[string]bool{}
		for frame := 0; frame < len(petFaces(state)); frame++ {
			out := renderMiniPet(state, frame)
			if !strings.Contains(out, "\x1b[") {
				t.Fatalf("renderMiniPet(%v, %d) missing ANSI color codes", state, frame)
			}
			seen[out] = true
		}
		if len(seen) < 2 {
			t.Fatalf("renderMiniPet(%v) has only %d distinct frames, animation not effective", state, len(seen))
		}
	}
}

// 状态差异：idle/working/thinking/happy 的帧集合互不相同。
func TestPetFacesDistinct(t *testing.T) {
	states := []petState{petIdle, petWorking, petThinking, petHappy}
	sets := map[petState]map[string]bool{}
	for _, state := range states {
		sets[state] = map[string]bool{}
		for _, f := range petFaces(state) {
			sets[state][f.eyes+"|"+strconv.Itoa(f.shift)] = true
		}
	}
	for _, a := range states {
		for _, b := range states {
			if a == b {
				continue
			}
			for key := range sets[a] {
				if sets[b][key] {
					t.Fatalf("face %q shared between state %v and %v", key, a, b)
				}
			}
		}
	}
}

// happy 状态：run 完成后开心眼，且到期后回到 idle。
func TestPetHappyState(t *testing.T) {
	tui := &TUI{}
	tui.chat.Loading = false
	if got := tui.chatPetState(); got != petIdle {
		t.Fatalf("pet state before happy = %v, want petIdle", got)
	}
	tui.petHappyUntil = time.Now().Add(time.Hour)
	if got := tui.chatPetState(); got != petHappy {
		t.Fatalf("pet state during happy = %v, want petHappy", got)
	}
	tui.petHappyUntil = time.Now().Add(-time.Second)
	if got := tui.chatPetState(); got != petIdle {
		t.Fatalf("pet state after happy expiry = %v, want petIdle", got)
	}
}

// 工作帧对称：working 的眼睛帧左右对称（◒◒  ◒◒ / ◓◓  ◓◓），
// 避免不对称帧造成水平偏移的观感。
func TestPetWorkingFramesSymmetric(t *testing.T) {
	for _, f := range petFaces(petWorking) {
		// 帧内容为“左眼(2列) + 两空格 + 右眼(2列)”，去掉空格后左右应相同。
		trimmed := strings.ReplaceAll(f.eyes, " ", "")
		runes := []rune(trimmed)
		if len(runes) != 4 || runes[0] != runes[2] || runes[1] != runes[3] {
			t.Fatalf("working face %q not symmetric", f.eyes)
		}
	}
}

// thinking 动画：帧序列内至少出现两种不同渲染（圆点旋转生效）。
func TestPetThinkingFrames(t *testing.T) {
	seen := map[string]bool{}
	for _, f := range petFaces(petThinking) {
		seen[f.eyes] = true
	}
	if len(seen) < 2 {
		t.Fatal("petThinking should have multiple distinct eye frames")
	}
}

// 帧索引越界安全：任意 frame 都能取到有效五官。
func TestRenderPetFrameOverflowSafe(t *testing.T) {
	for _, state := range []petState{petIdle, petWorking, petThinking} {
		for frame := 0; frame < 100; frame++ {
			out := renderMiniPet(state, frame)
			if out == "" {
				t.Fatalf("renderMiniPet(%v, %d) empty", state, frame)
			}
		}
	}
}

// tick 链防重入：已 ticking 时 startPetTick 返回 nil。
func TestStartPetTickReentrant(t *testing.T) {
	tui := &TUI{mode: uipage.Chat}
	cmd := tui.startPetTick()
	if cmd == nil {
		t.Fatal("first startPetTick returned nil")
	}
	if !tui.petTicking {
		t.Fatal("petTicking not set after start")
	}
	if got := tui.startPetTick(); got != nil {
		t.Fatal("second startPetTick should return nil while ticking")
	}
}

// 离开 Chat/Welcome 时链仍延续（与 inputCursorBlink 同模式），帧只在对应页面渲染时被消费。
func TestUpdatePetTickContinuesOutsidePages(t *testing.T) {
	tui := &TUI{mode: uipage.Config}
	if cmd := tui.updatePetTick(); cmd == nil {
		t.Fatal("updatePetTick should keep the tick chain alive")
	}
	if tui.petFrame != 1 {
		t.Fatalf("petFrame = %d, want 1", tui.petFrame)
	}
}

// 帧间隔：working 快于 idle。
func TestPetTickInterval(t *testing.T) {
	tui := &TUI{}
	// idle 状态（chat.Loading=false）
	tui.chat.Loading = false
	idle := tui.petTickInterval()
	// working 状态
	tui.chat.Loading = true
	tui.chat.Phase = phaseTool
	work := tui.petTickInterval()
	if work >= idle {
		t.Fatalf("working interval %v should be shorter than idle %v", work, idle)
	}
}

// petTickMsg 推进帧计数。
func TestPetTickAdvancesFrame(t *testing.T) {
	tui := &TUI{mode: uipage.Chat, petTicking: true}
	before := tui.petFrame
	cmd := tui.updatePetTick()
	if cmd == nil {
		t.Fatal("updatePetTick in Chat should return next tick cmd")
	}
	if tui.petFrame != before+1 {
		t.Fatalf("petFrame = %d, want %d", tui.petFrame, before+1)
	}
}

// 验证 tick 消息类型可被 Update 消费并持续链。
func TestPetTickMessageFlow(t *testing.T) {
	tui := &TUI{mode: uipage.Chat}
	_ = tui.startPetTick()
	model, cmd := tui.Update(petTickMsg{})
	if model == nil {
		t.Fatal("Update returned nil model")
	}
	if cmd == nil {
		t.Fatal("Update should return next tick cmd in Chat")
	}
	if got := tui.petFrame; got != 1 {
		t.Fatalf("petFrame after one tick = %d, want 1", got)
	}
}
