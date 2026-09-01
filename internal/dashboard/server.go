package dashboard

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"novabot/internal/admin"
	"novabot/internal/storage"
)

//go:embed templates/index.html
var indexHTML []byte

// ServerConfig holds configuration options for the dashboard server.
type ServerConfig struct {
	Port        string
	Password    string
	DataDir     string
	Models      map[string]string
	AdminState  *admin.State
	ChatLogger  *storage.ChatLogger
	MemoryStore *storage.MemoryStore
	LogHub      *LogHub
	Metrics     *MetricsTracker
}

// Server represents the Live Web Admin Dashboard HTTP server.
type Server struct {
	port        string
	password    string
	dataDir     string
	models      map[string]string
	adminState  *admin.State
	chatLogger  *storage.ChatLogger
	memoryStore *storage.MemoryStore
	logHub      *LogHub
	metrics     *MetricsTracker
	httpServer  *http.Server
	mu          sync.RWMutex
}

// NewServer initializes a new Server instance.
func NewServer(cfg ServerConfig) *Server {
	if cfg.Port == "" {
		cfg.Port = ":8080"
	} else if !strings.HasPrefix(cfg.Port, ":") {
		cfg.Port = ":" + cfg.Port
	}

	if cfg.DataDir == "" {
		cfg.DataDir = "data"
	}

	if cfg.LogHub == nil {
		cfg.LogHub = NewLogHub(1000)
	}

	if cfg.Metrics == nil {
		cfg.Metrics = NewMetricsTracker()
	}

	if cfg.Models == nil {
		cfg.Models = make(map[string]string)
	}

	s := &Server{
		port:        cfg.Port,
		password:    cfg.Password,
		dataDir:     cfg.DataDir,
		models:      cfg.Models,
		adminState:  cfg.AdminState,
		chatLogger:  cfg.ChatLogger,
		memoryStore: cfg.MemoryStore,
		logHub:      cfg.LogHub,
		metrics:     cfg.Metrics,
	}

	mux := http.NewServeMux()
	s.registerRoutes(mux)

	s.httpServer = &http.Server{
		Addr:         s.port,
		Handler:      mux,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 60 * time.Second,
	}

	return s
}

// GetLogHub returns the LogHub instance.
func (s *Server) GetLogHub() *LogHub {
	return s.logHub
}

// GetMetrics returns the MetricsTracker instance.
func (s *Server) GetMetrics() *MetricsTracker {
	return s.metrics
}

// SetModel records a model name for the dashboard.
func (s *Server) SetModel(category, modelName string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.models[category] = modelName
}

func (s *Server) registerRoutes(mux *http.ServeMux) {
	// Main Single-Page UI
	mux.HandleFunc("/", s.authMiddleware(s.handleIndex))

	// REST API Endpoints
	mux.HandleFunc("/api/stats", s.authMiddleware(s.handleStats))
	mux.HandleFunc("/api/chats", s.authMiddleware(s.handleChats))
	mux.HandleFunc("/api/emotions", s.authMiddleware(s.handleEmotions))
	mux.HandleFunc("/api/settings", s.authMiddleware(s.handleSettings))
	mux.HandleFunc("/api/logs", s.authMiddleware(s.handleLogs))

	// Quick Action Endpoints
	mux.HandleFunc("/api/actions/shutdown", s.authMiddleware(s.handleActionShutdown))
	mux.HandleFunc("/api/actions/restart", s.authMiddleware(s.handleActionRestart))
	mux.HandleFunc("/api/actions/clear-memory", s.authMiddleware(s.handleActionClearMemory))
	mux.HandleFunc("/api/actions/auto-triggers", s.authMiddleware(s.handleActionAutoTriggers))
}

// authMiddleware enforces Basic Auth if password is set.
func (s *Server) authMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Enable CORS
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusOK)
			return
		}

		if s.password != "" {
			// Check standard Basic Auth
			_, pass, ok := r.BasicAuth()
			if !ok || pass != s.password {
				// Also allow Bearer token
				authHeader := r.Header.Get("Authorization")
				if strings.HasPrefix(authHeader, "Bearer ") && strings.TrimPrefix(authHeader, "Bearer ") == s.password {
					next(w, r)
					return
				}

				w.Header().Set("WWW-Authenticate", `Basic realm="Nova Admin Dashboard"`)
				http.Error(w, "Unauthorized: Invalid or missing credentials", http.StatusUnauthorized)
				return
			}
		}

		next(w, r)
	}
}

// Start launches the HTTP server in a background goroutine.
func (s *Server) Start() error {
	s.logHub.AddLog("INFO", fmt.Sprintf("Starting Live Web Admin Dashboard on http://localhost%s", s.port))
	return s.httpServer.ListenAndServe()
}

// Stop gracefully shuts down the server.
func (s *Server) Stop(ctx context.Context) error {
	s.logHub.AddLog("INFO", "Stopping Live Web Admin Dashboard")
	return s.httpServer.Shutdown(ctx)
}

// -------------------------------------------------------------
// Route Handlers
// -------------------------------------------------------------

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(indexHTML)
}

func (s *Server) handleStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	chatsBaseDir := filepath.Join(s.dataDir, "chats")
	_, totalChats, groupCount, privateCount := ScanAllChats(chatsBaseDir, s.adminState)

	usersBaseDir := filepath.Join(s.dataDir, "users")
	userAffinities := ScanUserAffinities(usersBaseDir)
	memoryCount := len(userAffinities)

	isShutdown := false
	uptimeStr := "0 دقيقة"
	var uptimeSec int64 = 0

	if s.adminState != nil {
		isShutdown = s.adminState.GetShutdown()
		uptimeStr = s.adminState.GetUptimeString()
		uptimeSec = int64(time.Since(s.adminState.StartTime).Seconds())
	}

	incomingMsgs, novaMsgs, totalTokens, timeSeries := s.metrics.GetCounts()
	totalMsgs := incomingMsgs + novaMsgs

	currentMood, moodCounts, _, _ := s.metrics.GetMoodStats()

	// Estimated token cost ($0.0003 / 1k tokens average)
	estimatedCost := float64(totalTokens) * 0.0000003

	// Read scheduled tasks from data/tasks.json if exists
	scheduledCount := 0
	tasksFile := filepath.Join(s.dataDir, "tasks.json")
	if data, err := os.ReadFile(tasksFile); err == nil {
		var tasks []struct {
			Executed bool `json:"executed"`
		}
		if err := json.Unmarshal(data, &tasks); err == nil {
			for _, t := range tasks {
				if !t.Executed {
					scheduledCount++
				}
			}
		}
	}

	s.mu.RLock()
	modelsCopy := make(map[string]string)
	for k, v := range s.models {
		modelsCopy[k] = v
	}
	s.mu.RUnlock()

	response := map[string]interface{}{
		"status":              "online",
		"is_shutdown":         isShutdown,
		"uptime":              uptimeStr,
		"uptime_seconds":      uptimeSec,
		"active_chats":        totalChats,
		"groups_count":        groupCount,
		"private_chats_count": privateCount,
		"total_messages":      totalMsgs,
		"incoming_messages":   incomingMsgs,
		"nova_messages":       novaMsgs,
		"memory_count":        memoryCount,
		"scheduled_tasks":     scheduledCount,
		"models":              modelsCopy,
		"current_emotion":     currentMood,
		"emotions_breakdown":  moodCounts,
		"estimated_tokens":    totalTokens,
		"estimated_cost_usd":  estimatedCost,
		"time_series":         timeSeries,
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(response)
}

func (s *Server) handleChats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	chatID := r.URL.Query().Get("id")
	chatType := r.URL.Query().Get("type")

	// If no specific chat requested, return summary list
	if chatID == "" {
		chatsBaseDir := filepath.Join(s.dataDir, "chats")
		chats, total, groups, private := ScanAllChats(chatsBaseDir, s.adminState)

		response := map[string]interface{}{
			"total":         total,
			"groups_count":  groups,
			"private_count": private,
			"chats":         chats,
		}

		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(response)
		return
	}

	// Specific chat details & messages requested
	limit := 100
	if limStr := r.URL.Query().Get("limit"); limStr != "" {
		if v, err := strconv.Atoi(limStr); err == nil && v > 0 {
			limit = v
		}
	}

	if chatType == "" {
		if strings.Contains(chatID, "@g.us") || strings.Contains(chatID, "group") {
			chatType = "group"
		} else {
			chatType = "private"
		}
	}

	var messages []storage.LogMessage
	if s.chatLogger != nil {
		msgs, err := s.chatLogger.GetRecentMessages(chatType, chatID, limit)
		if err == nil {
			messages = msgs
		}
	}

	var summary string
	if s.chatLogger != nil {
		sum, _ := s.chatLogger.GetLatestSummary(chatType, chatID)
		summary = sum
	}

	var archives []storage.ArchiveInfo
	if s.chatLogger != nil {
		archs, _ := s.chatLogger.ListArchivedChats(chatType, chatID)
		archives = archs
	}

	response := map[string]interface{}{
		"chat_id":       chatID,
		"chat_type":     chatType,
		"message_count": len(messages),
		"messages":      messages,
		"summary":       summary,
		"archives":      archives,
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(response)
}

func (s *Server) handleEmotions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	usersBaseDir := filepath.Join(s.dataDir, "users")
	affinities := ScanUserAffinities(usersBaseDir)

	currentMood, counts, percentages, recent := s.metrics.GetMoodStats()

	response := map[string]interface{}{
		"current_mood":     currentMood,
		"mood_breakdown":   counts,
		"mood_percentages": percentages,
		"recent_emotions":  recent,
		"user_affinities":  affinities,
		"total_users":      len(affinities),
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(response)
}

func (s *Server) handleSettings(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		// Return current settings
		isShutdown := false
		adminNumber := ""
		var limits map[string]int
		var autos map[string]bool

		if s.adminState != nil {
			isShutdown = s.adminState.GetShutdown()
			limits = s.adminState.ChatLimits
			autos = s.adminState.AutoTriggersEnabled
			adminNumber = s.adminState.AdminNumber
		}

		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"is_shutdown":           isShutdown,
			"admin_number":          adminNumber,
			"chat_limits":           limits,
			"auto_triggers_enabled": autos,
		})
		return
	}

	if r.Method == http.MethodPost {
		var req struct {
			IsShutdown   *bool   `json:"is_shutdown,omitempty"`
			ChatID       string  `json:"chat_id,omitempty"`
			ChatLimit    *int    `json:"chat_limit,omitempty"`
			AutoTriggers *bool   `json:"auto_triggers,omitempty"`
			AdminNumber  *string `json:"admin_number,omitempty"`
		}

		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, fmt.Sprintf("Invalid JSON: %v", err), http.StatusBadRequest)
			return
		}

		if s.adminState != nil {
			if req.IsShutdown != nil {
				_ = s.adminState.SetShutdown(*req.IsShutdown)
				s.logHub.AddLog("INFO", fmt.Sprintf("Admin changed shutdown state to %v via Dashboard", *req.IsShutdown))
			}
			if req.ChatID != "" && req.ChatLimit != nil {
				_ = s.adminState.SetChatLimit(req.ChatID, *req.ChatLimit)
				s.logHub.AddLog("INFO", fmt.Sprintf("Admin set chat limit for %s to %d via Dashboard", req.ChatID, *req.ChatLimit))
			}
			if req.ChatID != "" && req.AutoTriggers != nil {
				_ = s.adminState.SetAutoTriggers(req.ChatID, *req.AutoTriggers)
				s.logHub.AddLog("INFO", fmt.Sprintf("Admin set auto triggers for %s to %v via Dashboard", req.ChatID, *req.AutoTriggers))
			}
			if req.AdminNumber != nil && strings.TrimSpace(*req.AdminNumber) != "" {
				_ = s.adminState.SetAdminNumber(*req.AdminNumber)
				s.logHub.AddLog("INFO", fmt.Sprintf("Admin number updated to %s via Dashboard", s.adminState.GetAdminNumber()))
			}
		}

		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"success": true,
			"message": "Settings updated successfully",
		})
		return
	}

	http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
}

func (s *Server) handleLogs(w http.ResponseWriter, r *http.Request) {
	// SSE stream check
	if r.URL.Query().Get("stream") == "true" || r.Header.Get("Accept") == "text/event-stream" {
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "Streaming unsupported", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")

		// Send initial recent logs
		recent := s.logHub.GetRecentLogs(25)
		for _, log := range recent {
			data, _ := json.Marshal(log)
			_, _ = fmt.Fprintf(w, "data: %s\n\n", data)
		}
		flusher.Flush()

		// Subscribe to real-time logs
		logChan, unsubscribe := s.logHub.Subscribe()
		defer unsubscribe()

		notify := r.Context().Done()
		for {
			select {
			case <-notify:
				return
			case log, ok := <-logChan:
				if !ok {
					return
				}
				data, _ := json.Marshal(log)
				_, _ = fmt.Fprintf(w, "data: %s\n\n", data)
				flusher.Flush()
			}
		}
	}

	// Normal JSON response
	limit := 100
	if limStr := r.URL.Query().Get("limit"); limStr != "" {
		if v, err := strconv.Atoi(limStr); err == nil && v > 0 {
			limit = v
		}
	}

	logs := s.logHub.GetRecentLogs(limit)
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"logs":  logs,
		"count": len(logs),
	})
}

// -------------------------------------------------------------
// Quick Actions Handlers
// -------------------------------------------------------------

func (s *Server) handleActionShutdown(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	isShutdown := false
	if s.adminState != nil {
		curr := s.adminState.GetShutdown()
		isShutdown = !curr
		_ = s.adminState.SetShutdown(isShutdown)
		s.logHub.AddLog("WARN", fmt.Sprintf("Toggle Shutdown Action executed: new state = %v", isShutdown))
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"success":     true,
		"is_shutdown": isShutdown,
	})
}

func (s *Server) handleActionRestart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	s.logHub.AddLog("INFO", "Soft restart command received from Web Admin Dashboard.")

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": "تم إرسال إشارة إعادة التشغيل الهادئة للبوت بنجاح.",
	})
}

func (s *Server) handleActionClearMemory(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		UserID string `json:"user_id,omitempty"`
		All    bool   `json:"all,omitempty"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)

	usersDir := filepath.Join(s.dataDir, "users")

	if req.All {
		files, err := os.ReadDir(usersDir)
		if err == nil {
			for _, f := range files {
				if strings.HasSuffix(f.Name(), ".md") {
					_ = os.Remove(filepath.Join(usersDir, f.Name()))
				}
			}
		}
		s.logHub.AddLog("WARN", "Admin cleared ALL user memory profiles via Dashboard.")
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"success": true,
			"message": "All user memory files cleared successfully",
		})
		return
	}

	if req.UserID != "" {
		// Sanitize to prevent path traversal
		cleanID := filepath.Base(req.UserID)
		cleanID = strings.TrimSuffix(cleanID, ".md")
		targetFile := filepath.Join(usersDir, cleanID+".md")
		_ = os.Remove(targetFile)

		s.logHub.AddLog("INFO", fmt.Sprintf("Admin cleared memory profile for %s via Dashboard.", req.UserID))
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"success": true,
			"message": fmt.Sprintf("Memory for %s cleared successfully", req.UserID),
		})
		return
	}

	http.Error(w, "Missing user_id or all flag", http.StatusBadRequest)
}

func (s *Server) handleActionAutoTriggers(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		ChatID  string `json:"chat_id,omitempty"`
		Enabled bool   `json:"enabled"`
		Global  bool   `json:"global,omitempty"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)

	if req.Global {
		chatsBaseDir := filepath.Join(s.dataDir, "chats")
		chats, _, _, _ := ScanAllChats(chatsBaseDir, s.adminState)
		for _, c := range chats {
			if s.adminState != nil {
				_ = s.adminState.SetAutoTriggers(c.ID, req.Enabled)
			}
		}
		s.logHub.AddLog("INFO", fmt.Sprintf("Global Auto Triggers toggled to %v via Dashboard", req.Enabled))
	} else if req.ChatID != "" && s.adminState != nil {
		_ = s.adminState.SetAutoTriggers(req.ChatID, req.Enabled)
		s.logHub.AddLog("INFO", fmt.Sprintf("Auto Triggers for %s toggled to %v via Dashboard", req.ChatID, req.Enabled))
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"enabled": req.Enabled,
	})
}
