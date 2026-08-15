package whatsapp

import (
	"testing"

	"go.mau.fi/whatsmeow/proto/waE2E"
	"google.golang.org/protobuf/proto"
)

func TestExtractTextAndReply(t *testing.T) {
	// 1. Nil message
	text, isReply, _, _, _ := ExtractTextAndReply(nil)
	if text != "" || isReply {
		t.Errorf("expected empty on nil message")
	}

	// 2. Simple Conversation message
	convMsg := &waE2E.Message{
		Conversation: proto.String("يا نوفا صباح الفل"),
	}
	text, isReply, _, _, _ = ExtractTextAndReply(convMsg)
	if text != "يا نوفا صباح الفل" || isReply {
		t.Errorf("expected plain conversation text, got %q (isReply=%v)", text, isReply)
	}

	// 3. ExtendedTextMessage with Quote/Reply
	quotedMsg := &waE2E.Message{
		Conversation: proto.String("سؤال سابق يا نوفا"),
	}
	extMsg := &waE2E.Message{
		ExtendedTextMessage: &waE2E.ExtendedTextMessage{
			Text: proto.String("إجابة ريبلاي"),
			ContextInfo: &waE2E.ContextInfo{
				StanzaID:      proto.String("ORIGINAL_MSG_ID"),
				Participant:   proto.String("201000000000@s.whatsapp.net"),
				QuotedMessage: quotedMsg,
			},
		},
	}
	text, isReply, repID, repSender, repText := ExtractTextAndReply(extMsg)
	if text != "إجابة ريبلاي" || !isReply {
		t.Errorf("expected reply message with text, got %q (isReply=%v)", text, isReply)
	}
	if repID != "ORIGINAL_MSG_ID" || repSender != "201000000000@s.whatsapp.net" || repText != "سؤال سابق يا نوفا" {
		t.Errorf("quoted context mismatch: repID=%q, repSender=%q, repText=%q", repID, repSender, repText)
	}

	// 4. Image with Caption
	imgMsg := &waE2E.Message{
		ImageMessage: &waE2E.ImageMessage{
			Caption: proto.String("يا نوفا بصي هنا"),
		},
	}
	text, _, _, _, _ = ExtractTextAndReply(imgMsg)
	if text != "يا نوفا بصي هنا" {
		t.Errorf("expected image caption, got %q", text)
	}

	// 5. Image without Caption
	imgNoCap := &waE2E.Message{
		ImageMessage: &waE2E.ImageMessage{},
	}
	text, _, _, _, _ = ExtractTextAndReply(imgNoCap)
	if text != "[Image]" {
		t.Errorf("expected [Image] placeholder, got %q", text)
	}

	// 6. Voice note (PTT)
	voiceMsg := &waE2E.Message{
		AudioMessage: &waE2E.AudioMessage{
			PTT: proto.Bool(true),
		},
	}
	text, _, _, _, _ = ExtractTextAndReply(voiceMsg)
	if text != "[Voice Note]" {
		t.Errorf("expected [Voice Note] placeholder, got %q", text)
	}

	// 7. Sticker
	stickerMsg := &waE2E.Message{
		StickerMessage: &waE2E.StickerMessage{},
	}
	text, _, _, _, _ = ExtractTextAndReply(stickerMsg)
	if text != "[Sticker]" {
		t.Errorf("expected [Sticker] placeholder, got %q", text)
	}
}

func TestBuildReplyMessage(t *testing.T) {
	reply := BuildReplyMessage("رد نوفا", "MSG123", "user@s.whatsapp.net")
	if reply.GetExtendedTextMessage().GetText() != "رد نوفا" {
		t.Errorf("unexpected reply text: %q", reply.GetExtendedTextMessage().GetText())
	}
	if reply.GetExtendedTextMessage().GetContextInfo().GetStanzaID() != "MSG123" {
		t.Errorf("unexpected stanza ID: %q", reply.GetExtendedTextMessage().GetContextInfo().GetStanzaID())
	}
}
