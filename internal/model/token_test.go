package model

import "testing"

func TestEstimateTokensUsesModerateNonASCIIWeight(t *testing.T) {
	if got, want := EstimateTokens("hello world"), 3; got != want {
		t.Fatalf("EstimateTokens(ascii) = %d, want %d", got, want)
	}
	if got, want := EstimateTokens("你好世界"), 5; got != want {
		t.Fatalf("EstimateTokens(cjk) = %d, want %d", got, want)
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
