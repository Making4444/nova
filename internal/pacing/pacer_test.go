package pacing

import (
	"strings"
	"testing"
	"time"
)

func TestComputeReadDelay(t *testing.T) {
	pacer := NewHumanPacer()

	shortDelay := pacer.ComputeReadDelay("اهلا")
	if shortDelay < 400*time.Millisecond || shortDelay > 1800*time.Millisecond {
		t.Errorf("short read delay out of bounds [400ms, 1800ms]: %v", shortDelay)
	}

	medText := strings.Repeat("كلام ", 10) // 50 runes -> ~1050ms
	medDelay := pacer.ComputeReadDelay(medText)
	if medDelay < 900*time.Millisecond || medDelay > 1200*time.Millisecond {
		t.Errorf("expected medium read delay around 1050ms, got %v", medDelay)
	}

	longText := strings.Repeat("رسالة طويلة جدا ", 20)
	longDelay := pacer.ComputeReadDelay(longText)
	if longDelay != 1800*time.Millisecond && (longDelay < 1750*time.Millisecond || longDelay > 1800*time.Millisecond) {
		t.Errorf("expected long read delay clamped near 1800ms, got %v", longDelay)
	}
}

func TestComputeTypingDuration(t *testing.T) {
	pacer := NewHumanPacer()

	if d := pacer.ComputeTypingDuration("تمام"); d != 750*time.Millisecond {
		t.Errorf("expected minimum clamp 750ms for short bubble, got %v", d)
	}

	medBubble := strings.Repeat("أ", 40) // 40 * 38ms = 1520ms
	if d := pacer.ComputeTypingDuration(medBubble); d != 1520*time.Millisecond {
		t.Errorf("expected 1520ms typing duration for 40 runes, got %v", d)
	}

	longBubble := strings.Repeat("أ", 200) // 200 * 38ms = 7600ms -> clamped to 4200ms
	if d := pacer.ComputeTypingDuration(longBubble); d != 4200*time.Millisecond {
		t.Errorf("expected max clamp 4200ms for long bubble, got %v", d)
	}
}

func TestShouldQuoteReply(t *testing.T) {
	pacer := NewHumanPacer()

	// Private 1-on-1 immediate back-and-forth -> false
	if pacer.ShouldQuoteReply(false, false, 0) {
		t.Errorf("expected ShouldQuoteReply=false in private 1-on-1 with messagesSinceTarget=0")
	}
	if pacer.ShouldQuoteReply(false, false, 1) {
		t.Errorf("expected ShouldQuoteReply=false in private 1-on-1 with messagesSinceTarget=1")
	}

	// Private 1-on-1 with older target or intervening messages -> true
	if !pacer.ShouldQuoteReply(false, true, 0) {
		t.Errorf("expected ShouldQuoteReply=true in private 1-on-1 when isExplicitReplyToOlder=true")
	}
	if !pacer.ShouldQuoteReply(false, false, 2) {
		t.Errorf("expected ShouldQuoteReply=true in private 1-on-1 when messagesSinceTarget > 1")
	}

	// Group chats -> true
	if !pacer.ShouldQuoteReply(true, false, 0) {
		t.Errorf("expected ShouldQuoteReply=true in group chat")
	}
	if !pacer.ShouldQuoteReply(true, false, 3) {
		t.Errorf("expected ShouldQuoteReply=true in group chat with intervening messages")
	}
}
