package whatsapp

import (
	"context"
	"fmt"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"google.golang.org/protobuf/proto"
)

// SendImage uploads an image (PNG/JPEG) to WhatsApp media servers and sends it as an ImageMessage.
func (c *Client) SendImage(
	ctx context.Context,
	chatJID types.JID,
	imageBytes []byte,
	mimeType string,
	caption string,
	replyToID string,
	replyToSender string,
	quotedText string,
) (types.MessageID, error) {
	if c == nil || c.WAClient == nil {
		return "", fmt.Errorf("whatsapp client not initialized")
	}

	if mimeType == "" {
		mimeType = "image/png"
	}

	// 1. Upload image to WhatsApp media server
	uploadResp, err := c.WAClient.Upload(ctx, imageBytes, whatsmeow.MediaImage)
	if err != nil {
		return "", fmt.Errorf("failed to upload image to whatsapp: %w", err)
	}

	// 2. Build ImageMessage
	imgMsg := &waE2E.ImageMessage{
		URL:           proto.String(uploadResp.URL),
		DirectPath:    proto.String(uploadResp.DirectPath),
		MediaKey:      uploadResp.MediaKey,
		Mimetype:      proto.String(mimeType),
		FileEncSHA256: uploadResp.FileEncSHA256,
		FileSHA256:    uploadResp.FileSHA256,
		FileLength:    proto.Uint64(uint64(len(imageBytes))),
	}
	if caption != "" {
		imgMsg.Caption = proto.String(caption)
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
		imgMsg.ContextInfo = ctxInfo
	}

	msgProto := &waE2E.Message{
		ImageMessage: imgMsg,
	}

	resp, err := c.WAClient.SendMessage(ctx, chatJID, msgProto)
	if err != nil {
		return "", fmt.Errorf("failed to send WhatsApp image: %w", err)
	}

	return resp.ID, nil
}
