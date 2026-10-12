package pacing

import (
	"sync"
	"testing"
	"time"
)

func TestTurnCoalescer_MultiMessageBurst(t *testing.T) {
	coalescer := NewTurnCoalescer(50*time.Millisecond, 40*time.Millisecond, 250*time.Millisecond)

	var mu sync.Mutex
	var flushed [][]CoalescedItem
	done := make(chan struct{}, 1)

	onFlush := func(chatID, senderID string, merged []CoalescedItem) {
		mu.Lock()
		flushed = append(flushed, merged)
		mu.Unlock()
		select {
		case done <- struct{}{}:
		default:
		}
	}

	chatID := "201000000001@s.whatsapp.net"
	senderID := "201000000001@s.whatsapp.net"

	coalescer.Enqueue(chatID, senderID, CoalescedItem{MessageID: "1", Text: "يا نوفا"}, onFlush)
	time.Sleep(15 * time.Millisecond)
	coalescer.Enqueue(chatID, senderID, CoalescedItem{MessageID: "2", Text: "عايز اسألك على حاجة"}, onFlush)
	time.Sleep(15 * time.Millisecond)
	coalescer.Enqueue(chatID, senderID, CoalescedItem{MessageID: "3", Text: "بخصوص الكود ده"}, onFlush)

	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		t.Fatalf("timed out waiting for coalescer flush")
	}

	mu.Lock()
	defer mu.Unlock()
	if len(flushed) != 1 {
		t.Fatalf("expected exactly 1 flush call, got %d", len(flushed))
	}
	if len(flushed[0]) != 3 {
		t.Fatalf("expected 3 merged items, got %d", len(flushed[0]))
	}

	combined := CombineTexts(flushed[0])
	expected := "يا نوفا\nعايز اسألك على حاجة\nبخصوص الكود ده"
	if combined != expected {
		t.Errorf("CombineTexts mismatch: got %q, want %q", combined, expected)
	}
}

func TestTurnCoalescer_NotifyTypingExtension(t *testing.T) {
	coalescer := NewTurnCoalescer(50*time.Millisecond, 70*time.Millisecond, 300*time.Millisecond)

	flushTimeCh := make(chan time.Time, 1)
	start := time.Now()

	chatID := "group1@g.us"
	senderID := "user1@s.whatsapp.net"

	coalescer.Enqueue(chatID, senderID, CoalescedItem{MessageID: "m1", Text: "يا نوفا ثواني"}, func(c, s string, merged []CoalescedItem) {
		flushTimeCh <- time.Now()
	})

	time.Sleep(30 * time.Millisecond)
	// Notify typing at 30ms -> extends deadline by 70ms (so ~100ms total from start instead of 50ms)
	coalescer.NotifyTyping(chatID, senderID)

	select {
	case flushedAt := <-flushTimeCh:
		elapsed := flushedAt.Sub(start)
		if elapsed < 65*time.Millisecond {
			t.Errorf("expected typing notification to extend flush beyond 65ms, flushed after %v", elapsed)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatalf("timed out waiting for flush after NotifyTyping")
	}
}

func TestTurnCoalescer_MaxWaitUpperBound(t *testing.T) {
	// baseWindow = 80ms, typingExtension = 80ms, maxWait = 180ms
	coalescer := NewTurnCoalescer(80*time.Millisecond, 80*time.Millisecond, 180*time.Millisecond)

	flushCh := make(chan []CoalescedItem, 2)
	start := time.Now()

	chatID := "chat_cap@g.us"
	senderID := "sender_cap@s.whatsapp.net"

	onFlush := func(c, s string, merged []CoalescedItem) {
		flushCh <- merged
	}

	coalescer.Enqueue(chatID, senderID, CoalescedItem{MessageID: "1", Text: "أول رسالة"}, onFlush)
	time.Sleep(30 * time.Millisecond)
	coalescer.Enqueue(chatID, senderID, CoalescedItem{MessageID: "2", Text: "تاني رسالة"}, onFlush)
	time.Sleep(30 * time.Millisecond)
	coalescer.NotifyTyping(chatID, senderID)
	coalescer.Enqueue(chatID, senderID, CoalescedItem{MessageID: "3", Text: "تالت رسالة"}, onFlush)

	select {
	case items := <-flushCh:
		elapsed := time.Since(start)
		if elapsed > 300*time.Millisecond {
			t.Errorf("expected maxWait cap (~180ms) to force flush promptly, took %v", elapsed)
		}
		if len(items) != 3 {
			t.Errorf("expected 3 items in capped flush, got %d", len(items))
		}
	case <-time.After(600 * time.Millisecond):
		t.Fatalf("timed out waiting for capped flush")
	}
}
