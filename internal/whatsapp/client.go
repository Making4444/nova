package whatsapp

import (
	"context"
	"fmt"
	"time"

	qrcode "github.com/skip2/go-qrcode"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types"
	waLog "go.mau.fi/whatsmeow/util/log"
)

// Client wraps whatsmeow.Client and provides helper methods.
type Client struct {
	WAClient       *whatsmeow.Client
	container      *sqlstore.Container
	logger         waLog.Logger
	userJID        types.JID
	sendReplyFn    func(ctx context.Context, chatJID types.JID, replyText, replyToID, replyToSender, quotedText string) (types.MessageID, error)
	sendTextFn     func(ctx context.Context, chatJID types.JID, text string) (types.MessageID, error)
	markReadFn     func(ctx context.Context, msgIDs []types.MessageID, timestamp time.Time, chatJID, senderJID types.JID) error
	sendPresenceFn func(ctx context.Context, chatJID types.JID, state types.ChatPresence, media types.ChatPresenceMedia) error
	sendReactionFn func(ctx context.Context, chatJID, senderJID types.JID, targetMsgID types.MessageID, emoji string) (types.MessageID, error)
}

// HasTransport returns true if the client has an active whatsmeow client or test transport hooks configured.
func (c *Client) HasTransport() bool {
	if c == nil {
		return false
	}
	return c.WAClient != nil || c.sendReplyFn != nil || c.sendTextFn != nil || c.markReadFn != nil || c.sendPresenceFn != nil || c.sendReactionFn != nil
}

// NewClient creates a new Client instance from the SQLite session container.
func NewClient(ctx context.Context, container *sqlstore.Container, logger waLog.Logger) (*Client, error) {
	deviceStore, err := container.GetFirstDevice(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get device from session store: %w", err)
	}

	waClient := whatsmeow.NewClient(deviceStore, logger)

	return &Client{
		WAClient:  waClient,
		container: container,
		logger:    logger,
	}, nil
}

// AddEventHandler registers an event handler function on the WhatsApp client.
func (c *Client) AddEventHandler(handler func(interface{})) uint32 {
	if c == nil || c.WAClient == nil {
		return 0
	}
	return c.WAClient.AddEventHandler(handler)
}

// Connect handles logging in (printing QR in terminal if unauthenticated) and connecting to WhatsApp.
func (c *Client) Connect(ctx context.Context) error {
	if c.WAClient.Store.ID == nil {
		// No existing login session, display QR code in terminal
		qrChan, _ := c.WAClient.GetQRChannel(ctx)
		err := c.WAClient.Connect()
		if err != nil {
			return fmt.Errorf("failed to connect to WhatsApp: %w", err)
		}

		for evt := range qrChan {
			if evt.Event == "code" {
				fmt.Println("\n=======================================================")
				fmt.Println("📲 برجاء مسح الـ QR Code التالي من تطبيق واتساب لربط البوت:")
				fmt.Println("=======================================================")

				// Render terminal QR code
				qr, err := qrcode.New(evt.Code, qrcode.Medium)
				if err == nil {
					fmt.Println(qr.ToSmallString(false))
				} else {
					fmt.Printf("QR Code string: %s\n", evt.Code)
				}
				fmt.Println("=======================================================")
			} else {
				fmt.Printf("ℹ️ حالة تسجيل الدخول: %s\n", evt.Event)
			}
		}
	} else {
		// Already logged in
		err := c.WAClient.Connect()
		if err != nil {
			return fmt.Errorf("failed to connect to WhatsApp: %w", err)
		}
		fmt.Println("✅ تم الاتصال بواتساب بنجاح باستخدام الجلسة المحفوظة.")
	}

	return nil
}

// Disconnect disconnects the WhatsApp client.
func (c *Client) Disconnect() {
	if c != nil && c.WAClient != nil {
		c.WAClient.Disconnect()
	}
}

// SendReply sends a reply message quoting the given message ID and content.
func (c *Client) SendReply(ctx context.Context, chatJID types.JID, replyText string, replyToID string, replyToSender string, quotedText string) (types.MessageID, error) {
	if c != nil && c.sendReplyFn != nil {
		return c.sendReplyFn(ctx, chatJID, replyText, replyToID, replyToSender, quotedText)
	}
	if c == nil || c.WAClient == nil {
		return "", fmt.Errorf("whatsapp client not initialized")
	}

	msgProto := BuildReplyMessage(replyText, replyToID, replyToSender, quotedText)

	resp, err := c.WAClient.SendMessage(ctx, chatJID, msgProto)
	if err != nil {
		return "", fmt.Errorf("failed to send WhatsApp reply: %w", err)
	}

	return resp.ID, nil
}

// SendText sends an unquoted text message to the given chat JID.
func (c *Client) SendText(ctx context.Context, chatJID types.JID, text string) (types.MessageID, error) {
	if c != nil && c.sendTextFn != nil {
		return c.sendTextFn(ctx, chatJID, text)
	}
	if c == nil || c.WAClient == nil {
		return "", fmt.Errorf("whatsapp client not initialized")
	}

	msgProto := BuildReplyMessage(text, "", "", "")

	resp, err := c.WAClient.SendMessage(ctx, chatJID, msgProto)
	if err != nil {
		return "", fmt.Errorf("failed to send WhatsApp text message: %w", err)
	}

	return resp.ID, nil
}

// MarkRead sends a read receipt for the specified message IDs.
func (c *Client) MarkRead(ctx context.Context, msgIDs []types.MessageID, timestamp time.Time, chatJID, senderJID types.JID) error {
	if c == nil || len(msgIDs) == 0 {
		return nil
	}
	if c.markReadFn != nil {
		return c.markReadFn(ctx, msgIDs, timestamp, chatJID, senderJID)
	}
	if c.WAClient == nil {
		return nil
	}
	return c.WAClient.MarkRead(ctx, msgIDs, timestamp, chatJID, senderJID)
}

// SendChatPresence sends a typing or recording presence indicator to the chat.
func (c *Client) SendChatPresence(ctx context.Context, chatJID types.JID, state types.ChatPresence, media types.ChatPresenceMedia) error {
	if c == nil {
		return nil
	}
	if c.sendPresenceFn != nil {
		return c.sendPresenceFn(ctx, chatJID, state, media)
	}
	if c.WAClient == nil {
		return nil
	}
	return c.WAClient.SendChatPresence(ctx, chatJID, state, media)
}

// SendReaction sends a native WhatsApp emoji reaction to a specific message.
func (c *Client) SendReaction(ctx context.Context, chatJID, senderJID types.JID, targetMsgID types.MessageID, emoji string) (types.MessageID, error) {
	if c != nil && c.sendReactionFn != nil {
		return c.sendReactionFn(ctx, chatJID, senderJID, targetMsgID, emoji)
	}
	if c == nil || c.WAClient == nil {
		return "", fmt.Errorf("whatsapp client not initialized")
	}

	reactionMsg := c.WAClient.BuildReaction(chatJID, senderJID, targetMsgID, emoji)
	resp, err := c.WAClient.SendMessage(ctx, chatJID, reactionMsg)
	if err != nil {
		return "", fmt.Errorf("failed to send WhatsApp reaction: %w", err)
	}

	return resp.ID, nil
}

// GetUserJID returns the bot's own JID.
func (c *Client) GetUserJID() types.JID {
	if c != nil {
		if !c.userJID.IsEmpty() {
			return c.userJID
		}
		if c.WAClient != nil && c.WAClient.Store != nil && c.WAClient.Store.ID != nil {
			return *c.WAClient.Store.ID
		}
	}
	return types.EmptyJID
}

// IsConnected returns whether the client is currently connected.
func (c *Client) IsConnected() bool {
	return c != nil && c.WAClient != nil && c.WAClient.IsConnected()
}

