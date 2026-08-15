package whatsapp

import (
	"go.mau.fi/whatsmeow/proto/waE2E"
	"google.golang.org/protobuf/proto"
)

// ExtractTextAndReply extracts the raw text/caption and reply context from a WhatsApp message.
func ExtractTextAndReply(msg *waE2E.Message) (text string, isReply bool, repliedID string, repliedSender string, repliedText string) {
	if msg == nil {
		return "", false, "", "", ""
	}

	var ctxInfo *waE2E.ContextInfo

	switch {
	case msg.Conversation != nil:
		text = msg.GetConversation()

	case msg.ExtendedTextMessage != nil:
		text = msg.GetExtendedTextMessage().GetText()
		ctxInfo = msg.GetExtendedTextMessage().GetContextInfo()

	case msg.ImageMessage != nil:
		if caption := msg.GetImageMessage().GetCaption(); caption != "" {
			text = caption
		} else {
			text = "[Image]"
		}
		ctxInfo = msg.GetImageMessage().GetContextInfo()

	case msg.VideoMessage != nil:
		if caption := msg.GetVideoMessage().GetCaption(); caption != "" {
			text = caption
		} else {
			text = "[Video]"
		}
		ctxInfo = msg.GetVideoMessage().GetContextInfo()

	case msg.DocumentMessage != nil:
		if caption := msg.GetDocumentMessage().GetCaption(); caption != "" {
			text = caption
		} else if title := msg.GetDocumentMessage().GetTitle(); title != "" {
			text = "[Document: " + title + "]"
		} else {
			text = "[Document]"
		}
		ctxInfo = msg.GetDocumentMessage().GetContextInfo()

	case msg.AudioMessage != nil:
		if msg.GetAudioMessage().GetPTT() {
			text = "[Voice Note]"
		} else {
			text = "[Audio]"
		}
		ctxInfo = msg.GetAudioMessage().GetContextInfo()

	case msg.StickerMessage != nil:
		text = "[Sticker]"
		ctxInfo = msg.GetStickerMessage().GetContextInfo()

	case msg.ContactMessage != nil:
		text = "[Contact: " + msg.GetContactMessage().GetDisplayName() + "]"
		ctxInfo = msg.GetContactMessage().GetContextInfo()

	case msg.LocationMessage != nil:
		text = "[Location]"
		ctxInfo = msg.GetLocationMessage().GetContextInfo()

	case msg.PollCreationMessage != nil || msg.PollCreationMessageV2 != nil || msg.PollCreationMessageV3 != nil:
		text = "[Poll]"

	case msg.ReactionMessage != nil:
		text = "[Reaction: " + msg.GetReactionMessage().GetText() + "]"

	default:
		text = "[Media]"
	}

	// Extract reply context if present
	if ctxInfo != nil && ctxInfo.StanzaID != nil && *ctxInfo.StanzaID != "" {
		isReply = true
		repliedID = ctxInfo.GetStanzaID()
		repliedSender = ctxInfo.GetParticipant()

		if quotedMsg := ctxInfo.GetQuotedMessage(); quotedMsg != nil {
			repliedText, _, _, _, _ = ExtractTextAndReply(quotedMsg)
		}
	}

	return text, isReply, repliedID, repliedSender, repliedText
}

// BuildReplyMessage creates a message proto quoting a target message ID and participant.
func BuildReplyMessage(text string, replyToID string, replyToSender string) *waE2E.Message {
	msg := &waE2E.Message{
		ExtendedTextMessage: &waE2E.ExtendedTextMessage{
			Text: proto.String(text),
		},
	}

	if replyToID != "" {
		ctxInfo := &waE2E.ContextInfo{
			StanzaID: proto.String(replyToID),
		}
		if replyToSender != "" {
			ctxInfo.Participant = proto.String(replyToSender)
		}
		msg.ExtendedTextMessage.ContextInfo = ctxInfo
	}

	return msg
}
