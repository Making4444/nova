package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	waLog "go.mau.fi/whatsmeow/util/log"

	"novabot/internal/ai"
	"novabot/internal/config"
	"novabot/internal/storage"
	"novabot/internal/trigger"
	"novabot/internal/whatsapp"
)

const banner = `
===========================================================
  🌟 Nova WhatsApp Bot (نوفا - بوت واتساب الذكي) 🌟
===========================================================
`

func main() {
	fmt.Print(banner)

	// 1. Load configuration
	cfg, err := config.LoadConfig()
	if err != nil {
		fmt.Printf("❌ خطأ في تحميل الإعدادات: %v\n", err)
		os.Exit(1)
	}

	if cfg.OpenRouterAPIKey == "" {
		fmt.Println("⚠️  تنبيه: لم يتم العثور على OPENROUTER_API_KEY في ملف .env!")
		fmt.Println("    يرجى إضافة المفتاح في .env لتتمكن نوفا من توليد الردود الذكية.")
	} else {
		fmt.Printf("🤖 تم تحميل إعدادات الذكاء الاصطناعي بنجاح (النموذج: %s)\n", cfg.OpenRouterModel)
	}

	// Set up logger
	logger := waLog.Stdout("NovaBot", "INFO", true)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 2. Initialize storage components
	chatLogger, err := storage.NewChatLogger("data/chats")
	if err != nil {
		logger.Errorf("Failed to initialize chat logger: %v", err)
		os.Exit(1)
	}

	memStore, err := storage.NewMemoryStore("data/users")
	if err != nil {
		logger.Errorf("Failed to initialize memory store: %v", err)
		os.Exit(1)
	}

	sessionContainer, err := storage.InitSessionStore(ctx, cfg.SessionDBPath, logger)
	if err != nil {
		logger.Errorf("Failed to initialize session database: %v", err)
		os.Exit(1)
	}

	// 3. Initialize AI Client & Limiter
	aiClient := ai.NewOpenRouterClient(cfg.OpenRouterAPIKey, cfg.OpenRouterModel, cfg.SystemPrompt)
	limiter := trigger.NewChatLimiter(cfg.ChatCooldownSeconds)

	// 4. Initialize WhatsApp Client
	waClient, err := whatsapp.NewClient(ctx, sessionContainer, logger)
	if err != nil {
		logger.Errorf("Failed to initialize WhatsApp client: %v", err)
		os.Exit(1)
	}

	// 5. Register Event Handler
	eventHandler := whatsapp.NewEventHandler(
		waClient,
		aiClient,
		chatLogger,
		memStore,
		limiter,
		cfg.ContextHistoryLimit,
		logger,
	)
	waClient.AddEventHandler(eventHandler.HandleEvent)

	// 6. Connect to WhatsApp (Generates QR if not authenticated)
	if err := waClient.Connect(ctx); err != nil {
		logger.Errorf("Failed to connect: %v", err)
		os.Exit(1)
	}

	fmt.Println("\n🚀 بوت نوفا قيد التشغيل والاستماع للرسائل الآن...")
	fmt.Println("💡 التريجر النشط: \"يا نوفا\" (في النص المباشر أو الـ Reply)")
	fmt.Println("⏹️  اضغط Ctrl+C للإيقاف الآمن.")

	// 7. Wait for interrupt signal for graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	<-sigChan

	fmt.Println("\n🛑 جاري إيقاف نوفا وفصل الاتصال بأمان...")
	waClient.Disconnect()
	fmt.Println("👋 تم إيقاف البوت بنجاح. مع السلامة!")
}
