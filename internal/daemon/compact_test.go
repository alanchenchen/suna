package daemon

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/alanchenchen/suna/internal/memory"
	"github.com/alanchenchen/suna/internal/protocol"
	"github.com/alanchenchen/suna/internal/runner"
)

func TestCompactStatePayloadPreservesKindsAndOverallResult(t *testing.T) {
	for _, kind := range []memory.CompactKind{memory.CompactTools, memory.CompactHistory, memory.CompactMixed} {
		t.Run(string(kind), func(t *testing.T) {
			for _, state := range []string{"preparing", "ready", "applied", "failed"} {
				value := compactStatePayload("session", runner.CompactState{ID: "candidate", Kind: kind, State: state, Wait: "manual", PrepareElapsed: 2 * time.Second, WaitElapsed: time.Second, BeforeTokens: 10000, AfterTokens: 6000, ContextWindow: 20000})
				if value.Kind != protocol.CompactKind(kind) || value.CompactID != "candidate" || value.SessionID != "session" || value.Wait != "manual" || value.PrepareElapsedMs != 2000 || value.WaitElapsedMs != 1000 {
					t.Fatalf("payload=%+v", value)
				}
				if (value.Result != nil) != (state == "applied") {
					t.Fatalf("payload=%+v", value)
				}
				data, err := json.Marshal(value)
				if err != nil {
					t.Fatal(err)
				}
				var decoded protocol.CompactState
				if err = json.Unmarshal(data, &decoded); err != nil {
					t.Fatal(err)
				}
				if decoded.Kind != value.Kind || decoded.CompactID != value.CompactID {
					t.Fatalf("decoded=%+v", decoded)
				}
				if state == "applied" && (decoded.Result.BeforeTokens != 10000 || decoded.Result.AfterTokens != 6000) {
					t.Fatalf("result=%+v", decoded.Result)
				}
			}
		})
	}
}
