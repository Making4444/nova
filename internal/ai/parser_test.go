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
	// 1. Invalid JSON
	_, err := ParseResponse("Not a json string at all")
	if err == nil {
		t.Errorf("expected error for non-json string")
	}

	// 2. should_reply: true but missing reply_text
	rawNoText := `{"should_reply": true, "reply_text": "", "reply_to_message_id": "M1"}`
	_, err = ParseResponse(rawNoText)
	if err == nil {
		t.Errorf("expected error when should_reply is true but reply_text is empty")
	}
}
