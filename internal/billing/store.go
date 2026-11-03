package billing

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

// User represents a registered platform user.
type User struct {
	ID           int64     `json:"id"`
	Username     string    `json:"username"`
	PasswordHash string    `json:"-"`
	BalanceEGP   float64   `json:"balance_egp"`
	LinkCode     string    `json:"link_code"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// UserSummary provides a summary of a user for the admin view.
type UserSummary struct {
	User
	LinkedChats []LinkedChat `json:"linked_chats"`
}

// LinkedChat represents a WhatsApp chat or group associated with a user account.
type LinkedChat struct {
	ID        int64     `json:"id"`
	UserID    int64     `json:"user_id"`
	ChatID    string    `json:"chat_id"`
	ChatType  string    `json:"chat_type"` // "direct" or "group"
	ChatName  string    `json:"chat_name"`
	CreatedAt time.Time `json:"created_at"`
}

// LinkCode represents a temporary OTP generated to link a WhatsApp chat to a user.
type LinkCode struct {
	Code      string    `json:"code"`
	UserID    int64     `json:"user_id"`
	ExpiresAt time.Time `json:"expires_at"`
	Used      bool      `json:"used"`
}

// Transaction represents a financial or usage debit/credit entry.
type Transaction struct {
	ID          int64     `json:"id"`
	UserID      int64     `json:"user_id"`
	ChatID      string    `json:"chat_id"`
	AmountEGP   float64   `json:"amount_egp"`
	Type        string    `json:"type"` // "deduction", "topup", "bonus"
	CostUSD     float64   `json:"cost_usd"`
	Tokens      int       `json:"tokens"`
	Description string    `json:"description"`
	CreatedAt   time.Time `json:"created_at"`
}

// PlatformSettings holds the commercial parameters and model configurations.
type PlatformSettings struct {
	USDEquivalentEGP      float64 `json:"usd_equivalent_egp"`              // Default 55.0
	USDtoEGPRate          float64 `json:"usd_to_egp_rate,omitempty"`       // Alias for admin UI compatibility
	FixedMarginEGP        float64 `json:"fixed_margin_egp"`                // Default 0.10 EGP (10 piasters)
	EnforcePaid           bool    `json:"enforce_paid"`                    // Default true
	AdminPassword         string  `json:"admin_password"`                  // Default "nova_admin_2026"
	ModelChat             string  `json:"model_chat"`                      // e.g. "gemini-3.8-flash"
	ModelMath             string  `json:"model_math"`                      // e.g. "gemini-3.8-flash"
	ModelVision           string  `json:"model_vision"`                    // e.g. "gemini-3.8-flash"
	ModelAcademic         string  `json:"model_academic"`                  // e.g. "gemini-3.8-flash"
	ModelSummarizer       string  `json:"model_summarizer"`                // e.g. "gemini-3.8-flash"
	TTSModel              string  `json:"tts_model"`                       // e.g. "gemini-3.8-flash-tts"
	TTSVoice              string  `json:"tts_voice"`                       // e.g. "Charon"
	RechargeInstapay      string  `json:"recharge_instapay"`               // e.g. "makari@instapay"
	RechargeVodafone      string  `json:"recharge_vodafone"`               // e.g. "01202172699"
	RechargeInstructions string  `json:"recharge_instructions"`           // Friendly text
}

// Store manages all billing and user records in SQLite.
type Store struct {
	db *sql.DB
	mu sync.RWMutex
}

// HashPassword creates a SHA-256 hash with salt for secure storage.
func HashPassword(password string) string {
	salt := "nova_platform_salt_2026"
	h := sha256.New()
	h.Write([]byte(salt + password))
	return hex.EncodeToString(h.Sum(nil))
}

// NewStore initializes a SQLite store for billing, users, and linking.
func NewStore(dbPath string) (*Store, error) {
	if dbPath == "" {
		dbPath = "data/billing.db"
	}
	dir := filepath.Dir(dbPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create db directory %s: %w", dir, err)
	}

	db, err := sql.Open("sqlite", dbPath+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)")
	if err != nil {
		return nil, fmt.Errorf("failed to open sqlite database: %w", err)
	}

	store := &Store{db: db}
	if err := store.initSchema(); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to init billing schema: %w", err)
	}

	return store, nil
}

func (s *Store) initSchema() error {
	schema := `
	CREATE TABLE IF NOT EXISTS users (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		username TEXT UNIQUE NOT NULL,
		password_hash TEXT NOT NULL,
		balance_egp REAL NOT NULL DEFAULT 0.0,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS linked_chats (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		user_id INTEGER NOT NULL,
		chat_id TEXT UNIQUE NOT NULL,
		chat_type TEXT NOT NULL DEFAULT 'direct',
		chat_name TEXT NOT NULL DEFAULT '',
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY(user_id) REFERENCES users(id) ON DELETE CASCADE
	);

	CREATE TABLE IF NOT EXISTS link_codes (
		code TEXT PRIMARY KEY,
		user_id INTEGER NOT NULL,
		expires_at DATETIME NOT NULL,
		used INTEGER DEFAULT 0,
		FOREIGN KEY(user_id) REFERENCES users(id) ON DELETE CASCADE
	);

	CREATE TABLE IF NOT EXISTS transactions (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		user_id INTEGER NOT NULL,
		chat_id TEXT NOT NULL DEFAULT '',
		amount_egp REAL NOT NULL,
		type TEXT NOT NULL,
		cost_usd REAL NOT NULL DEFAULT 0.0,
		tokens INTEGER NOT NULL DEFAULT 0,
		description TEXT NOT NULL DEFAULT '',
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY(user_id) REFERENCES users(id) ON DELETE CASCADE
	);

	CREATE TABLE IF NOT EXISTS settings (
		key TEXT PRIMARY KEY,
		value TEXT NOT NULL
	);

	CREATE TABLE IF NOT EXISTS web_sessions (
		token TEXT PRIMARY KEY,
		user_id INTEGER NOT NULL DEFAULT 0,
		username TEXT NOT NULL DEFAULT '',
		is_admin BOOLEAN NOT NULL DEFAULT 0,
		expires_at DATETIME NOT NULL,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);
	`
	_, err := s.db.Exec(schema)
	if err != nil {
		return err
	}

	// Migrate users table with permanent link_code column
	_, _ = s.db.Exec(`ALTER TABLE users ADD COLUMN link_code TEXT`)
	_, _ = s.db.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS idx_users_link_code ON users(link_code)`)

	// Populate permanent link_code for any users that don't have one
	rows, err := s.db.Query(`SELECT id FROM users WHERE link_code IS NULL OR link_code = ''`)
	if err == nil {
		var ids []int64
		for rows.Next() {
			var id int64
			if err := rows.Scan(&id); err == nil {
				ids = append(ids, id)
			}
		}
		rows.Close()
		for _, id := range ids {
			code := s.generateUniqueLinkCode()
			_, _ = s.db.Exec(`UPDATE users SET link_code = ? WHERE id = ?`, code, id)
		}
	}

	// Seed default settings if empty
	defaults := map[string]string{
		"usd_rate":              "55.0",
		"fixed_margin_egp":      "0.10",
		"enforce_paid":          "true",
		"admin_password":        "nova_admin_2026",
		"model_chat":            "gemini-3.8-flash",
		"model_math":            "gemini-3.8-flash",
		"model_vision":          "gemini-3.8-flash",
		"model_academic":        "gemini-3.8-flash",
		"model_summarizer":      "gemini-3.8-flash",
		"tts_model":             "gemini-3.8-flash-tts",
		"tts_voice":             "Charon",
		"recharge_instapay":     "makari@instapay",
		"recharge_vodafone":     "01202172699",
		"recharge_instructions": "لشحن رصيدك عبر إنستاباي أو فودافون كاش، قم بالتحويل للرقم الموضح ثم تواصل لتأكيد الإيداع وإضافة الرصيد لحسابك فوراً.",
	}

	for k, v := range defaults {
		_, _ = s.db.Exec(`INSERT OR IGNORE INTO settings (key, value) VALUES (?, ?)`, k, v)
	}

	return nil
}

// Close closes the underlying database.
func (s *Store) Close() error {
	return s.db.Close()
}

// generateUniqueLinkCode creates a unique 6-digit numeric permanent link code.
func (s *Store) generateUniqueLinkCode() string {
	for {
		n, err := rand.Int(rand.Reader, big.NewInt(900000))
		if err != nil {
			return fmt.Sprintf("%06d", time.Now().UnixNano()%1000000)
		}
		code := fmt.Sprintf("%06d", n.Int64()+100000)
		var count int
		_ = s.db.QueryRow(`SELECT COUNT(*) FROM users WHERE link_code = ?`, code).Scan(&count)
		if count == 0 {
			return code
		}
	}
}

// RegisterUser creates a new user account with initial zero balance and permanent link code.
func (s *Store) RegisterUser(username, password string) (*User, error) {
	username = strings.TrimSpace(username)
	if username == "" {
		return nil, errors.New("اسم المستخدم لا يمكن أن يكون فارغاً")
	}
	if len(password) < 4 {
		return nil, errors.New("كلمة المرور يجب أن تكون 4 أحرف على الأقل")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	linkCode := s.generateUniqueLinkCode()
	hash := HashPassword(password)
	res, err := s.db.Exec(
		`INSERT INTO users (username, password_hash, balance_egp, link_code, created_at, updated_at) VALUES (?, ?, 0.0, ?, ?, ?)`,
		username, hash, linkCode, time.Now(), time.Now(),
	)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return nil, errors.New("اسم المستخدم مسجل بالفعل، يرجى اختيار اسم آخر")
		}
		return nil, err
	}

	id, _ := res.LastInsertId()
	return &User{
		ID:         id,
		Username:   username,
		BalanceEGP: 0.0,
		LinkCode:   linkCode,
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
	}, nil
}

// AuthenticateUser verifies user credentials and returns the User record.
func (s *Store) AuthenticateUser(username, password string) (*User, error) {
	username = strings.TrimSpace(username)
	hash := HashPassword(password)

	s.mu.RLock()
	defer s.mu.RUnlock()

	row := s.db.QueryRow(`SELECT id, username, password_hash, balance_egp, COALESCE(link_code, ''), created_at, updated_at FROM users WHERE username = ?`, username)
	var u User
	if err := row.Scan(&u.ID, &u.Username, &u.PasswordHash, &u.BalanceEGP, &u.LinkCode, &u.CreatedAt, &u.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, errors.New("اسم المستخدم أو كلمة المرور غير صحيحة")
		}
		return nil, err
	}

	if u.PasswordHash != hash {
		return nil, errors.New("اسم المستخدم أو كلمة المرور غير صحيحة")
	}

	return &u, nil
}

// GetUserByID loads a user by their unique database ID.
func (s *Store) GetUserByID(id int64) (*User, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	row := s.db.QueryRow(`SELECT id, username, balance_egp, COALESCE(link_code, ''), created_at, updated_at FROM users WHERE id = ?`, id)
	var u User
	if err := row.Scan(&u.ID, &u.Username, &u.BalanceEGP, &u.LinkCode, &u.CreatedAt, &u.UpdatedAt); err != nil {
		return nil, err
	}
	return &u, nil
}

// GetAllUsers retrieves all users and their linked chats for admin review.
func (s *Store) GetAllUsers() ([]UserSummary, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	rows, err := s.db.Query(`SELECT id, username, balance_egp, COALESCE(link_code, ''), created_at, updated_at FROM users ORDER BY id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var summaries []UserSummary
	for rows.Next() {
		var u User
		if err := rows.Scan(&u.ID, &u.Username, &u.BalanceEGP, &u.LinkCode, &u.CreatedAt, &u.UpdatedAt); err == nil {
			summaries = append(summaries, UserSummary{User: u})
		}
	}

	for i := range summaries {
		chats, _ := s.getLinkedChatsForUserInternal(summaries[i].ID)
		summaries[i].LinkedChats = chats
	}

	return summaries, nil
}

// TopupBalance adds credit to a user account and records the transaction.
func (s *Store) TopupBalance(userID int64, amountEGP float64, description string) (float64, error) {
	if amountEGP <= 0 {
		return 0, errors.New("قيمة الشحن يجب أن تكون أكبر من صفر")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	tx, err := s.db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	var currentBal float64
	row := tx.QueryRow(`SELECT balance_egp FROM users WHERE id = ?`, userID)
	if err := row.Scan(&currentBal); err != nil {
		return 0, fmt.Errorf("user not found: %w", err)
	}

	newBal := currentBal + amountEGP
	newBal = math.Round(newBal*100) / 100

	_, err = tx.Exec(`UPDATE users SET balance_egp = ?, updated_at = ? WHERE id = ?`, newBal, time.Now(), userID)
	if err != nil {
		return 0, err
	}

	if description == "" {
		description = fmt.Sprintf("شحن رصيد: +%.2f جنيه", amountEGP)
	}

	_, err = tx.Exec(
		`INSERT INTO transactions (user_id, amount_egp, type, description, created_at) VALUES (?, ?, 'topup', ?, ?)`,
		userID, amountEGP, description, time.Now(),
	)
	if err != nil {
		return 0, err
	}

	if err := tx.Commit(); err != nil {
		return 0, err
	}

	return newBal, nil
}

// DeductBalance deducts the calculated request cost plus fixed margin from user balance.
func (s *Store) DeductBalance(userID int64, chatID string, costUSD float64, tokens int, description string) (float64, float64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	settings := s.getSettingsInternal()
	usdRate := settings.USDEquivalentEGP
	if usdRate <= 0 {
		usdRate = 55.0
	}
	fixedMargin := settings.FixedMarginEGP
	if fixedMargin < 0 {
		fixedMargin = 0.10
	}

	if costUSD <= 0 && tokens > 0 {
		costUSD = float64(tokens) * 0.0000003
	}

	deductedEGP := (costUSD * usdRate) + fixedMargin
	deductedEGP = math.Round(deductedEGP*10000) / 10000

	tx, err := s.db.Begin()
	if err != nil {
		return 0, 0, err
	}
	defer tx.Rollback()

	var currentBal float64
	row := tx.QueryRow(`SELECT balance_egp FROM users WHERE id = ?`, userID)
	if err := row.Scan(&currentBal); err != nil {
		return 0, 0, fmt.Errorf("user %d not found: %w", userID, err)
	}

	newBal := currentBal - deductedEGP
	newBal = math.Round(newBal*10000) / 10000
	if newBal < 0 {
		newBal = 0.0
	}

	_, err = tx.Exec(`UPDATE users SET balance_egp = ?, updated_at = ? WHERE id = ?`, newBal, time.Now(), userID)
	if err != nil {
		return 0, 0, err
	}

	if description == "" {
		description = "استفسار ذكاء اصطناعي"
	}

	_, err = tx.Exec(
		`INSERT INTO transactions (user_id, chat_id, amount_egp, type, cost_usd, tokens, description, created_at) VALUES (?, ?, ?, 'deduction', ?, ?, ?, ?)`,
		userID, chatID, -deductedEGP, costUSD, tokens, description, time.Now(),
	)
	if err != nil {
		return 0, 0, err
	}

	if err := tx.Commit(); err != nil {
		return 0, 0, err
	}

	return deductedEGP, newBal, nil
}

// GenerateLinkCode retrieves the user's permanent link code.
func (s *Store) GenerateLinkCode(userID int64) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var code string
	row := s.db.QueryRow(`SELECT COALESCE(link_code, '') FROM users WHERE id = ?`, userID)
	if err := row.Scan(&code); err != nil {
		return "", err
	}
	if code == "" {
		code = s.generateUniqueLinkCode()
		_, _ = s.db.Exec(`UPDATE users SET link_code = ? WHERE id = ?`, code, userID)
	}
	return code, nil
}

// VerifyAndLinkCode checks if a code matches a user's permanent link_code (or legacy code) and links the chat.
func (s *Store) VerifyAndLinkCode(code, chatID, chatType, chatName string) (*User, error) {
	code = strings.TrimSpace(code)
	chatID = strings.TrimSpace(chatID)
	if code == "" || chatID == "" {
		return nil, errors.New("invalid code or chatID")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	var u User
	// Check permanent link_code on users table first
	row := s.db.QueryRow(`SELECT id, username, balance_egp, COALESCE(link_code, ''), created_at, updated_at FROM users WHERE link_code = ?`, code)
	err := row.Scan(&u.ID, &u.Username, &u.BalanceEGP, &u.LinkCode, &u.CreatedAt, &u.UpdatedAt)
	if err != nil {
		// Fallback check legacy link_codes table
		var legacyUserID int64
		legacyRow := s.db.QueryRow(`SELECT user_id FROM link_codes WHERE code = ?`, code)
		if legacyErr := legacyRow.Scan(&legacyUserID); legacyErr != nil {
			return nil, errors.New("كود الربط غير صحيح أو غير مسجل")
		}
		userRow := s.db.QueryRow(`SELECT id, username, balance_egp, COALESCE(link_code, ''), created_at, updated_at FROM users WHERE id = ?`, legacyUserID)
		if userErr := userRow.Scan(&u.ID, &u.Username, &u.BalanceEGP, &u.LinkCode, &u.CreatedAt, &u.UpdatedAt); userErr != nil {
			return nil, errors.New("المستخدم غير موجود")
		}
	}

	// Link chat to user
	_, err = s.db.Exec(
		`INSERT INTO linked_chats (user_id, chat_id, chat_type, chat_name, created_at)
		 VALUES (?, ?, ?, ?, ?)
		 ON CONFLICT(chat_id) DO UPDATE SET user_id=excluded.user_id, chat_name=excluded.chat_name`,
		u.ID, chatID, chatType, chatName, time.Now(),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to link chat: %w", err)
	}

	return &u, nil
}

// GetLinkedUserForChat returns the user associated with a WhatsApp chat or sender ID.
func (s *Store) GetLinkedUserForChat(chatID string) (*User, error) {
	chatID = strings.TrimSpace(chatID)
	if chatID == "" {
		return nil, errors.New("empty chatID")
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	var u User
	row := s.db.QueryRow(`
		SELECT u.id, u.username, u.balance_egp, COALESCE(u.link_code, ''), u.created_at, u.updated_at
		FROM users u
		JOIN linked_chats lc ON u.id = lc.user_id
		WHERE lc.chat_id = ?
	`, chatID)

	if err := row.Scan(&u.ID, &u.Username, &u.BalanceEGP, &u.LinkCode, &u.CreatedAt, &u.UpdatedAt); err != nil {
		return nil, err
	}

	return &u, nil
}

// WebSessionData represents session state stored in SQLite for reboot persistence.
type WebSessionData struct {
	Token     string    `json:"token"`
	UserID    int64     `json:"user_id"`
	Username  string    `json:"username"`
	IsAdmin   bool      `json:"is_admin"`
	ExpiresAt time.Time `json:"expires_at"`
}

// SaveWebSession saves or updates a web session token in SQLite.
func (s *Store) SaveWebSession(token string, userID int64, username string, isAdmin bool, expiresAt time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(`
		INSERT INTO web_sessions (token, user_id, username, is_admin, expires_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(token) DO UPDATE SET user_id=excluded.user_id, username=excluded.username, is_admin=excluded.is_admin, expires_at=excluded.expires_at
	`, token, userID, username, isAdmin, expiresAt)
	return err
}

// GetWebSession retrieves a session from SQLite if not expired.
func (s *Store) GetWebSession(token string) (WebSessionData, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var data WebSessionData
	row := s.db.QueryRow(`SELECT token, user_id, username, is_admin, expires_at FROM web_sessions WHERE token = ?`, token)
	if err := row.Scan(&data.Token, &data.UserID, &data.Username, &data.IsAdmin, &data.ExpiresAt); err != nil {
		return WebSessionData{}, false
	}

	if time.Now().After(data.ExpiresAt) {
		go func(tok string) {
			s.mu.Lock()
			_, _ = s.db.Exec(`DELETE FROM web_sessions WHERE token = ?`, tok)
			s.mu.Unlock()
		}(token)
		return WebSessionData{}, false
	}

	return data, true
}

// DeleteWebSession removes a session from SQLite.
func (s *Store) DeleteWebSession(token string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(`DELETE FROM web_sessions WHERE token = ?`, token)
	return err
}

// GetLinkedChatsForUser returns all WhatsApp chats linked to a specific user.
func (s *Store) GetLinkedChatsForUser(userID int64) ([]LinkedChat, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.getLinkedChatsForUserInternal(userID)
}

func (s *Store) getLinkedChatsForUserInternal(userID int64) ([]LinkedChat, error) {
	rows, err := s.db.Query(`SELECT id, user_id, chat_id, chat_type, chat_name, created_at FROM linked_chats WHERE user_id = ?`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var chats []LinkedChat
	for rows.Next() {
		var c LinkedChat
		if err := rows.Scan(&c.ID, &c.UserID, &c.ChatID, &c.ChatType, &c.ChatName, &c.CreatedAt); err == nil {
			chats = append(chats, c)
		}
	}
	return chats, nil
}

// GetUserTransactions returns the latest transactions for a user.
func (s *Store) GetUserTransactions(userID int64, limit int) ([]Transaction, error) {
	if limit <= 0 {
		limit = 50
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	rows, err := s.db.Query(
		`SELECT id, user_id, chat_id, amount_egp, type, cost_usd, tokens, description, created_at
		 FROM transactions WHERE user_id = ? ORDER BY id DESC LIMIT ?`,
		userID, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []Transaction
	for rows.Next() {
		var t Transaction
		if err := rows.Scan(&t.ID, &t.UserID, &t.ChatID, &t.AmountEGP, &t.Type, &t.CostUSD, &t.Tokens, &t.Description, &t.CreatedAt); err == nil {
			list = append(list, t)
		}
	}
	return list, nil
}

// GetSettings retrieves current platform settings.
func (s *Store) GetSettings() (*PlatformSettings, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	cfg := s.getSettingsInternal()
	return &cfg, nil
}

func (s *Store) getSettingsInternal() PlatformSettings {
	cfg := PlatformSettings{
		USDEquivalentEGP:    55.0,
		FixedMarginEGP:      0.10,
		EnforcePaid:         true,
		AdminPassword:       "nova_admin_2026",
		ModelChat:           "gemini-3.8-flash",
		ModelMath:           "gemini-3.8-flash",
		ModelVision:         "gemini-3.8-flash",
		ModelAcademic:       "gemini-3.8-flash",
		ModelSummarizer:     "gemini-3.8-flash",
		TTSModel:            "gemini-3.8-flash-tts",
		TTSVoice:            "Charon",
		RechargeInstapay:     "makari@instapay",
		RechargeVodafone:     "01202172699",
		RechargeInstructions: "لشحن رصيدك عبر إنستاباي أو فودافون كاش، قم بالتحويل للرقم الموضح ثم تواصل لتأكيد الإيداع وإضافة الرصيد لحسابك فوراً.",
	}

	rows, err := s.db.Query(`SELECT key, value FROM settings`)
	if err != nil {
		return cfg
	}
	defer rows.Close()

	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err == nil {
			switch k {
			case "usd_rate":
				var f float64
				if _, err := fmt.Sscanf(v, "%f", &f); err == nil && f > 0 {
					cfg.USDEquivalentEGP = f
				}
			case "fixed_margin_egp":
				var f float64
				if _, err := fmt.Sscanf(v, "%f", &f); err == nil && f >= 0 {
					cfg.FixedMarginEGP = f
				}
			case "enforce_paid":
				cfg.EnforcePaid = strings.ToLower(v) == "true" || v == "1"
			case "admin_password":
				if v != "" {
					cfg.AdminPassword = v
				}
			case "model_chat":
				if v != "" {
					cfg.ModelChat = v
				}
			case "model_math":
				if v != "" {
					cfg.ModelMath = v
				}
			case "model_vision":
				if v != "" {
					cfg.ModelVision = v
				}
			case "model_academic":
				if v != "" {
					cfg.ModelAcademic = v
				}
			case "model_summarizer":
				if v != "" {
					cfg.ModelSummarizer = v
				}
			case "tts_model":
				if v != "" {
					cfg.TTSModel = v
				}
			case "tts_voice":
				if v != "" {
					cfg.TTSVoice = v
				}
			case "recharge_instapay":
				cfg.RechargeInstapay = v
			case "recharge_vodafone":
				cfg.RechargeVodafone = v
			case "recharge_instructions":
				cfg.RechargeInstructions = v
			}
		}
	}

	if cfg.USDtoEGPRate <= 0 {
		cfg.USDtoEGPRate = cfg.USDEquivalentEGP
	}

	return cfg
}

// UpdateSettings updates the platform configuration keys in SQLite.
func (s *Store) UpdateSettings(cfg *PlatformSettings) error {
	if cfg == nil {
		return errors.New("nil settings")
	}

	if cfg.USDtoEGPRate > 0 {
		cfg.USDEquivalentEGP = cfg.USDtoEGPRate
	}
	if cfg.USDEquivalentEGP <= 0 {
		cfg.USDEquivalentEGP = 55.0
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	updates := map[string]string{
		"usd_rate":              fmt.Sprintf("%.2f", cfg.USDEquivalentEGP),
		"fixed_margin_egp":      fmt.Sprintf("%.4f", cfg.FixedMarginEGP),
		"enforce_paid":          fmt.Sprintf("%t", cfg.EnforcePaid),
		"admin_password":        cfg.AdminPassword,
		"model_chat":            cfg.ModelChat,
		"model_math":            cfg.ModelMath,
		"model_vision":          cfg.ModelVision,
		"model_academic":        cfg.ModelAcademic,
		"model_summarizer":      cfg.ModelSummarizer,
		"tts_model":             cfg.TTSModel,
		"tts_voice":             cfg.TTSVoice,
		"recharge_instapay":     cfg.RechargeInstapay,
		"recharge_vodafone":     cfg.RechargeVodafone,
		"recharge_instructions": cfg.RechargeInstructions,
	}

	for k, v := range updates {
		_, err := s.db.Exec(`INSERT INTO settings (key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, k, v)
		if err != nil {
			return err
		}
	}

	return nil
}
