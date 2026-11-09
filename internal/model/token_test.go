package model

import (
	"strings"
	"testing"
)

func TestEstimateTokensUsesModerateNonASCIIWeight(t *testing.T) {
	if got, want := EstimateTokens("hello world"), 3; got != want {
		t.Fatalf("EstimateTokens(ascii) = %d, want %d", got, want)
	}
	if got, want := EstimateTokens("你好世界"), 5; got != want {
		t.Fatalf("EstimateTokens(cjk) = %d, want %d", got, want)
	}
}

func TestEstimateTokensPreservesCharacterWeights(t *testing.T) {
	for _, tt := range []struct {
		name string
		text string
		want int
	}{
		{name: "empty"},
		{name: "ASCII boundary", text: "\x00\x7fAB", want: 2},
		{name: "non ASCII boundary", text: "\u0080", want: 2},
		{name: "mixed scripts", text: "abcd汉かなé😀", want: 7},
		{name: "combining mark", text: "e\u0301", want: 2},
		{name: "invalid UTF8", text: "\xff\xfeabcd", want: 4},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := EstimateTokens(tt.text); got != tt.want {
				t.Fatalf("EstimateTokens() = %d, want %d", got, tt.want)
			}
		})
	}
}

func BenchmarkEstimateTokensLargeText(b *testing.B) {
	for _, tt := range []struct{ name, text string }{
		{name: "ASCII", text: strings.Repeat("source evidence line; ", 200000)},
		{name: "mixed", text: strings.Repeat("源码 evidence; ", 200000)},
	} {
		b.Run(tt.name, func(b *testing.B) {
			b.SetBytes(int64(len(tt.text)))
			b.ReportAllocs()
			for b.Loop() {
				EstimateTokens(tt.text)
			}
		})
	}
}

func TestEstimateMessagesTokensCountsImages(t *testing.T) {
	msgs := []Message{
		NewTextMessage(RoleUser, "hello"),
		// 图片消息不带文本，差值即为纯图片 token。
		{Role: RoleUser, Content: []ContentBlock{{Type: ContentImage, Media: &MediaRef{Kind: MediaPath, Path: "/a/shot.png"}}}},
	}
	base := EstimateMessagesTokens(msgs[:1])
	total := EstimateMessagesTokens(msgs)
	if got, want := total-base, imageTokensPerImage; got != want {
		t.Fatalf("image tokens = %d, want %d", got, want)
	}
}

func TestEstimateMessagesTokensSkipsEmptyMedia(t *testing.T) {
	msgs := []Message{{Role: RoleUser, Content: []ContentBlock{{Type: ContentImage, Media: nil}}, TextContent: "x"}}
	if got, want := EstimateMessagesTokens(msgs), 1; got != want {
		t.Fatalf("EstimateMessagesTokens(empty media) = %d, want %d", got, want)
	}
}
