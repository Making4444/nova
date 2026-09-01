package whatsapp

import (
	"context"
	"encoding/base64"
	"fmt"
	"strings"

	"go.mau.fi/whatsmeow"
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

// ExtractDownloadableMedia finds any downloadable image in the direct or quoted message.
func ExtractDownloadableMedia(msg *waE2E.Message) (whatsmeow.DownloadableMessage, string) {
	if msg == nil {
		return nil, ""
	}

	// 1. Direct message image
	if img := msg.GetImageMessage(); img != nil {
		mime := img.GetMimetype()
		if mime == "" {
			mime = "image/jpeg"
		}
		return img, mime
	}

	// 2. Quoted message image (when user replies to an earlier image)
	var ctxInfo *waE2E.ContextInfo
	switch {
	case msg.ExtendedTextMessage != nil:
		ctxInfo = msg.GetExtendedTextMessage().GetContextInfo()
	case msg.ImageMessage != nil:
		ctxInfo = msg.GetImageMessage().GetContextInfo()
	case msg.VideoMessage != nil:
		ctxInfo = msg.GetVideoMessage().GetContextInfo()
	case msg.DocumentMessage != nil:
		ctxInfo = msg.GetDocumentMessage().GetContextInfo()
	}

	if ctxInfo != nil && ctxInfo.GetQuotedMessage() != nil {
		quoted := ctxInfo.GetQuotedMessage()
		if img := quoted.GetImageMessage(); img != nil {
			mime := img.GetMimetype()
			if mime == "" {
				mime = "image/jpeg"
			}
			return img, mime
		}
	}

	return nil, ""
}

// DownloadMediaAsBase64 downloads media (direct or from quoted reply) and formats it as a base64 Data URL.
func DownloadMediaAsBase64(ctx context.Context, cli *whatsmeow.Client, msg *waE2E.Message) (*string, error) {
	downloadable, mimeType := ExtractDownloadableMedia(msg)
	if downloadable == nil || cli == nil {
		return nil, nil
	}

	if ctx == nil {
		ctx = context.Background()
	}

	data, err := cli.Download(ctx, downloadable)
	if err != nil {
		return nil, fmt.Errorf("failed to download whatsapp media: %w", err)
	}

	// Cap at 10MB to avoid excessive token / payload sizes
	if len(data) > 10*1024*1024 {
		return nil, fmt.Errorf("media size (%d bytes) exceeds 10MB limit", len(data))
	}

	// Clean MIME type (strip params like '; codecs=...' if any)
	if idx := strings.Index(mimeType, ";"); idx != -1 {
		mimeType = strings.TrimSpace(mimeType[:idx])
	}

	dataURL := fmt.Sprintf("data:%s;base64,%s", mimeType, base64.StdEncoding.EncodeToString(data))
	return &dataURL, nil
}

// EnsureRTLFormatting ensures that messages containing Arabic text render correctly in WhatsApp
// without getting scrambled by English prefixes, numbers, or symbols.
func EnsureRTLFormatting(text string) string {
	hasArabic := false
	for _, r := range text {
		if (r >= 0x0600 && r <= 0x06FF) || (r >= 0x0750 && r <= 0x077F) || (r >= 0x08A0 && r <= 0x08FF) {
			hasArabic = true
			break
		}
	}
	if !hasArabic {
		return text
	}
	if strings.HasPrefix(text, "\u200F") {
		return text
	}
	return "\u200F" + text
}

// BuildReplyMessage creates a message proto quoting a target message ID and participant.
func BuildReplyMessage(text string, replyToID string, replyToSender string) *waE2E.Message {
	formattedText := EnsureRTLFormatting(text)
	msg := &waE2E.Message{
		ExtendedTextMessage: &waE2E.ExtendedTextMessage{
			Text: proto.String(formattedText),
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
