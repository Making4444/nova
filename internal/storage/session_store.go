package storage

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"go.mau.fi/whatsmeow/store/sqlstore"
	waLog "go.mau.fi/whatsmeow/util/log"
	_ "modernc.org/sqlite"
)

// InitSessionStore initializes the SQLite container for whatsmeow session data.
func InitSessionStore(ctx context.Context, dbPath string, logger waLog.Logger) (*sqlstore.Container, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	dir := filepath.Dir(dbPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create directory for session db: %w", err)
	}

	// Use modernc sqlite driver registered under "sqlite"
	dbURI := fmt.Sprintf("file:%s?_pragma=foreign_keys(1)&_pragma=busy_timeout(10000)&_pragma=journal_mode(WAL)", dbPath)
	container, err := sqlstore.New(ctx, "sqlite", dbURI, logger)
	if err != nil {
		return nil, fmt.Errorf("failed to open whatsmeow session store: %w", err)
	}

	return container, nil
}

