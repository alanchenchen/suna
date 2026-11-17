package memory

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/alanchenchen/suna/internal/model"
)

func TestToolSelectionStopsAtTargetAndOmitsUnrelatedContext(t *testing.T) {
	messages := compactToolBatch(6, 6000, 3000)
	q := testCompactOptions(6000)
	p, err := PlanCompact(messages, "unrelated previous summary", q)
	if err != nil || p == nil || p.Kind != CompactTools || len(p.Groups) != 2 {
		t.Fatalf("plan=%+v err=%v", p, err)
	}
	if len(p.Prefix) != 0 || p.Summary != "" || p.ExpectedGain < q.TargetGain {
		t.Fatalf("plan=%+v", p)
	}
	payload, err := compactPayload(p)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(payload), "unrelated previous summary") || len(p.Background) != 2 {
		t.Fatal("unrelated summary or latest tail leaked into request")
	}
	if compactRequestTokens(t, p) > p.inputTokens {
		t.Fatalf("actual input=%d planned=%d", compactRequestTokens(t, p), p.inputTokens)
	}
}

func TestToolTargetToleranceDoesNotSacrificeHistory(t *testing.T) {
	messages := compactToolBatch(1, 6000, 3000)
	q := testCompactOptions(4500)
	q.Tolerance = 1000
	p, err := PlanCompact(messages, "old", q)
	if err != nil || p == nil || p.Kind != CompactTools || len(p.Prefix) != 0 {
		t.Fatalf("plan=%+v err=%v", p, err)
	}
}

func TestMixedPlanUsesMinimumHeadAndNoDuplicateSources(t *testing.T) {
	messages := []Message{consumedMessage(model.RoleUser, "old requirement"), consumedMessage(model.RoleAssistant, compactSizedText(5000))}
	tools := compactToolBatch(1, 4000, 3000)
	messages = append(messages, tools...)
	q := testCompactOptions(5000)
	p, err := PlanCompact(messages, "old summary", q)
	if err != nil || p == nil || p.Kind != CompactMixed || len(p.Prefix) != 2 || len(p.Groups) != 1 {
		t.Fatalf("plan=%+v err=%v", p, err)
	}
	if p.Groups[0].Index < len(p.Prefix) || p.Summary != "old summary" {
		t.Fatal("overlapping source or missing old summary")
	}
	for _, m := range p.Background {
		if m.Text() == "old requirement" || m.Text() == messages[1].Text() {
			t.Fatal("head duplicated as background")
		}
	}
	applied, err := p.Apply(messages, "old summary", CompactOutput{Summary: "new summary", Observations: []ToolObservation{{ID: p.Groups[0].Index, Text: "read evidence"}}})
	if err != nil || applied.Kind != CompactMixed || applied.Summary != "new summary" {
		t.Fatalf("applied=%+v err=%v", applied, err)
	}
	if !reflect.DeepEqual(applied.Messages[len(applied.Messages)-2:], messages[len(messages)-2:]) {
		t.Fatal("tail changed")
	}
}

func TestHistorySelectionAbsorbsOverlappingToolsOnce(t *testing.T) {
	messages := toolHistory()
	q := testCompactOptions(5000)
	p, err := PlanCompact(messages, "old", q)
	if err != nil || p == nil || len(p.Prefix) == 0 {
		t.Fatalf("plan=%+v err=%v", p, err)
	}
	for _, g := range p.Groups {
		if g.Index < len(p.Prefix) {
			t.Fatal("overlap")
		}
	}
	payload, err := compactPayload(p)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(payload), "call-a") != 2 {
		t.Fatalf("source repeated or omitted: %s", payload)
	}
	if p.SourceTokens != EstimateMessagesTokens(p.Prefix)+model.EstimateTokens("old") {
		t.Fatalf("source tokens=%d", p.SourceTokens)
	}
}

func TestPlanProtectsRecentUnconsumedAndIncompleteSources(t *testing.T) {
	for _, tt := range []struct {
		name   string
		modify func([]Message) []Message
	}{
		{name: "unconsumed declaration", modify: func(m []Message) []Message { m[1].Consumed = false; return m }},
		{name: "unconsumed result", modify: func(m []Message) []Message { m[2].Consumed = false; return m }},
		{name: "missing result", modify: func(m []Message) []Message { return append(m[:3], m[4:]...) }},
	} {
		t.Run(tt.name, func(t *testing.T) {
			messages := tt.modify(toolHistory())
			p, err := PlanCompact(messages, "", testCompactOptions(1000))
			if err != nil || p != nil {
				t.Fatalf("plan=%+v err=%v want no safe source", p, err)
			}
		})
	}
	messages := []Message{consumedMessage(model.RoleUser, "request"), consumedMessage(model.RoleAssistant, compactSizedText(5000))}
	if p, err := PlanCompact(messages, "", testCompactOptions(1000)); err != nil || p != nil {
		t.Fatalf("recent whole message was selected: %v %v", p, err)
	}
}

func TestPlanRespectsFullRequestCapacityAndNeverCutsToolGroup(t *testing.T) {
	messages := compactToolBatch(6, 6000, 3000)
	q := testCompactOptions(50000)
	q.InputLimit = 18000
	q.OutputLimit = 4000
	p, err := PlanCompact(messages, "old", q)
	if err != nil || p == nil {
		t.Fatalf("plan=%+v err=%v", p, err)
	}
	if compactRequestTokens(t, p)+p.OutputBudget > q.InputLimit {
		t.Fatal("serialized request exceeds capacity")
	}
	for _, g := range p.Groups {
		if len(g.Messages) != 2 || g.Messages[1].Text() != compactSizedText(6000) {
			t.Fatal("tool group split")
		}
	}
}

func TestPlanAcceptsOutputLimitedLargeSourcesWithoutArbitraryInputCap(t *testing.T) {
	messages := compactToolBatch(6, 50000, 100100)
	q := CompactOptions{Budget: 1000000, TargetGain: 120000, MinimumGain: 20000, InputLimit: 1000000, OutputLimit: 8000, Coefficient: 1}
	p, err := PlanCompact(messages, "old", q)
	if err != nil || p == nil || p.Kind != CompactTools || len(p.Groups) != 3 || p.OutputBudget != 8000 {
		t.Fatalf("plan=%+v err=%v", p, err)
	}
	if compactRequestTokens(t, p)+p.OutputBudget > q.InputLimit {
		t.Fatal("request does not fit")
	}
}

func TestBackgroundReferencesArePositionDeduplicated(t *testing.T) {
	messages := compactToolBatch(2, 6000, 3000)
	q := testCompactOptions(6000)
	p, err := PlanCompact(messages, "", q)
	if err != nil || p == nil || len(p.Background) != 2 {
		t.Fatalf("background=%v plan=%v err=%v", p.Background, p, err)
	}
	if p.Background[0].Text() != "inspect all evidence" || p.Background[1].Text() != "complete explanation" {
		t.Fatalf("background=%v", p.Background)
	}
}

func TestExcludedToolsDoNotReenterPaidPlanAfterAppendingTail(t *testing.T) {
	messages := compactToolBatch(2, 6000, 3000)
	first, err := PlanCompact(messages, "", testCompactOptions(1000))
	if err != nil || first == nil {
		t.Fatalf("plan=%v err=%v", first, err)
	}
	q := testCompactOptions(1000)
	q.ExcludedTools = map[string]bool{first.Groups[0].SourceKey(): true}
	messages = append(messages, consumedMessage(model.RoleUser, "new suffix"))
	p, err := PlanCompact(messages, "", q)
	if err != nil || p == nil || len(p.Groups) != 1 || p.Groups[0].Messages[0].ToolCalls[0].ID != "batch-1" {
		t.Fatalf("plan=%+v err=%v", p, err)
	}
}

func TestApplyPreservesOutsideSourceAndRejectsChangedOrigins(t *testing.T) {
	messages := toolHistory()
	p, err := PlanCompact(messages, "old", testCompactOptions(500))
	if err != nil || p == nil {
		t.Fatalf("plan=%v err=%v", p, err)
	}
	original := ProjectMessages(messages)
	suffix := NewTextMessage(model.RoleUser, "new correction")
	messages = append(messages, suffix)
	messages[1].Consumed = false
	applied, err := p.Apply(messages, "old", CompactOutput{Observations: []ToolObservation{{ID: 1, Text: "exact evidence"}}})
	if err != nil || applied.Kind != CompactTools || applied.Summary != "old" || !reflect.DeepEqual(applied.Messages[len(applied.Messages)-1], suffix) {
		t.Fatalf("applied=%+v err=%v", applied, err)
	}
	if !reflect.DeepEqual(applied.Messages[2:len(applied.Messages)-1], messages[4:len(messages)-1]) {
		t.Fatal("outside source changed")
	}
	if !reflect.DeepEqual(ProjectMessages(toolHistory()), original) {
		t.Fatal("source fixture changed")
	}
	messages[2].TextContent = "changed"
	messages[2].Content = nil
	if _, err = p.Apply(messages, "old", CompactOutput{}); err == nil {
		t.Fatal("changed source accepted")
	}
}

func TestHistoryApplyOnlyRemovesProgramSelectedPrefix(t *testing.T) {
	messages := []Message{consumedMessage(model.RoleUser, "old requirement"), consumedMessage(model.RoleAssistant, compactSizedText(5000)), consumedMessage(model.RoleUser, "later original"), consumedMessage(model.RoleAssistant, compactSizedText(5000)), NewTextMessage(model.RoleUser, "pending")}
	q := testCompactOptions(2000)
	p, err := PlanCompact(messages, "old", q)
	if err != nil || p == nil || p.Kind != CompactHistory || len(p.Prefix) != 2 {
		t.Fatalf("plan=%+v err=%v", p, err)
	}
	applied, err := p.Apply(messages, "old", CompactOutput{Summary: "new"})
	if err != nil || applied.Kind != CompactHistory || applied.Summary != "new" || !reflect.DeepEqual(applied.Messages, messages[2:]) {
		t.Fatalf("applied=%+v err=%v", applied, err)
	}
	if _, err = p.Apply(messages, "changed", CompactOutput{Summary: "new"}); err == nil {
		t.Fatal("changed summary accepted")
	}
	kept, err := p.Apply(messages, "old", CompactOutput{})
	if err != nil || kept.Kind != "" || !reflect.DeepEqual(kept.Messages, messages) {
		t.Fatalf("empty summary removed head: %+v %v", kept, err)
	}
}

func TestMixedApplyReportsOnlyActuallyAdoptedRegions(t *testing.T) {
	messages := append([]Message{consumedMessage(model.RoleUser, "old"), consumedMessage(model.RoleAssistant, compactSizedText(5000))}, compactToolBatch(1, 4000, 3000)...)
	p, err := PlanCompact(messages, "old", testCompactOptions(5000))
	if err != nil || p == nil || p.Kind != CompactMixed {
		t.Fatalf("plan=%+v err=%v", p, err)
	}
	for _, tt := range []struct {
		name string
		out  CompactOutput
		kind CompactKind
	}{
		{name: "history only", out: CompactOutput{Summary: "new"}, kind: CompactHistory},
		{name: "tool only", out: CompactOutput{Observations: []ToolObservation{{ID: p.Groups[0].Index, Text: "evidence"}}}, kind: CompactTools},
		{name: "neither", out: CompactOutput{}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			applied, err := p.Apply(messages, "old", tt.out)
			if err != nil || applied.Kind != tt.kind {
				t.Fatalf("applied=%+v err=%v", applied, err)
			}
			if tt.kind == CompactTools && !reflect.DeepEqual(applied.Messages[:2], messages[:2]) {
				t.Fatal("invalid history summary removed head")
			}
		})
	}
}

func TestToolObservationRepairPreservesAmbiguousAndUnprofitableGroups(t *testing.T) {
	messages := compactToolBatch(2, 4000, 3000)
	p, err := PlanCompact(messages, "", testCompactOptions(4000))
	if err != nil || p == nil || len(p.Groups) != 2 {
		t.Fatalf("plan=%+v err=%v", p, err)
	}
	out := CompactOutput{Observations: []ToolObservation{{ID: p.Groups[0].Index, Text: "first"}, {ID: p.Groups[0].Index, Text: "different"}, {ID: p.Groups[1].Index, Text: compactSizedText(5000)}}}
	applied, err := p.Apply(messages, "", out)
	if err != nil || applied.Kind != "" || len(applied.RetainedTools) != 2 || !reflect.DeepEqual(applied.Messages, messages) {
		t.Fatalf("applied=%+v err=%v", applied, err)
	}
}

func TestSourceFingerprintIgnoresConsumptionAndPositionNotContent(t *testing.T) {
	messages := toolHistory()
	group := ToolGroup{Index: 1, Messages: ProjectMessages(messages[1:4])}
	key := group.SourceKey()
	group.Index = 42
	group.Messages[0].Consumed = false
	if group.SourceKey() != key {
		t.Fatal("position or consumption changed fingerprint")
	}
	group.Messages[1].Content = nil
	group.Messages[1].TextContent = "changed"
	if group.SourceKey() == key {
		t.Fatal("content did not change fingerprint")
	}
}

func TestDenseToolIDsNeverAliasSourcePositions(t *testing.T) {
	groups := []ToolGroup{{Index: 2}, {Index: 5}}
	wire := compactToolGroups(groups)
	if wire[0].ID != 0 || wire[1].ID != 1 {
		t.Fatal("IDs not dense")
	}
	got := restoreToolGroupIndexes(groups, []ToolObservation{{ID: 5, Text: "stray"}, {ID: 1, Text: "evidence"}})
	if got[0].ID != -1 || got[1].ID != 5 {
		t.Fatalf("restored=%v", got)
	}
	raw, err := json.Marshal(CompactOutput{Summary: "facts"})
	if err != nil || strings.Contains(string(raw), "keep") {
		t.Fatal("history keep leaked")
	}
}

func TestToolReplacementUsesStableMediaProjectionAndRetainsWholeKeptGroup(t *testing.T) {
	messages := compactToolBatch(2, 4000, 3000)
	messages[1].TextContent = ""
	messages[1].Content = []model.ContentBlock{{Type: model.ContentText, Text: "first block"}, {Type: model.ContentImage, Media: &model.MediaRef{Kind: model.MediaAttachment, Name: "reference.png"}}, {Type: model.ContentText, Text: "second block"}}
	q := testCompactOptions(4000)
	p, err := PlanCompact(messages, "", q)
	if err != nil || p == nil || len(p.Groups) != 2 {
		t.Fatalf("plan=%v err=%v", p, err)
	}
	applied, err := p.Apply(messages, "", CompactOutput{Observations: []ToolObservation{{ID: 1, Text: "evidence"}, {ID: 3, Keep: true}}})
	if err != nil {
		t.Fatal(err)
	}
	text := applied.Messages[1].Text()
	if !strings.Contains(text, "first block") || !strings.Contains(text, "source=attachment:reference.png") || !strings.Contains(text, "second block") {
		t.Fatalf("prose=%q", text)
	}
	if !reflect.DeepEqual(applied.Messages[2:4], messages[3:5]) {
		t.Fatal("kept native group changed")
	}
}

func TestMixedHistoryThatGrowsCannotCancelToolSavings(t *testing.T) {
	messages := append([]Message{consumedMessage(model.RoleUser, "old"), consumedMessage(model.RoleAssistant, compactSizedText(5000))}, compactToolBatch(1, 4000, 3000)...)
	p, err := PlanCompact(messages, "old", testCompactOptions(5000))
	if err != nil || p == nil || p.Kind != CompactMixed {
		t.Fatalf("plan=%v err=%v", p, err)
	}
	applied, err := p.Apply(messages, "old", CompactOutput{Summary: compactSizedText(6000), Observations: []ToolObservation{{ID: p.Groups[0].Index, Text: "evidence"}}})
	if err != nil || applied.Kind != CompactTools || applied.Summary != "old" || !reflect.DeepEqual(applied.Messages[:2], messages[:2]) {
		t.Fatalf("kind=%s summary size=%d err=%v", applied.Kind, len(applied.Summary), err)
	}
}

func TestToolPlannerSkipsOversizedGroupForOtherUsefulSources(t *testing.T) {
	messages := compactToolBatch(2, 4000, 3000)
	messages[2] = consumedMessage(model.RoleTool, compactSizedText(50000))
	messages[2].ToolCallID = "batch-0"
	q := testCompactOptions(1000)
	q.InputLimit = 12000
	p, err := PlanCompact(messages, "", q)
	if err != nil || p == nil || p.Kind != CompactTools || len(p.Groups) != 1 || p.Groups[0].Index != 3 {
		t.Fatalf("plan=%v err=%v", p, err)
	}
}

func TestManyToolsOutputReserveCoversCompleteDenseStructure(t *testing.T) {
	messages := compactToolBatch(400, 500, 5000)
	q := testCompactOptions(1000000)
	q.Budget = 100000
	q.OutputLimit = 8000
	p, err := PlanCompact(messages, "", q)
	if err != nil || p == nil {
		t.Fatalf("plan=%v err=%v", p, err)
	}
	items := make([]ToolObservation, len(p.Groups))
	for i := range items {
		items[i] = ToolObservation{ID: i, Keep: true}
	}
	data, _ := json.Marshal(CompactOutput{Observations: items})
	if model.EstimateTokens(string(data)) > p.OutputBudget {
		t.Fatalf("minimum output=%d reserve=%d", model.EstimateTokens(string(data)), p.OutputBudget)
	}
}

func TestToolBackgroundDoesNotReadUnrelatedLaterAnswer(t *testing.T) {
	messages := compactToolBatch(1, 4000, 3000)
	messages = append(messages[:3], messages[4:]...)
	messages[len(messages)-1] = consumedMessage(model.RoleAssistant, "unrelated latest answer")
	p, err := PlanCompact(messages, "", testCompactOptions(1000))
	if err != nil || p == nil || p.Kind != CompactTools {
		t.Fatalf("plan=%v err=%v", p, err)
	}
	if len(p.Background) != 1 || p.Background[0].Text() != "inspect all evidence" {
		t.Fatalf("background=%v", p.Background)
	}
}

func TestHistoryRequestNeverContainsUnselectedOriginalTail(t *testing.T) {
	messages := []Message{consumedMessage(model.RoleUser, "old requirement"), consumedMessage(model.RoleAssistant, compactSizedText(5000)), consumedMessage(model.RoleUser, "private tail marker"), consumedMessage(model.RoleAssistant, compactSizedText(3000)), NewTextMessage(model.RoleUser, "pending marker")}
	p, err := PlanCompact(messages, "old decisions", testCompactOptions(2000))
	if err != nil || p == nil || p.Kind != CompactHistory || len(p.Prefix) != 2 {
		t.Fatalf("plan=%v err=%v", p, err)
	}
	data, err := compactPayload(p)
	if err != nil {
		t.Fatal(err)
	}
	for _, text := range []string{"private tail marker", "pending marker"} {
		if strings.Contains(string(data), text) {
			t.Fatalf("unselected suffix %q entered request", text)
		}
	}
	if !strings.Contains(string(data), "old decisions") || !strings.Contains(string(data), "old requirement") {
		t.Fatal("selected history or previous summary missing")
	}
}

func TestGainEstimateCalibratesBothRemovedAndReplacementTokens(t *testing.T) {
	messages := compactToolBatch(3, 4000, 3000)
	q := testCompactOptions(2500)
	q.Coefficient = 0.7
	p, err := PlanCompact(messages, "", q)
	if err != nil || p == nil {
		t.Fatalf("plan=%v err=%v", p, err)
	}
	want := model.ApplyCoefficient(p.SourceTokens-p.OutputBudget, q.Coefficient)
	if p.ExpectedGain != want {
		t.Fatalf("gain=%d want=%d", p.ExpectedGain, want)
	}
	if model.ApplyCoefficient(compactRequestTokens(t, p), q.Coefficient)+p.OutputBudget > q.InputLimit {
		t.Fatal("calibrated request does not fit")
	}
}

func TestPlannerMakesRoomForNecessaryHistoryInOneRequest(t *testing.T) {
	messages := []Message{consumedMessage(model.RoleUser, "earlier requirement"), consumedMessage(model.RoleAssistant, compactSizedText(10000))}
	messages = append(messages, compactToolBatch(3, 4000, 3000)...)
	q := testCompactOptions(11000)
	q.InputLimit, q.OutputLimit = 28000, 8000
	p, err := PlanCompact(messages, "", q)
	if err != nil || p == nil || p.Kind != CompactMixed || len(p.Prefix) != 2 || len(p.Groups) != 2 || p.ExpectedGain < q.TargetGain {
		t.Fatalf("plan=%+v err=%v", p, err)
	}
	if compactRequestTokens(t, p)+p.OutputBudget > q.InputLimit {
		t.Fatal("mixed request exceeds capacity")
	}
}

func TestPruneToolSourcesKeepsOnlyRemainingNativeGroups(t *testing.T) {
	messages := compactToolBatch(2, 4000, 3000)
	first := (ToolGroup{Messages: messages[1:3]}).SourceKey()
	second := (ToolGroup{Messages: messages[3:5]}).SourceKey()
	keys := map[string]bool{first: true, second: true, "obsolete": true}
	remaining := messages[3:]
	got := PruneToolSourceKeys(remaining, keys)
	if len(got) != 1 || !got[second] {
		t.Fatalf("keys=%v", got)
	}
	if len(keys) != 3 {
		t.Fatal("pruning mutated supplied set")
	}
	if got := PruneToolSourceKeys(nil, keys); len(got) != 0 {
		t.Fatalf("removed sources retained: %v", got)
	}
}

func TestJSONCostMatchesSerializedInputWithoutRetainingPreviousWrite(t *testing.T) {
	messages := toolHistory()
	observed := NewTextMessage(model.RoleAssistant, "prose")
	observed.Observation = &Observation{Text: "精确错误 <tag>\nwith \"quotes\" & evidence", Calls: []ToolLabel{{Name: "readfile", Status: "completed"}}}
	messages = append(messages, observed, Message{Role: model.RoleUser, Content: []model.ContentBlock{{Type: model.ContentImage, Media: &model.MediaRef{Kind: model.MediaAttachment, Name: "图片.png"}}}})
	values := []any{"", "ASCII", "中文🙂\u2028\u2029", "<>&\\\"\n\t", string([]byte{0xff, 0x80, 'a'}), nil, compactSizedText(50000)}
	for _, m := range ProjectMessages(messages) {
		values = append(values, compactSource(m))
	}
	var counter compactJSONEstimator
	for _, value := range values {
		data, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		want := model.EstimateTokens(string(data))
		if got := counter.tokens(value); got != want {
			t.Fatalf("type=%T got=%d want=%d", value, got, want)
		}
	}
	if got := counter.tokens(""); got != model.EstimateTokens(`""`) {
		t.Fatalf("previous data retained: %d", got)
	}
}

func TestSourceFingerprintPreservesBoundariesAndAllSourceFacts(t *testing.T) {
	original := ProjectMessages(toolHistory()[1:4])
	plan := &CompactPlan{Summary: "prior facts", Prefix: original}
	key := plan.SourceKey()
	for _, tt := range []struct {
		name   string
		change func(*CompactPlan)
	}{
		{name: "summary", change: func(p *CompactPlan) { p.Summary += " changed" }},
		{name: "text", change: func(p *CompactPlan) { p.Prefix[0].TextContent += " changed" }},
		{name: "role", change: func(p *CompactPlan) { p.Prefix[0].Role = model.RoleUser }},
		{name: "origin", change: func(p *CompactPlan) { p.Prefix[0].Origin = OriginTool }},
		{name: "call identity", change: func(p *CompactPlan) { p.Prefix[0].ToolCalls[0].ID += " changed" }},
		{name: "arguments", change: func(p *CompactPlan) { p.Prefix[0].ToolCalls[0].Arguments = `{"path":"different"}` }},
		{name: "intent", change: func(p *CompactPlan) { p.Prefix[0].ToolCalls[0].Intent = "new intent" }},
		{name: "result identity", change: func(p *CompactPlan) { p.Prefix[1].ToolCallID += " changed" }},
		{name: "result status", change: func(p *CompactPlan) { p.Prefix[1].ToolStatus = "failed" }},
		{name: "truncation", change: func(p *CompactPlan) { p.Prefix[1].Truncated = true }},
		{name: "observation", change: func(p *CompactPlan) { p.Prefix[1].Observation = &Observation{Text: "new evidence"} }},
	} {
		t.Run(tt.name, func(t *testing.T) {
			p := &CompactPlan{Summary: plan.Summary, Prefix: ProjectMessages(original)}
			tt.change(p)
			if p.SourceKey() == key {
				t.Fatal("changed source retained fingerprint")
			}
		})
	}
	left := &CompactPlan{Prefix: []Message{consumedMessage(model.RoleAssistant, "ab"), consumedMessage(model.RoleAssistant, "c")}}
	right := &CompactPlan{Prefix: []Message{consumedMessage(model.RoleAssistant, "a"), consumedMessage(model.RoleAssistant, "bc")}}
	if left.SourceKey() == right.SourceKey() {
		t.Fatal("message boundary disappeared")
	}
	otherRegion := &CompactPlan{Summary: plan.Summary, Groups: []ToolGroup{{Messages: original}}}
	if plan.SourceKey() == otherRegion.SourceKey() {
		t.Fatal("region boundary disappeared")
	}
	if !reflect.DeepEqual(original, ProjectMessages(toolHistory()[1:4])) {
		t.Fatal("fingerprint mutated source")
	}
}

func TestPlannerFallbackKeepsSourceAndCapacityInvariants(t *testing.T) {
	// 交叉覆盖头部扩展、独立工具让位和最佳计划回退，避免只验证恰好达标的路径。
	for _, limit := range []int{6000, 12000, 20000, 28000, 50000} {
		for _, target := range []int{1500, 7000, 15000, 100000} {
			for _, coefficient := range []float64{0.7, 1, 1.5} {
				name := fmt.Sprintf("limit_%d_target_%d_coefficient_%.1f", limit, target, coefficient)
				t.Run(name, func(t *testing.T) {
					messages := []Message{consumedMessage(model.RoleUser, "earlier requirement"), consumedMessage(model.RoleAssistant, compactSizedText(6000))}
					messages = append(messages, compactToolBatch(4, 3000, 4000)...)
					q := testCompactOptions(target)
					q.InputLimit, q.OutputLimit, q.Coefficient = limit, 8000, coefficient
					p, err := PlanCompact(messages, "prior facts", q)
					if err != nil {
						t.Fatal(err)
					}
					if p == nil {
						return
					}
					if model.ApplyCoefficient(compactRequestTokens(t, p), coefficient)+p.OutputBudget > limit {
						t.Fatalf("returned plan exceeds request capacity: head=%d groups=%d", len(p.Prefix), len(p.Groups))
					}
					source := 0
					if len(p.Prefix) > 0 {
						source = EstimateMessagesTokens(p.Prefix) + model.EstimateTokens("prior facts")
					}
					end := len(p.Prefix)
					output := CompactOutput{Summary: "updated facts"}
					for _, group := range p.Groups {
						if group.Index < end || !sameSource(messages[group.Index:group.Index+len(group.Messages)], group.Messages) {
							t.Fatal("returned overlapping or stale group")
						}
						end = group.Index + len(group.Messages)
						source += EstimateMessagesTokens(group.Messages) - model.EstimateTokens(group.Messages[0].Text())
						output.Observations = append(output.Observations, ToolObservation{ID: group.Index, Text: "exact evidence"})
					}
					if source != p.SourceTokens || p.ExpectedGain < q.MinimumGain {
						t.Fatalf("source=%d want=%d gain=%d", p.SourceTokens, source, p.ExpectedGain)
					}
					pending := NewTextMessage(model.RoleUser, "new pending correction")
					current := append(append([]Message(nil), messages...), pending)
					applied, err := p.Apply(current, "prior facts", output)
					if err != nil {
						t.Fatal(err)
					}
					if !reflect.DeepEqual(applied.Messages[len(applied.Messages)-1], pending) {
						t.Fatal("new suffix changed")
					}
					declared := make(map[string]bool)
					for _, m := range applied.Messages {
						for _, call := range m.ToolCalls {
							declared[call.ID] = true
						}
						if m.Role == model.RoleTool && !declared[m.ToolCallID] {
							t.Fatal("orphan tool result after replacement")
						}
					}
				})
			}
		}
	}
}

func FuzzCompactPlanKeepsRequestAndReplacementBoundaries(f *testing.F) {
	f.Add([]byte{1, 10, 3, 20, 5, 1, 2, 8})
	f.Add([]byte{9, 0, 255, 60, 80, 2, 7, 9, 4})
	f.Add([]byte{0, 1, 0, 0, 0})
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) < 5 {
			return
		}
		value := func(index int) int { return int(data[index%len(data)]) }
		messages := []Message{consumedMessage(model.RoleUser, "prior request"), consumedMessage(model.RoleAssistant, compactSizedText(100+value(0)*25))}
		for i := 0; i < 1+value(1)%8; i++ {
			if i%2 == 0 {
				messages = append(messages, consumedMessage(model.RoleUser, fmt.Sprintf("request-%d", i)))
			}
			group := compactToolBatch(1, 200+value(i+2)*20, 0)[1:3]
			group[0].ToolCalls[0].ID = fmt.Sprintf("call-%d", i)
			group[1].ToolCallID = group[0].ToolCalls[0].ID
			messages = append(messages, group...)
			messages = append(messages, consumedMessage(model.RoleAssistant, compactSizedText(10+value(i+3)*5)))
		}
		messages = append(messages, consumedMessage(model.RoleAssistant, compactSizedText(1000+value(2)*10)), NewTextMessage(model.RoleUser, "pending correction"))
		q := CompactOptions{Budget: 30000, TargetGain: 1 + value(3)*100, MinimumGain: 1, InputLimit: 4000 + value(4)*200, OutputLimit: 64 + value(0)*32, Coefficient: 0.5 + float64(value(1)%11)/10}
		summary := compactSizedText(value(2) * 3)
		p, err := PlanCompact(messages, summary, q)
		if err != nil {
			t.Fatal(err)
		}
		if p == nil {
			return
		}
		if model.ApplyCoefficient(compactRequestTokens(t, p), q.Coefficient)+p.OutputBudget > q.InputLimit {
			t.Fatal("returned plan exceeds serialized request capacity")
		}
		end := len(p.Prefix)
		out := CompactOutput{Summary: "updated facts"}
		for _, group := range p.Groups {
			if group.Index < end || group.Index+len(group.Messages) > len(messages) {
				t.Fatal("overlapping or out-of-bounds tool source")
			}
			end = group.Index + len(group.Messages)
			out.Observations = append(out.Observations, ToolObservation{ID: group.Index, Text: "exact evidence"})
		}
		applied, err := p.Apply(messages, summary, out)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(applied.Messages[len(applied.Messages)-1], messages[len(messages)-1]) {
			t.Fatal("unconsumed correction changed")
		}
		declared := make(map[string]bool)
		for _, m := range applied.Messages {
			for _, call := range m.ToolCalls {
				declared[call.ID] = true
			}
			if m.Role == model.RoleTool && !declared[m.ToolCallID] {
				t.Fatal("orphan tool result after applying plan")
			}
		}
	})
}
