package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
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
  🌟 Nova WhatsApp Bot (نوفا - بوت واتساب الذكي Multi-Model) 🌟
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

func performHealthCheck(cfg *config.Config) {
	fmt.Println("🔍 --- تقرير الفحص والجاهزية عند الإقلاع (Startup Diagnostics) ---")

	// 1. ffmpeg check
	if _, err := exec.LookPath("ffmpeg"); err == nil {
		fmt.Println("  [✅ ffmpeg] مثبت وجاهز لمعالجة الصوت وتحويل OGG Opus.")
	} else {
		fmt.Println("  [⚠️ ffmpeg] غير موجود في PATH! (توليد الفويس نوت الصوتي قد لا يعمل إذا لم يكن ffmpeg مثبتاً).")
	}

	// 2. OpenRouter check
	if cfg.OpenRouterAPIKey != "" {
		fmt.Printf("  [✅ OpenRouter] المفتاح متوفر (الحوار: %s | الرياضيات: %s | الرؤية: %s)\n",
			cfg.ModelChat, cfg.ModelMath, cfg.ModelVision)
	} else {
		fmt.Println("  [❌ OpenRouter] لم يتم العثور على OPENROUTER_API_KEY في ملف .env!")
	}

	// 3. Groq check
	if cfg.GroqAPIKey != "" {
		fmt.Printf("  [✅ Groq API] المفتاح متوفر (تفريغ الصوت: %s | الراوتر السريع: %s)\n",
			cfg.ModelWhisper, cfg.ModelRouterGroq)
	} else {
		fmt.Println("  [⚠️ Groq API] لم يتم العثور على GROQ_API_KEY (تفريغ الفويس نوت سيكون معطلاً).")
	}

	fmt.Println("-------------------------------------------------------------------")
}

func main() {
	fmt.Print(banner)

	// 1. Load configuration
	cfg, err := config.LoadConfig()
	if err != nil {
		fmt.Printf("❌ خطأ في تحميل الإعدادات: %v\n", err)
		os.Exit(1)
	}

	performHealthCheck(cfg)

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

	// 3. Initialize Groq Fast Router & Multi-Model AI Client
	groqRouter := ai.NewGroqRouter(cfg.GroqAPIKey, cfg.ModelRouterGroq)
	aiClient := ai.NewMultiModelClient(
		cfg.OpenRouterAPIKey,
		cfg.ModelChat,
		cfg.ModelMath,
		cfg.ModelVision,
		groqRouter,
		cfg.SystemPrompt,
	)
	aiClient.SetMemoryUpdater(memStore)

	limiter := trigger.NewChatLimiter(cfg.ChatCooldownSeconds)

	// 4. Initialize WhatsApp Client
	waClient, err := whatsapp.NewClient(ctx, sessionContainer, logger)
	if err != nil {
		logger.Errorf("Failed to initialize WhatsApp client: %v", err)
		os.Exit(1)
	}

	// 5. Register Event Handler, Groq STT & Voice Engine
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

	if cfg.GroqAPIKey != "" {
		groqSTT := voice.NewGroqSTT(cfg.GroqAPIKey, cfg.ModelWhisper)
		eventHandler.SetGroqSTT(groqSTT)
	}

	if cfg.OpenRouterAPIKey != "" {
		ttsClient := voice.NewOpenRouterTTS(cfg.OpenRouterAPIKey, cfg.TTSModel, cfg.TTSVoice)
		eventHandler.SetTTSClient(ttsClient)
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
		modelName: fmt.Sprintf("%s (Chat) + %s (Math) + %s (Vision)", cfg.ModelChat, cfg.ModelMath, cfg.ModelVision),
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

	fmt.Println("\n🚀 بوت نوفا قيد التشغيل والاستماع للرسائل الآن بنظام Multi-Model...")
	fmt.Println("💡 التريجرات: \"يا نوفا\"، \"نوفا\"، أو منشن/تاج @nova أو برقم الحساب.")
	fmt.Println("👑 أوامر الأدمن المتاحة: /shutdown, /start, /set <limit>, /auto <on|off>, /archive <new|list|load N>, /status")
	fmt.Println("⏹️  اضغط Ctrl+C للإيقاف الآمن.")

	// 8. Wait for interrupt signal for graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	<-sigChan

	fmt.Println("\n🛑 جاري إيقاف نوفا وفصل الاتصال بأمان...")
	waClient.Disconnect()
	fmt.Println("👋 تم إيقاف البوت بنجاح. مع السلامة!")
}

