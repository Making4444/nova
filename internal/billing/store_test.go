package billing

import (
	"os"
	"path/filepath"
	"testing"
)

func TestBillingStore_FullFlow(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "nova_billing_test_*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "billing.db")
	store, err := NewStore(dbPath)
	if err != nil {
		t.Fatalf("NewStore failed: %v", err)
	}
	defer store.Close()

	// 1. Register User
	user, err := store.RegisterUser("makari", "secret123")
	if err != nil {
		t.Fatalf("RegisterUser failed: %v", err)
	}
	if user.Username != "makari" || user.BalanceEGP != 0.0 {
		t.Errorf("unexpected user: %+v", user)
	}

	// 2. Authenticate User
	authUser, err := store.AuthenticateUser("makari", "secret123")
	if err != nil {
		t.Fatalf("AuthenticateUser failed: %v", err)
	}
	if authUser.ID != user.ID {
		t.Errorf("expected user ID %d, got %d", user.ID, authUser.ID)
	}

	// 3. Topup Balance
	newBal, err := store.TopupBalance(user.ID, 50.0, "شحن رصيد تجريبي")
	if err != nil {
		t.Fatalf("TopupBalance failed: %v", err)
	}
	if newBal != 50.0 {
		t.Errorf("expected balance 50.0, got %f", newBal)
	}

	// 4. Generate Link Code
	code, err := store.GenerateLinkCode(user.ID)
	if err != nil {
		t.Fatalf("GenerateLinkCode failed: %v", err)
	}
	if len(code) != 6 {
		t.Errorf("expected 6-digit code, got %s", code)
	}

	// 5. Verify & Link Code
	linkedUser, err := store.VerifyAndLinkCode(code, "201202172699@s.whatsapp.net", "direct", "Makari Chat")
	if err != nil {
		t.Fatalf("VerifyAndLinkCode failed: %v", err)
	}
	if linkedUser.ID != user.ID {
		t.Errorf("expected linked user ID %d, got %d", user.ID, linkedUser.ID)
	}

	// 6. Query Linked User
	fetchedUser, err := store.GetLinkedUserForChat("201202172699@s.whatsapp.net")
	if err != nil {
		t.Fatalf("GetLinkedUserForChat failed: %v", err)
	}
	if fetchedUser.Username != "makari" {
		t.Errorf("expected username makari, got %s", fetchedUser.Username)
	}

	// 7. Deduct Balance (Cost: $0.0001, Rate: 55, Margin: 0.10 EGP -> Deduct: 0.0055 + 0.10 = 0.1055 EGP)
	deducted, rem, err := store.DeductBalance(user.ID, "201202172699@s.whatsapp.net", 0.0001, 100, "سؤال تجريبي")
	if err != nil {
		t.Fatalf("DeductBalance failed: %v", err)
	}
	expectedDeducted := 0.1055
	if deducted != expectedDeducted {
		t.Errorf("expected deducted %f, got %f", expectedDeducted, deducted)
	}
	if rem != 50.0-expectedDeducted {
		t.Errorf("expected remaining %f, got %f", 50.0-expectedDeducted, rem)
	}

	// 8. Test Settings Update
	settings, err := store.GetSettings()
	if err != nil {
		t.Fatalf("GetSettings failed: %v", err)
	}
	if settings.USDEquivalentEGP != 55.0 || settings.FixedMarginEGP != 0.10 {
		t.Errorf("unexpected settings: %+v", settings)
	}

	settings.USDEquivalentEGP = 50.0
	settings.ModelChat = "gemini-3.8-flash"
	settings.TTSVoice = "Puck"
	if err := store.UpdateSettings(settings); err != nil {
		t.Fatalf("UpdateSettings failed: %v", err)
	}

	upSettings, _ := store.GetSettings()
	if upSettings.USDEquivalentEGP != 50.0 || upSettings.TTSVoice != "Puck" {
		t.Errorf("updated settings mismatch: %+v", upSettings)
	}
}
