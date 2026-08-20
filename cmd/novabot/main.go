package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	waLog "go.mau.fi/whatsmeow/util/log"

	"novabot/internal/admin"
	"novabot/internal/ai"
	"novabot/internal/config"
	"novabot/internal/scheduler"
	"novabot/internal/storage"
	"novabot/internal/trigger"
	"novabot/internal/voice"
	"novabot/internal/whatsapp"
)

const banner = `
===========================================================
  🌟 Nova WhatsApp Bot (نوفا - بوت واتساب الذكي) 🌟
===========================================================
`

type botStats struct {
	modelName string
	scheduler *scheduler.Engine
}

func (s *botStats) GetTotalChatsCount() int {
	count := 0
	for _, sub := range []string{"data/chats/groups", "data/chats/private"} {
		if files, err := os.ReadDir(sub); err == nil {
			count += len(files)
		}
	}
	return count
}

func (s *botStats) GetMemoryProfilesCount() int {
	if files, err := os.ReadDir("data/users"); err == nil {
		return len(files)
	}
	return 0
}

func (s *botStats) GetScheduledTasksCount() int {
	if s.scheduler != nil {
		return s.scheduler.GetScheduledTasksCount()
	}
	return 0
}

func (s *botStats) GetModelName() string {
	return s.modelName
}

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

	// 2. Initialize storage components and Admin State
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

	adminState, err := admin.NewState("data", "201202172699")
	if err != nil {
		logger.Errorf("Failed to initialize admin state: %v", err)
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

	// 5. Register Event Handler & Voice Engine
	eventHandler := whatsapp.NewEventHandler(
		waClient,
		aiClient,
		chatLogger,
		memStore,
		adminState,
		limiter,
		cfg.ContextHistoryLimit,
		logger,
	)

	if cfg.OpenRouterAPIKey != "" {
		ttsClient := voice.NewOpenRouterTTS(cfg.OpenRouterAPIKey, cfg.TTSModel, cfg.TTSVoice)
		eventHandler.SetTTSClient(ttsClient)
		fmt.Printf("🎙️  تم تفعيل المحرك الصوتي بنجاح (النموذج: %s | الصوت: %s)\n", cfg.TTSModel, cfg.TTSVoice)
	}

	// 6. Initialize Scheduler Engine & wire dependencies
	schedulerEngine := scheduler.NewEngine(
		"data",
		adminState,
		chatLogger,
		memStore,
		eventHandler,
		aiClient,
	)
	aiClient.SetScheduler(schedulerEngine)
	eventHandler.SetSchedulerEngine(schedulerEngine)

	stats := &botStats{
		modelName: cfg.OpenRouterModel,
		scheduler: schedulerEngine,
	}
	eventHandler.SetStatsProvider(stats)

	schedulerEngine.Start()
	defer schedulerEngine.Stop()

	waClient.AddEventHandler(eventHandler.HandleEvent)

	// 7. Connect to WhatsApp (Generates QR if not authenticated)
	if err := waClient.Connect(ctx); err != nil {
		logger.Errorf("Failed to connect: %v", err)
		os.Exit(1)
	}

	fmt.Println("\n🚀 بوت نوفا قيد التشغيل والاستماع للرسائل الآن...")
	fmt.Println("💡 التريجر النشط: \"يا نوفا\" (في النص المباشر أو الـ Reply)")
	fmt.Println("👑 أوامر الأدمن المتاحة: /shutdown, /start, /set <limit>, /auto <on|off>, /status")
	fmt.Println("⏹️  اضغط Ctrl+C للإيقاف الآمن.")

	// 8. Wait for interrupt signal for graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	<-sigChan

	fmt.Println("\n🛑 جاري إيقاف نوفا وفصل الاتصال بأمان...")
	waClient.Disconnect()
	fmt.Println("👋 تم إيقاف البوت بنجاح. مع السلامة!")
}
