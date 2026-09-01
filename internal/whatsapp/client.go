package whatsapp

import (
	"context"
	"fmt"

	qrcode "github.com/skip2/go-qrcode"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types"
	waLog "go.mau.fi/whatsmeow/util/log"
)

// Client wraps whatsmeow.Client and provides helper methods.
type Client struct {
	WAClient  *whatsmeow.Client
	container *sqlstore.Container
	logger    waLog.Logger
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

// SendReply sends a reply message quoting the given message ID.
func (c *Client) SendReply(ctx context.Context, chatJID types.JID, replyText string, replyToID string, replyToSender string) (types.MessageID, error) {
	if c == nil || c.WAClient == nil {
		return "", fmt.Errorf("whatsapp client not initialized")
	}

	msgProto := BuildReplyMessage(replyText, replyToID, replyToSender)

	resp, err := c.WAClient.SendMessage(ctx, chatJID, msgProto)
	if err != nil {
		return "", fmt.Errorf("failed to send WhatsApp reply: %w", err)
	}

	return resp.ID, nil
}

// GetUserJID returns the bot's own JID.
func (c *Client) GetUserJID() types.JID {
	if c != nil && c.WAClient != nil && c.WAClient.Store != nil && c.WAClient.Store.ID != nil {
		return *c.WAClient.Store.ID
	}
	return types.EmptyJID
}

// IsConnected returns whether the client is currently connected.
func (c *Client) IsConnected() bool {
	return c != nil && c.WAClient != nil && c.WAClient.IsConnected()
}
