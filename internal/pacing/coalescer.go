package pacing

import (
	"strings"
	"sync"
	"time"
)

// CoalescedItem represents a single incoming message bubble buffered during a user turn.
type CoalescedItem struct {
	MessageID     string
	Text          string
	IsReply       bool
	RepliedMsgID  string
	RepliedSender string
	RepliedText   string
	IsVoice       bool
	Timestamp     time.Time
	RawEvent      interface{}
}

type pendingBucket struct {
	chatID           string
	senderID         string
	items            []CoalescedItem
	firstEnqueueTime time.Time
	deadline         time.Time
	timer            *time.Timer
	seq              uint64
	onFlush          func(chatID, senderID string, merged []CoalescedItem)
}

// TurnCoalescer buffers rapid consecutive multi-bubble messages from a user
// into a single unified turn, extending the debounce window when typing presence arrives.
type TurnCoalescer struct {
	mu              sync.Mutex
	baseWindow      time.Duration
	typingExtension time.Duration
	maxWait         time.Duration
	buckets         map[string]*pendingBucket
}

// NewTurnCoalescer creates a new TurnCoalescer with configurable timing windows.
func NewTurnCoalescer(baseWindow, typingExtension, maxWait time.Duration) *TurnCoalescer {
	if baseWindow <= 0 {
		baseWindow = 1800 * time.Millisecond
	}
	if typingExtension <= 0 {
		typingExtension = 1500 * time.Millisecond
	}
	if maxWait <= 0 {
		maxWait = 4500 * time.Millisecond
	}
	return &TurnCoalescer{
		baseWindow:      baseWindow,
		typingExtension: typingExtension,
		maxWait:         maxWait,
		buckets:         make(map[string]*pendingBucket),
	}
}

func bucketKey(chatID, senderID string) string {
	return chatID + "|" + senderID
}

// Enqueue adds a message bubble to the user's pending turn buffer and resets the debounce timer
// up to firstEnqueueTime + maxWait.
func (c *TurnCoalescer) Enqueue(chatID, senderID string, item CoalescedItem, onFlush func(chatID, senderID string, merged []CoalescedItem)) {
	key := bucketKey(chatID, senderID)
	now := time.Now()

	c.mu.Lock()
	b, exists := c.buckets[key]
	if !exists {
		delay := c.baseWindow
		if delay > c.maxWait {
			delay = c.maxWait
		}
		b = &pendingBucket{
			chatID:           chatID,
			senderID:         senderID,
			items:            []CoalescedItem{item},
			firstEnqueueTime: now,
			deadline:         now.Add(delay),
			seq:              1,
			onFlush:          onFlush,
		}
		seq := b.seq
		b.timer = time.AfterFunc(delay, func() {
			c.flushIfSeq(key, seq)
		})
		c.buckets[key] = b
		c.mu.Unlock()
		return
	}

	// Bucket already exists: append item and reset window capped at firstEnqueueTime + maxWait
	b.items = append(b.items, item)
	if onFlush != nil {
		b.onFlush = onFlush
	}

	maxDeadline := b.firstEnqueueTime.Add(c.maxWait)
	desiredDeadline := now.Add(c.baseWindow)
	if desiredDeadline.After(maxDeadline) {
		desiredDeadline = maxDeadline
	}

	remaining := desiredDeadline.Sub(now)
	if remaining <= 0 {
		if b.timer != nil {
			b.timer.Stop()
		}
		delete(c.buckets, key)
		itemsCopy := make([]CoalescedItem, len(b.items))
		copy(itemsCopy, b.items)
		flushFn := b.onFlush
		c.mu.Unlock()
		if flushFn != nil {
			go flushFn(chatID, senderID, itemsCopy)
		}
		return
	}

	if b.timer != nil {
		b.timer.Stop()
	}
	b.seq++
	seq := b.seq
	b.deadline = desiredDeadline
	b.timer = time.AfterFunc(remaining, func() {
		c.flushIfSeq(key, seq)
	})
	c.mu.Unlock()
}

// NotifyTyping extends the active debounce timer for (chatID, senderID) by typingExtension,
// capped at firstEnqueueTime + maxWait.
func (c *TurnCoalescer) NotifyTyping(chatID, senderID string) {
	key := bucketKey(chatID, senderID)
	now := time.Now()

	c.mu.Lock()
	defer c.mu.Unlock()

	b, exists := c.buckets[key]
	if !exists {
		return
	}

	maxDeadline := b.firstEnqueueTime.Add(c.maxWait)
	extendedDeadline := now.Add(c.typingExtension)
	// Do not shorten an already longer deadline
	if b.deadline.After(extendedDeadline) {
		extendedDeadline = b.deadline
	}
	if extendedDeadline.After(maxDeadline) {
		extendedDeadline = maxDeadline
	}

	remaining := extendedDeadline.Sub(now)
	if remaining <= 0 {
		return
	}

	if b.timer != nil {
		b.timer.Stop()
	}
	b.seq++
	seq := b.seq
	b.deadline = extendedDeadline
	b.timer = time.AfterFunc(remaining, func() {
		c.flushIfSeq(key, seq)
	})
}

// HasPending returns true if there is an active coalescing buffer for (chatID, senderID).
func (c *TurnCoalescer) HasPending(chatID, senderID string) bool {
	if c == nil {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	_, exists := c.buckets[bucketKey(chatID, senderID)]
	return exists
}

func (c *TurnCoalescer) flushIfSeq(key string, seq uint64) {
	c.mu.Lock()
	b, exists := c.buckets[key]
	if !exists || b.seq != seq {
		c.mu.Unlock()
		return
	}
	delete(c.buckets, key)
	itemsCopy := make([]CoalescedItem, len(b.items))
	copy(itemsCopy, b.items)
	chatID := b.chatID
	senderID := b.senderID
	flushFn := b.onFlush
	c.mu.Unlock()

	if flushFn != nil {
		go flushFn(chatID, senderID, itemsCopy)
	}
}

// CombineTexts joins non-empty item texts with "\n".
func (c *TurnCoalescer) CombineTexts(items []CoalescedItem) string {
	return CombineTexts(items)
}

// CombineTexts joins non-empty item texts with "\n".
func CombineTexts(items []CoalescedItem) string {
	var parts []string
	for _, it := range items {
		t := strings.TrimSpace(it.Text)
		if t != "" {
			parts = append(parts, t)
		}
	}
	return strings.Join(parts, "\n")
}
