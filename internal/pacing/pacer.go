package pacing

import (
	"math/rand"
	"time"
	"unicode/utf8"
)

// HumanPacer calculates natural human-like reading delays, typing durations,
// and smart quote-reply decisions.
type HumanPacer struct{}

// NewHumanPacer creates a new HumanPacer instance.
func NewHumanPacer() *HumanPacer {
	return &HumanPacer{}
}

func clampDuration(d, minD, maxD time.Duration) time.Duration {
	if d < minD {
		return minD
	}
	if d > maxD {
		return maxD
	}
	return d
}

// ComputeReadDelay returns clamp(350ms + 14ms * utf8.RuneCountInString(incomingText), 400ms, 1800ms) with small jitter.
func (p *HumanPacer) ComputeReadDelay(incomingText string) time.Duration {
	return ComputeReadDelay(incomingText)
}

// ComputeReadDelay returns clamp(350ms + 14ms * utf8.RuneCountInString(incomingText), 400ms, 1800ms) with small jitter.
func ComputeReadDelay(incomingText string) time.Duration {
	runes := utf8.RuneCountInString(incomingText)
	base := 350*time.Millisecond + time.Duration(14*runes)*time.Millisecond
	// Add small jitter in [-15ms, +15ms] and clamp strictly to [400ms, 1800ms]
	jitterMs := rand.Intn(31) - 15
	withJitter := base + time.Duration(jitterMs)*time.Millisecond
	return clampDuration(withJitter, 400*time.Millisecond, 1800*time.Millisecond)
}

// ComputeTypingDuration returns clamp(38ms * utf8.RuneCountInString(bubbleText), 750ms, 4200ms).
func (p *HumanPacer) ComputeTypingDuration(bubbleText string) time.Duration {
	return ComputeTypingDuration(bubbleText)
}

// ComputeTypingDuration returns clamp(38ms * utf8.RuneCountInString(bubbleText), 750ms, 4200ms).
func ComputeTypingDuration(bubbleText string) time.Duration {
	runes := utf8.RuneCountInString(bubbleText)
	dur := time.Duration(38*runes) * time.Millisecond
	return clampDuration(dur, 750*time.Millisecond, 4200*time.Millisecond)
}

// ShouldQuoteReply determines whether Nova should use a quoted reply (SendReply)
// or an unquoted natural message (SendText).
func (p *HumanPacer) ShouldQuoteReply(isGroup bool, isExplicitReplyToOlder bool, messagesSinceTarget int) bool {
	return ShouldQuoteReply(isGroup, isExplicitReplyToOlder, messagesSinceTarget)
}

// ShouldQuoteReply determines whether Nova should use a quoted reply (SendReply)
// or an unquoted natural message (SendText).
func ShouldQuoteReply(isGroup bool, isExplicitReplyToOlder bool, messagesSinceTarget int) bool {
	if !isGroup {
		// In private 1-on-1 chats, only quote if messagesSinceTarget > 1 or isExplicitReplyToOlder.
		// Immediate back-and-forth (messagesSinceTarget == 0) is unquoted for natural conversation.
		return isExplicitReplyToOlder || messagesSinceTarget > 1
	}
	// In group chats, quote if messagesSinceTarget > 0 or isExplicitReplyToOlder (default true in groups).
	if isExplicitReplyToOlder || messagesSinceTarget > 0 {
		return true
	}
	return true
}
