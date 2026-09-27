package trigger

import (
	"testing"
	"time"
)

func TestAttentionGate_PersonalAccountSafety_UntriggeredDM(t *testing.T) {
	gate := NewAttentionGate(90 * time.Second)

	// 1. Un-triggered 1-on-1 DM from a regular contact MUST NOT trigger Nova
	decision := gate.Evaluate(TriggerEvalParams{
		ChatID:      "201099999999@s.whatsapp.net",
		SenderID:    "201099999999@s.whatsapp.net",
		IsGroup:     false,
		IsFromMe:    false,
		MessageText: "صباح الخير يا محمد، فاضي نتكلم النهاردة؟",
		IsReply:     false,
	})
	if decision.Matched {
		t.Fatalf("CRITICAL SAFETY VIOLATION: un-triggered 1-on-1 DM matched! decision=%+v", decision)
	}
	if decision.Reason != ReasonNone {
		t.Errorf("expected ReasonNone, got %q", decision.Reason)
	}
}

func TestAttentionGate_PersonalAccountSafety_ReplyToHumanOwnerMessage(t *testing.T) {
	gate := NewAttentionGate(90 * time.Second)

	// 2. Contact in DM replying to the human owner's personal message (RepliedToNovaMsg: false) MUST NOT trigger
	decision := gate.Evaluate(TriggerEvalParams{
		ChatID:           "201099999999@s.whatsapp.net",
		SenderID:         "201099999999@s.whatsapp.net",
		IsGroup:          false,
		IsFromMe:         false,
		MessageText:      "تمام هعدي عليك الساعة 8",
		IsReply:          true,
		RepliedToText:    "عدي عليا بالليل",
		RepliedToNovaMsg: false,
	})
	if decision.Matched {
		t.Fatalf("CRITICAL SAFETY VIOLATION: reply to human owner's personal message matched! decision=%+v", decision)
	}
}

func TestAttentionGate_ReplyToNovaMessage(t *testing.T) {
	gate := NewAttentionGate(90 * time.Second)

	// 3a. Contact in DM replying to a message sent by Nova (RepliedToNovaMsg: true) -> Matched
	dmDec := gate.Evaluate(TriggerEvalParams{
		ChatID:           "201099999999@s.whatsapp.net",
		SenderID:         "201099999999@s.whatsapp.net",
		IsGroup:          false,
		IsFromMe:         false,
		MessageText:      "طب اشرحلي النقطة التانية كده",
		IsReply:          true,
		RepliedToText:    "أكيد يا صاحبي، الكود شغال بـ 3 خطوات",
		RepliedToNovaMsg: true,
	})
	if !dmDec.Matched || dmDec.Reason != ReasonRepliedToNova || dmDec.IsLeaseTurn {
		t.Errorf("expected Matched=true, Reason=ReasonRepliedToNova, IsLeaseTurn=false; got %+v", dmDec)
	}

	// 3b. Contact in Group replying to a message sent by Nova -> Matched
	grpDec := gate.Evaluate(TriggerEvalParams{
		ChatID:           "120363000000@g.us",
		SenderID:         "201088888888@s.whatsapp.net",
		IsGroup:          true,
		IsFromMe:         false,
		MessageText:      "صح عندك حق",
		IsReply:          true,
		RepliedToText:    "الماتش هيبدأ الساعة 9",
		RepliedToNovaMsg: true,
	})
	if !grpDec.Matched || grpDec.Reason != ReasonRepliedToNova {
		t.Errorf("expected group reply to Nova message to match with ReasonRepliedToNova, got %+v", grpDec)
	}
}

func TestAttentionGate_ExplicitWakeAndLeaseLifecycle(t *testing.T) {
	gate := NewAttentionGate(60 * time.Millisecond)

	chatID := "201055555555@s.whatsapp.net"
	senderID := "201055555555@s.whatsapp.net"

	// Step 1: Contact calls "يا نوفا" in DM
	wakeDec := gate.Evaluate(TriggerEvalParams{
		ChatID:      chatID,
		SenderID:    senderID,
		IsGroup:     false,
		IsFromMe:    false,
		MessageText: "يا نوفا عايز اسألك سؤال في الجو",
	})
	if !wakeDec.Matched || wakeDec.Reason != ReasonExplicitWake || wakeDec.IsLeaseTurn {
		t.Fatalf("expected explicit wake match, got %+v", wakeDec)
	}

	// Step 2: Nova replies and grants a lease to (chatID, senderID)
	gate.GrantLease(chatID, senderID)
	if !gate.HasActiveLease(chatID, senderID) {
		t.Fatalf("expected active lease after GrantLease")
	}

	// Step 3: Follow-up message within lease TTL without "يا نوفا" -> matches via ReasonAttentionLease
	followUpDec := gate.Evaluate(TriggerEvalParams{
		ChatID:      chatID,
		SenderID:    senderID,
		IsGroup:     false,
		IsFromMe:    false,
		MessageText: "طب والخميس الجاي هيكون الجو عامل ايه؟",
	})
	if !followUpDec.Matched || followUpDec.Reason != ReasonAttentionLease || !followUpDec.IsLeaseTurn {
		t.Fatalf("expected follow-up within lease to match with ReasonAttentionLease and IsLeaseTurn=true, got %+v", followUpDec)
	}

	// Step 4: Human owner typing from their own phone (IsFromMe=true) without "يا نوفا" must NOT trigger
	ownerDec := gate.Evaluate(TriggerEvalParams{
		ChatID:      chatID,
		SenderID:    senderID,
		IsGroup:     false,
		IsFromMe:    true,
		MessageText: "انا هرد عليك لما اروح",
	})
	if ownerDec.Matched {
		t.Fatalf("expected human owner's un-triggered message (IsFromMe=true) NOT to trigger lease, got %+v", ownerDec)
	}

	// Step 5: Wait for lease TTL to expire
	time.Sleep(80 * time.Millisecond)
	if gate.HasActiveLease(chatID, senderID) {
		t.Errorf("expected lease to expire after TTL")
	}

	afterExpiryDec := gate.Evaluate(TriggerEvalParams{
		ChatID:      chatID,
		SenderID:    senderID,
		IsGroup:     false,
		IsFromMe:    false,
		MessageText: "كمان سؤال معلش",
	})
	if afterExpiryDec.Matched {
		t.Errorf("expected message after lease expiry not to match, got %+v", afterExpiryDec)
	}
}

func TestAttentionGate_RevokeLeaseAndGroupMentions(t *testing.T) {
	gate := NewAttentionGate(90 * time.Second)
	chatID := "120363111111@g.us"
	senderID := "201077777777@s.whatsapp.net"
	botJID := "201202172699@s.whatsapp.net"
	botLID := "998877665544@lid"

	// Group @mention by BotLID
	mentionDec := gate.Evaluate(TriggerEvalParams{
		ChatID:        chatID,
		SenderID:      senderID,
		IsGroup:       true,
		IsFromMe:      false,
		MessageText:   "ايه رأيك في الكلام ده؟",
		MentionedJIDs: []string{botLID},
		BotJID:        botJID,
		BotLID:        botLID,
	})
	if !mentionDec.Matched || mentionDec.Reason != ReasonGroupMention {
		t.Errorf("expected group @mention to match with ReasonGroupMention, got %+v", mentionDec)
	}

	// Grant lease then revoke when AI decides should_reply=false
	gate.GrantLease(chatID, senderID)
	if !gate.HasActiveLease(chatID, senderID) {
		t.Fatalf("expected lease to be active")
	}
	gate.RevokeLease(chatID, senderID)
	if gate.HasActiveLease(chatID, senderID) {
		t.Fatalf("expected lease to be revoked")
	}

	postRevoke := gate.Evaluate(TriggerEvalParams{
		ChatID:      chatID,
		SenderID:    senderID,
		IsGroup:     true,
		IsFromMe:    false,
		MessageText: "خلاص تمام شكرا",
	})
	if postRevoke.Matched {
		t.Errorf("expected no match after RevokeLease, got %+v", postRevoke)
	}
}
