package portal

import (
	"context"
	"crypto/rand"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"strings"
	"sync"
	"time"

	"novabot/internal/billing"
)

//go:embed templates/index.html
var indexHTML []byte

//go:embed templates/dashboard.html
var dashboardHTML []byte

//go:embed templates/admin.html
var adminHTML []byte

// AIClientUpdater allows dynamic updating of active AI models.
type AIClientUpdater interface {
	SetModels(chat, math, academic, vision, summarizer string)
	GetModels() map[string]string
}

// TTSClientUpdater allows dynamic updating of active TTS model and voice.
type TTSClientUpdater interface {
	SetModelAndVoice(model, voice string)
}

// ImageGenUpdater allows dynamic updating of image generator configuration.
type ImageGenUpdater interface {
	SetConfig(baseURL, model string)
	GetConfig() (string, string)
}

// ServerConfig holds parameters for the Commercial Web Portal.
type ServerConfig struct {
	Port          string
	Store         *billing.Store
	AIClient      AIClientUpdater
	TTSClient     TTSClientUpdater
	ImageGen      ImageGenUpdater
	AdminPassword string
}

// Server manages the commercial web portal HTTP endpoints on port 8000.
type Server struct {
	port          string
	store         *billing.Store
	aiClient      AIClientUpdater
	ttsClient     TTSClientUpdater
	imageGen      ImageGenUpdater
	adminPassword string
	httpServer    *http.Server

	sessionsMu sync.RWMutex
	sessions   map[string]sessionData // token -> sessionData
}

type sessionData struct {
	UserID    int64
	Username  string
	IsAdmin   bool
	ExpiresAt time.Time
}

// NewServer creates a new Commercial Web Portal instance.
func NewServer(cfg ServerConfig) *Server {
	port := cfg.Port
	if port == "" {
		port = ":8000"
	} else if !strings.HasPrefix(port, ":") {
		port = ":" + port
	}

	adminPwd := cfg.AdminPassword
	if adminPwd == "" {
		adminPwd = "nova_admin_2026"
	}

	s := &Server{
		port:          port,
		store:         cfg.Store,
		aiClient:      cfg.AIClient,
		ttsClient:     cfg.TTSClient,
		imageGen:      cfg.ImageGen,
		adminPassword: adminPwd,
		sessions:      make(map[string]sessionData),
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

// Start runs the HTTP portal server in a background goroutine.
func (s *Server) Start() error {
	fmt.Printf("[🚀 Commercial Portal] Starting web portal at http://0.0.0.0%s (Domain: https://nova.elyon.buzz)\n", s.port)
	go func() {
		if err := s.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			fmt.Printf("[❌ Commercial Portal ERROR] Server failed: %v\n", err)
		}
	}()
	return nil
}

// Shutdown stops the HTTP portal gracefully.
func (s *Server) Shutdown(ctx context.Context) error {
	return s.httpServer.Shutdown(ctx)
}

func generateToken() string {
	b := make([]byte, 24)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func (s *Server) createSession(userID int64, username string, isAdmin bool) string {
	token := generateToken()
	expiresAt := time.Now().Add(30 * 24 * time.Hour)

	data := sessionData{
		UserID:    userID,
		Username:  username,
		IsAdmin:   isAdmin,
		ExpiresAt: expiresAt,
	}

	s.sessionsMu.Lock()
	s.sessions[token] = data
	s.sessionsMu.Unlock()

	if s.store != nil {
		_ = s.store.SaveWebSession(token, userID, username, isAdmin, expiresAt)
	}
	return token
}

func (s *Server) resolveSession(token string) (sessionData, bool) {
	if token == "" {
		return sessionData{}, false
	}

	s.sessionsMu.RLock()
	data, exists := s.sessions[token]
	s.sessionsMu.RUnlock()

	if exists {
		if time.Now().After(data.ExpiresAt) {
			s.sessionsMu.Lock()
			delete(s.sessions, token)
			s.sessionsMu.Unlock()
			if s.store != nil {
				_ = s.store.DeleteWebSession(token)
			}
			return sessionData{}, false
		}
		return data, true
	}

	if s.store != nil {
		dbData, found := s.store.GetWebSession(token)
		if found {
			sess := sessionData{
				UserID:    dbData.UserID,
				Username:  dbData.Username,
				IsAdmin:   dbData.IsAdmin,
				ExpiresAt: dbData.ExpiresAt,
			}
			s.sessionsMu.Lock()
			s.sessions[token] = sess
			s.sessionsMu.Unlock()
			return sess, true
		}
	}

	return sessionData{}, false
}

func (s *Server) getSession(r *http.Request) (sessionData, bool) {
	token := ""
	if cookie, err := r.Cookie("nova_portal_session"); err == nil && cookie.Value != "" {
		token = cookie.Value
	}
	if token == "" {
		token = r.Header.Get("X-User-Token")
	}
	if token == "" {
		return sessionData{}, false
	}
	return s.resolveSession(token)
}

func (s *Server) verifyAdmin(r *http.Request) bool {
	token := ""
	if cookie, err := r.Cookie("nova_admin_session"); err == nil && cookie.Value != "" {
		token = cookie.Value
	}
	if token == "" {
		token = r.Header.Get("X-Admin-Token")
	}
	if token == "" {
		auth := r.Header.Get("Authorization")
		if strings.HasPrefix(auth, "Bearer ") {
			token = strings.TrimSpace(strings.TrimPrefix(auth, "Bearer "))
		}
	}
	if token == "" {
		return false
	}
	data, ok := s.resolveSession(token)
	return ok && data.IsAdmin
}

func (s *Server) clearUserSession(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie("nova_portal_session"); err == nil {
		s.sessionsMu.Lock()
		delete(s.sessions, cookie.Value)
		s.sessionsMu.Unlock()
		if s.store != nil {
			_ = s.store.DeleteWebSession(cookie.Value)
		}
	}
	http.SetCookie(w, &http.Cookie{
		Name:     "nova_portal_session",
		Value:    "",
		Path:     "/",
		Expires:  time.Unix(0, 0),
		MaxAge:   -1,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
}

func (s *Server) clearAdminSession(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie("nova_admin_session"); err == nil {
		s.sessionsMu.Lock()
		delete(s.sessions, cookie.Value)
		s.sessionsMu.Unlock()
		if s.store != nil {
			_ = s.store.DeleteWebSession(cookie.Value)
		}
	}
	http.SetCookie(w, &http.Cookie{
		Name:     "nova_admin_session",
		Value:    "",
		Path:     "/",
		Expires:  time.Unix(0, 0),
		MaxAge:   -1,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
}

func (s *Server) registerRoutes(mux *http.ServeMux) {
	// Public UI pages
	mux.HandleFunc("/", s.handleRoot)
	mux.HandleFunc("/dashboard", s.handleDashboardPage)

	// User Auth APIs
	mux.HandleFunc("/api/register", s.handleRegister)
	mux.HandleFunc("/api/login", s.handleLogin)
	mux.HandleFunc("/api/logout", s.handleLogout)
	mux.HandleFunc("/api/me", s.handleMe)
	mux.HandleFunc("/api/link-code", s.handleGenerateLinkCode)
	mux.HandleFunc("/api/transactions", s.handleTransactions)

	// Secret Admin Panel (Never linked in user navigation)
	mux.HandleFunc("/portal-admin-secure", s.handleAdminPage)
	mux.HandleFunc("/portal-admin-secure/login", s.handleAdminLogin)
	mux.HandleFunc("/portal-admin-secure/logout", s.handleAdminLogout)
	mux.HandleFunc("/portal-admin-secure/api/users", s.handleAdminGetUsers)
	mux.HandleFunc("/portal-admin-secure/api/topup", s.handleAdminTopup)
	mux.HandleFunc("/portal-admin-secure/api/settings", s.handleAdminSettings)
}

func (s *Server) handleRoot(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Expires", "0")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(indexHTML)
}

func (s *Server) handleDashboardPage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Expires", "0")

	sess, ok := s.getSession(r)
	if !ok || sess.UserID <= 0 {
		s.clearUserSession(w, r)
		http.Redirect(w, r, "/", http.StatusFound)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(dashboardHTML)
}

func (s *Server) handleAdminPage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Expires", "0")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(adminHTML)
}

func (s *Server) handleRegister(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "بيانات غير صالحة", http.StatusBadRequest)
		return
	}

	user, err := s.store.RegisterUser(req.Username, req.Password)
	if err != nil {
		jsonError(w, err.Error(), http.StatusBadRequest)
		return
	}

	token := s.createSession(user.ID, user.Username, false)
	http.SetCookie(w, &http.Cookie{
		Name:     "nova_portal_session",
		Value:    token,
		Path:     "/",
		Expires:  time.Now().Add(7 * 24 * time.Hour),
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})

	jsonResponse(w, map[string]interface{}{
		"success":  true,
		"user":     user,
		"redirect": "/dashboard",
	})
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "بيانات غير صالحة", http.StatusBadRequest)
		return
	}

	user, err := s.store.AuthenticateUser(req.Username, req.Password)
	if err != nil {
		jsonError(w, err.Error(), http.StatusUnauthorized)
		return
	}

	token := s.createSession(user.ID, user.Username, false)
	http.SetCookie(w, &http.Cookie{
		Name:     "nova_portal_session",
		Value:    token,
		Path:     "/",
		Expires:  time.Now().Add(7 * 24 * time.Hour),
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})

	jsonResponse(w, map[string]interface{}{
		"success":  true,
		"user":     user,
		"redirect": "/dashboard",
	})
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	s.clearUserSession(w, r)
	http.Redirect(w, r, "/", http.StatusFound)
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	sess, ok := s.getSession(r)
	if !ok || sess.UserID <= 0 {
		s.clearUserSession(w, r)
		jsonError(w, "غير مسجل الدخول", http.StatusUnauthorized)
		return
	}

	user, err := s.store.GetUserByID(sess.UserID)
	if err != nil {
		jsonError(w, "المستخدم غير موجود", http.StatusNotFound)
		return
	}

	chats, _ := s.store.GetLinkedChatsForUser(sess.UserID)
	settings, _ := s.store.GetSettings()

	jsonResponse(w, map[string]interface{}{
		"success":              true,
		"user":                 user,
		"linked_chats":         chats,
		"recharge_instapay":    settings.RechargeInstapay,
		"recharge_vodafone":    settings.RechargeVodafone,
		"recharge_instructions": settings.RechargeInstructions,
	})
}

func (s *Server) handleGenerateLinkCode(w http.ResponseWriter, r *http.Request) {
	sess, ok := s.getSession(r)
	if !ok || sess.UserID <= 0 {
		jsonError(w, "غير مسجل الدخول", http.StatusUnauthorized)
		return
	}

	code, err := s.store.GenerateLinkCode(sess.UserID)
	if err != nil {
		jsonError(w, "تعذر جلب كود الربط", http.StatusInternalServerError)
		return
	}

	jsonResponse(w, map[string]interface{}{
		"success":    true,
		"code":       code,
		"expires_in": "كود دائم وثابت لحسابك",
		"bot_number": "01202172699",
	})
}

func (s *Server) handleTransactions(w http.ResponseWriter, r *http.Request) {
	sess, ok := s.getSession(r)
	if !ok || sess.UserID <= 0 {
		jsonError(w, "غير مسجل الدخول", http.StatusUnauthorized)
		return
	}

	txs, err := s.store.GetUserTransactions(sess.UserID, 50)
	if err != nil {
		jsonError(w, "تعذر جلب العمليات", http.StatusInternalServerError)
		return
	}

	jsonResponse(w, map[string]interface{}{
		"success":      true,
		"transactions": txs,
	})
}

// Secret Admin Panel Handlers
func (s *Server) handleAdminLogout(w http.ResponseWriter, r *http.Request) {
	s.clearAdminSession(w, r)
	http.Redirect(w, r, "/portal-admin-secure", http.StatusFound)
}

func (s *Server) handleAdminLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "بيانات غير صالحة", http.StatusBadRequest)
		return
	}

	settings, _ := s.store.GetSettings()
	targetPassword := s.adminPassword
	if settings != nil && settings.AdminPassword != "" {
		targetPassword = settings.AdminPassword
	}

	if req.Password != targetPassword {
		jsonError(w, "كلمة المرور غير صحيحة", http.StatusUnauthorized)
		return
	}

	token := s.createSession(0, "admin", true)
	http.SetCookie(w, &http.Cookie{
		Name:     "nova_admin_session",
		Value:    token,
		Path:     "/",
		Expires:  time.Now().Add(30 * 24 * time.Hour),
		MaxAge:   86400 * 30,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})

	jsonResponse(w, map[string]interface{}{
		"success":  true,
		"is_admin": true,
		"token":    token,
	})
}

func (s *Server) handleAdminGetUsers(w http.ResponseWriter, r *http.Request) {
	if !s.verifyAdmin(r) {
		jsonError(w, "غير مصرح - صلاحيات الأدمن مطلوبة", http.StatusUnauthorized)
		return
	}

	users, err := s.store.GetAllUsers()
	if err != nil {
		jsonError(w, "فشل جلب المستخدمين: "+err.Error(), http.StatusInternalServerError)
		return
	}

	var totalBalance float64
	for _, u := range users {
		totalBalance += u.BalanceEGP
	}

	financeStats := s.store.GetFinanceStats()

	jsonResponse(w, map[string]interface{}{
		"success":           true,
		"users":             users,
		"total_users":       len(users),
		"total_balance":     totalBalance,
		"total_revenue":     financeStats.TotalRevenueEGP,
		"total_server_cost": financeStats.TotalServerCostEGP,
		"net_profit":        financeStats.NetProfitEGP,
		"total_queries":     financeStats.TotalQueries,
	})
}

func (s *Server) handleAdminTopup(w http.ResponseWriter, r *http.Request) {
	if !s.verifyAdmin(r) {
		jsonError(w, "غير مصرح", http.StatusUnauthorized)
		return
	}

	var req struct {
		UserID      int64   `json:"user_id"`
		AmountEGP   float64 `json:"amount_egp"`
		Description string  `json:"description"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "بيانات غير صالحة", http.StatusBadRequest)
		return
	}

	newBal, err := s.store.AdjustBalance(req.UserID, req.AmountEGP, req.Description)
	if err != nil {
		jsonError(w, "فشل تعديل الرصيد: "+err.Error(), http.StatusBadRequest)
		return
	}

	actionWord := "إضافة"
	if req.AmountEGP < 0 {
		actionWord = "خصم"
	}

	jsonResponse(w, map[string]interface{}{
		"success":     true,
		"new_balance": newBal,
		"message":     fmt.Sprintf("تم %s %.2f جنيه بنجاح. الرصيد الجديد: %.2f جنيه", actionWord, math.Abs(req.AmountEGP), newBal),
	})
}

func (s *Server) handleAdminSettings(w http.ResponseWriter, r *http.Request) {
	if !s.verifyAdmin(r) {
		jsonError(w, "غير مصرح", http.StatusUnauthorized)
		return
	}

	if r.Method == http.MethodGet {
		settings, err := s.store.GetSettings()
		if err != nil {
			jsonError(w, err.Error(), http.StatusInternalServerError)
			return
		}
		jsonResponse(w, map[string]interface{}{
			"success":  true,
			"settings": settings,
		})
		return
	}

	if r.Method == http.MethodPost {
		var cfg billing.PlatformSettings
		if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
			jsonError(w, "بيانات غير صالحة", http.StatusBadRequest)
			return
		}

		if err := s.store.UpdateSettings(&cfg); err != nil {
			jsonError(w, "فشل حفظ الإعدادات: "+err.Error(), http.StatusInternalServerError)
			return
		}

		// Dynamically update runtime AI models
		if s.aiClient != nil {
			s.aiClient.SetModels(cfg.ModelChat, cfg.ModelMath, cfg.ModelAcademic, cfg.ModelVision, cfg.ModelSummarizer)
			fmt.Printf("\n[⚙️ Portal Admin] Updated live AI models -> Chat: %s, Math: %s, Academic: %s, Vision: %s\n", cfg.ModelChat, cfg.ModelMath, cfg.ModelAcademic, cfg.ModelVision)
		}

		// Dynamically update runtime TTS model & voice
		if s.ttsClient != nil {
			s.ttsClient.SetModelAndVoice(cfg.TTSModel, cfg.TTSVoice)
			fmt.Printf("[🎙️ Portal Admin] Updated live TTS -> Model: %s, Voice: %s\n", cfg.TTSModel, cfg.TTSVoice)
		}

		// Dynamically update runtime Image Generator
		if s.imageGen != nil {
			s.imageGen.SetConfig(cfg.ImageBaseURL, cfg.ModelImage)
			fmt.Printf("[🎨 Portal Admin] Updated live Image Gen -> Model: %s, BaseURL: %s\n", cfg.ModelImage, cfg.ImageBaseURL)
		}

		jsonResponse(w, map[string]interface{}{
			"success":  true,
			"message":  "تم حفظ وتطبيق الإعدادات فوراً على السيرفر بنجاح!",
			"settings": cfg,
		})
		return
	}

	http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
}

func jsonResponse(w http.ResponseWriter, data interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(data)
}

func jsonError(w http.ResponseWriter, message string, status int) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"success": false,
		"error":   message,
	})
}
