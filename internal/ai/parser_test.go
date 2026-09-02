package ai

import (
	"testing"
)

func TestParseResponse_ValidReply(t *testing.T) {
	raw := `{"should_reply": true, "reply_text": "ازيك يا غالي!", "reply_to_message_id": "MSG123", "memory_note": "اسمه احمد", "mood": "happy"}`

	resp, err := ParseResponse(raw)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	if !resp.ShouldReply {
		t.Errorf("expected should_reply to be true")
	}
	if resp.ReplyText == nil || *resp.ReplyText != "ازيك يا غالي!" {
		t.Errorf("expected reply_text 'ازيك يا غالي!', got %v", resp.ReplyText)
	}
	if resp.ReplyToMessageID == nil || *resp.ReplyToMessageID != "MSG123" {
		t.Errorf("expected reply_to_message_id 'MSG123', got %v", resp.ReplyToMessageID)
	}
	if resp.MemoryNote == nil || *resp.MemoryNote != "اسمه احمد" {
		t.Errorf("expected memory_note 'اسمه احمد', got %v", resp.MemoryNote)
	}
	if resp.Mood == nil || *resp.Mood != "happy" {
		t.Errorf("expected mood 'happy', got %v", resp.Mood)
	}
}

func TestParseResponse_ShouldReplyFalse(t *testing.T) {
	raw := `{"should_reply": false, "reply_text": null, "reply_to_message_id": null, "memory_note": null, "mood": null}`

	resp, err := ParseResponse(raw)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	if resp.ShouldReply {
		t.Errorf("expected should_reply to be false")
	}
}

func TestParseResponse_WithMarkdownFence(t *testing.T) {
	raw := "```json\n" +
		`{"should_reply": true, "reply_text": "هههه تمام يا باشا", "reply_to_message_id": "M1", "memory_note": null, "mood": "joking"}` +
		"\n```"

	resp, err := ParseResponse(raw)
	if err != nil {
		t.Fatalf("expected successful parse from markdown fence, got: %v", err)
	}

	if !resp.ShouldReply {
		t.Errorf("expected should_reply to be true")
	}
	if resp.ReplyText == nil || *resp.ReplyText != "هههه تمام يا باشا" {
		t.Errorf("expected correct text, got %v", resp.ReplyText)
	}
}

func TestParseResponse_WithSurroundingCommentary(t *testing.T) {
	raw := `Here is the requested output: {"should_reply": true, "reply_text": "يا هلا", "reply_to_message_id": "M2", "memory_note": null, "mood": "neutral"} Hope this helps!`

	resp, err := ParseResponse(raw)
	if err != nil {
		t.Fatalf("expected successful parse from surrounding text, got: %v", err)
	}

	if !resp.ShouldReply || resp.ReplyText == nil || *resp.ReplyText != "يا هلا" {
		t.Errorf("expected valid extracted response, got %+v", resp)
	}
}

func TestParseResponse_Invalid(t *testing.T) {
	// 1. Empty string returns error
	_, err := ParseResponse("   ")
	if err == nil {
		t.Errorf("expected error for empty string")
	}

	// 2. Direct plain text fallback
	resp, err := ParseResponse("صباح القشطة يا عمنا")
	if err != nil {
		t.Fatalf("expected direct text to be accepted as fallback, got err: %v", err)
	}
	if !resp.ShouldReply || resp.ReplyText == nil || *resp.ReplyText != "صباح القشطة يا عمنا" {
		t.Errorf("expected direct text in reply_text, got: %+v", resp)
	}
}

func TestParseResponse_WithReactionEmoji(t *testing.T) {
	raw := `{"should_reply": true, "reply_text": "ههههه عاش!", "reply_to_message_id": "M3", "memory_note": null, "mood": "joking", "reaction_emoji": "😂"}`
	resp, err := ParseResponse(raw)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	if resp.ReactionEmoji == nil || *resp.ReactionEmoji != "😂" {
		t.Errorf("expected reaction emoji '😂', got %v", resp.ReactionEmoji)
	}
}

