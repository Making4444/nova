package trigger

import (
	"strings"
	"sync"
	"time"
)

// TriggerEvalParams encapsulates all signals needed to decide whether an incoming
// WhatsApp message should trigger Nova while protecting the owner's personal account.
type TriggerEvalParams struct {
	ChatID           string
	SenderID         string
	IsGroup          bool
	IsFromMe         bool
	MessageText      string
	IsReply          bool
	RepliedToText    string
	RepliedToNovaMsg bool // TRUE if and only if the quoted MessageID was sent by Nova
	MentionedJIDs    []string
	BotJID           string
	BotLID           string
}

// TriggerReason describes why a message triggered (or did not trigger) Nova.
type TriggerReason string

const (
	ReasonNone           TriggerReason = ""
	ReasonExplicitWake   TriggerReason = "explicit_wake"
	ReasonRepliedToNova  TriggerReason = "replied_to_nova"
	ReasonGroupMention   TriggerReason = "group_mention"
	ReasonAttentionLease TriggerReason = "attention_lease"
)

// TriggerDecision holds the outcome of AttentionGate.Evaluate.
type TriggerDecision struct {
	Matched     bool
	Reason      TriggerReason
	IsLeaseTurn bool
}

type leaseEntry struct {
	expiresAt     time.Time
	allowFromMe   bool
}

// AttentionGate manages per-(chatID, senderID) conversational attention leases
// and enforces strict personal-account safety invariants in 1-on-1 DMs and groups.
type AttentionGate struct {
	mu          sync.Mutex
	leaseTTL    time.Duration
	leases      map[string]leaseEntry
	fromMeWoken map[string]bool
	nowFunc     func() time.Time
}

// NewAttentionGate creates an AttentionGate with the given lease TTL (default 90s).
func NewAttentionGate(leaseTTL time.Duration) *AttentionGate {
	if leaseTTL <= 0 {
		leaseTTL = 90 * time.Second
	}
	return &AttentionGate{
		leaseTTL:    leaseTTL,
		leases:      make(map[string]leaseEntry),
		fromMeWoken: make(map[string]bool),
		nowFunc:     time.Now,
	}
}

// SetNowFunc overrides the time source for deterministic testing.
func (g *AttentionGate) SetNowFunc(fn func() time.Time) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if fn != nil {
		g.nowFunc = fn
	}
}

func leaseKey(chatID, senderID string) string {
	return chatID + "|" + senderID
}

func cleanJIDUser(jid string) string {
	jid = strings.TrimSpace(jid)
	if jid == "" {
		return ""
	}
	jid = strings.TrimSuffix(jid, "@s.whatsapp.net")
	jid = strings.TrimSuffix(jid, "@lid")
	if idx := strings.IndexByte(jid, ':'); idx >= 0 {
		jid = jid[:idx]
	}
	if idx := strings.IndexByte(jid, '@'); idx >= 0 {
		jid = jid[:idx]
	}
	return jid
}

func isBotMentionedInGroup(mentionedJIDs []string, botJID, botLID string) bool {
	if len(mentionedJIDs) == 0 {
		return false
	}
	cleanBotJID := cleanJIDUser(botJID)
	cleanBotLID := cleanJIDUser(botLID)

	for _, m := range mentionedJIDs {
		if strings.Contains(m, "201202172699") {
			return true
		}
		if cleanBotJID != "" && strings.Contains(m, cleanBotJID) {
			return true
		}
		if cleanBotLID != "" && strings.Contains(m, cleanBotLID) {
			return true
		}
	}
	return false
}

// Evaluate determines whether an incoming message should trigger Nova.
// CRITICAL INVARIANT: Un-triggered 1-on-1 DMs and replies to the human owner's personal
// messages (RepliedToNovaMsg == false) NEVER match unless an explicit trigger or active lease exists.
func (g *AttentionGate) Evaluate(params TriggerEvalParams) TriggerDecision {
	key := leaseKey(params.ChatID, params.SenderID)

	// 1. Explicit wake word in MessageText
	if ContainsTrigger(params.MessageText) {
		if g != nil && params.IsFromMe && params.ChatID != "" && params.SenderID != "" {
			g.mu.Lock()
			g.fromMeWoken[key] = true
			g.mu.Unlock()
		}
		return TriggerDecision{
			Matched:     true,
			Reason:      ReasonExplicitWake,
			IsLeaseTurn: false,
		}
	}

	// 2. Direct reply to a message specifically sent by Nova
	if params.IsReply && params.RepliedToNovaMsg {
		if g != nil && params.IsFromMe && params.ChatID != "" && params.SenderID != "" {
			g.mu.Lock()
			g.fromMeWoken[key] = true
			g.mu.Unlock()
		}
		return TriggerDecision{
			Matched:     true,
			Reason:      ReasonRepliedToNova,
			IsLeaseTurn: false,
		}
	}

	// 3. Group-only mentions (@mention of BotJID/BotLID/201202172699 or reply to text containing wake trigger)
	if params.IsGroup {
		if isBotMentionedInGroup(params.MentionedJIDs, params.BotJID, params.BotLID) ||
			(params.IsReply && ContainsTrigger(params.RepliedToText)) {
			if g != nil && params.IsFromMe && params.ChatID != "" && params.SenderID != "" {
				g.mu.Lock()
				g.fromMeWoken[key] = true
				g.mu.Unlock()
			}
			return TriggerDecision{
				Matched:     true,
				Reason:      ReasonGroupMention,
				IsLeaseTurn: false,
			}
		}
	}

	// 4. Active Attention Lease for (chatID, senderID)
	if g != nil && g.HasActiveLease(params.ChatID, params.SenderID, params.IsFromMe) {
		return TriggerDecision{
			Matched:     true,
			Reason:      ReasonAttentionLease,
			IsLeaseTurn: true,
		}
	}

	// 5. Otherwise do not trigger
	return TriggerDecision{
		Matched:     false,
		Reason:      ReasonNone,
		IsLeaseTurn: false,
	}
}

// GrantLease grants or refreshes an attention lease for (chatID, senderID).
func (g *AttentionGate) GrantLease(chatID, senderID string, isFromMe ...bool) {
	if g == nil || chatID == "" || senderID == "" {
		return
	}
	key := leaseKey(chatID, senderID)
	g.mu.Lock()
	defer g.mu.Unlock()

	allowFromMe := g.fromMeWoken[key]
	if len(isFromMe) > 0 && isFromMe[0] {
		allowFromMe = true
	}

	g.leases[key] = leaseEntry{
		expiresAt:   g.nowFunc().Add(g.leaseTTL),
		allowFromMe: allowFromMe,
	}
}

// RevokeLease immediately terminates any active attention lease for (chatID, senderID).
func (g *AttentionGate) RevokeLease(chatID, senderID string) {
	if g == nil || chatID == "" || senderID == "" {
		return
	}
	key := leaseKey(chatID, senderID)
	g.mu.Lock()
	defer g.mu.Unlock()
	delete(g.leases, key)
	delete(g.fromMeWoken, key)
}

// HasActiveLease checks whether (chatID, senderID) currently holds an unexpired attention lease.
// Accepts an optional isFromMe boolean for personal-account self-message protection.
func (g *AttentionGate) HasActiveLease(chatID, senderID string, isFromMe ...bool) bool {
	if g == nil || chatID == "" || senderID == "" {
		return false
	}
	key := leaseKey(chatID, senderID)
	g.mu.Lock()
	defer g.mu.Unlock()

	entry, exists := g.leases[key]
	if !exists {
		return false
	}
	if !g.nowFunc().Before(entry.expiresAt) {
		delete(g.leases, key)
		delete(g.fromMeWoken, key)
		return false
	}
	if len(isFromMe) > 0 && isFromMe[0] && !entry.allowFromMe {
		return false
	}
	return true
}
