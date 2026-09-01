package whatsapp

import (
	"context"
	"fmt"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"google.golang.org/protobuf/proto"
)

// SendVoiceNote uploads an OGG Opus audio stream to WhatsApp and sends it as a PTT Voice Note.
func (c *Client) SendVoiceNote(
	ctx context.Context,
	chatJID types.JID,
	oggBytes []byte,
	durationSec uint32,
	replyToID string,
	replyToSender string,
	quotedText string,
) (types.MessageID, error) {
	if c == nil || c.WAClient == nil {
		return "", fmt.Errorf("whatsapp client not initialized")
	}

	// 1. Upload audio to WhatsApp media server
	uploadResp, err := c.WAClient.Upload(ctx, oggBytes, whatsmeow.MediaAudio)
	if err != nil {
		return "", fmt.Errorf("failed to upload audio to whatsapp: %w", err)
	}

	// 2. Build AudioMessage with PTT: true
	audioMsg := &waE2E.AudioMessage{
		URL:           proto.String(uploadResp.URL),
		DirectPath:    proto.String(uploadResp.DirectPath),
		MediaKey:      uploadResp.MediaKey,
		Mimetype:      proto.String("audio/ogg; codecs=opus"),
		FileEncSHA256: uploadResp.FileEncSHA256,
		FileSHA256:    uploadResp.FileSHA256,
		FileLength:    proto.Uint64(uint64(len(oggBytes))),
		PTT:           proto.Bool(true), // Push To Talk = Green Voice Note!
		Seconds:       proto.Uint32(durationSec),
	}

	// 3. Add context info for quote/reply if present
	if replyToID != "" {
		ctxInfo := &waE2E.ContextInfo{
			StanzaID: proto.String(replyToID),
		}
		if replyToSender != "" {
			ctxInfo.Participant = proto.String(replyToSender)
		}
		if quotedText != "" {
			ctxInfo.QuotedMessage = &waE2E.Message{
				Conversation: proto.String(quotedText),
			}
		}
		audioMsg.ContextInfo = ctxInfo
	}

	msgProto := &waE2E.Message{
		AudioMessage: audioMsg,
	}

	resp, err := c.WAClient.SendMessage(ctx, chatJID, msgProto)
	if err != nil {
		return "", fmt.Errorf("failed to send WhatsApp voice note: %w", err)
	}

	return resp.ID, nil
}
