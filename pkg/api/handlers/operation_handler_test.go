package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/art-pro/stock-backend/pkg/config"
	"github.com/art-pro/stock-backend/pkg/models"
	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func setupOperationHandlerTest(t *testing.T) (*gorm.DB, *OperationHandler, uint) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	dbPath := filepath.Join(t.TempDir(), "operation-handler-test.db")
	db, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(
		&models.User{},
		&models.Portfolio{},
		&models.ExchangeRate{},
		&models.CashHolding{},
		&models.Operation{},
	); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	user := models.User{Username: "testuser", Password: "hashed"}
	if err := db.Create(&user).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}
	portfolio := models.Portfolio{Name: "Default", UserID: user.ID, IsDefault: true}
	if err := db.Create(&portfolio).Error; err != nil {
		t.Fatalf("create portfolio: %v", err)
	}
	now := time.Now()
	for _, code := range []string{"USD", "EUR"} {
		rate := 1.0
		if code == "USD" {
			rate = 1.08
		}
		if err := db.Create(&models.ExchangeRate{
			CurrencyCode: code,
			Rate:         rate,
			LastUpdated:  now,
			IsActive:     true,
		}).Error; err != nil {
			t.Fatalf("create exchange rate %s: %v", code, err)
		}
	}
	cfg := &config.Config{}
	cashHandler := NewCashHandler(db, cfg, zerolog.Nop())
	opHandler := NewOperationHandler(db, cashHandler, zerolog.Nop())
	return db, opHandler, portfolio.ID
}

func TestCreateOperation_Deposit_IncreasesCash(t *testing.T) {
	t.Parallel()
	db, h, portfolioID := setupOperationHandlerTest(t)

	payload := CreateOperationRequest{
		OperationType: "Deposit",
		Currency:      "USD",
		Quantity:      100.0,
		TradeDate:     "15.02.2026",
		Note:          "Test deposit",
	}
	body, _ := json.Marshal(payload)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/operations", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")

	h.CreateOperation(c)

	if w.Code != http.StatusCreated {
		t.Fatalf("status: got %d want 201, body: %s", w.Code, w.Body.Bytes())
	}

	var op models.Operation
	if err := json.Unmarshal(w.Body.Bytes(), &op); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if op.OperationType != "Deposit" || op.Amount != 100 || op.Currency != "USD" {
		t.Errorf("operation: got type=%s amount=%f currency=%s", op.OperationType, op.Amount, op.Currency)
	}

	var cash models.CashHolding
	if err := db.Where("portfolio_id = ? AND currency_code = ?", portfolioID, "USD").First(&cash).Error; err != nil {
		t.Fatalf("find cash: %v", err)
	}
	if cash.Amount != 100 {
		t.Errorf("cash amount: got %f want 100", cash.Amount)
	}
}

func TestCreateOperation_Deposit_ThenList(t *testing.T) {
	t.Parallel()
	_, h, _ := setupOperationHandlerTest(t)

	payload := CreateOperationRequest{
		OperationType: "Deposit",
		Currency:      "USD",
		Quantity:      50.0,
		TradeDate:     "01.02.2026",
	}
	body, _ := json.Marshal(payload)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/operations", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")

	h.CreateOperation(c)
	if w.Code != http.StatusCreated {
		t.Fatalf("create status: got %d want 201, body: %s", w.Code, w.Body.Bytes())
	}

	w2 := httptest.NewRecorder()
	c2, _ := gin.CreateTestContext(w2)
	c2.Request = httptest.NewRequest(http.MethodGet, "/operations", nil)

	h.ListOperations(c2)
	if w2.Code != http.StatusOK {
		t.Fatalf("list status: got %d want 200", w2.Code)
	}
	var list []models.Operation
	if err := json.Unmarshal(w2.Body.Bytes(), &list); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("list length: got %d want 1", len(list))
	}
	if list[0].OperationType != "Deposit" || list[0].Amount != 50 {
		t.Errorf("list[0]: got type=%s amount=%f", list[0].OperationType, list[0].Amount)
	}
}

func TestCreateOperation_InvalidType_Returns400(t *testing.T) {
	t.Parallel()
	_, h, _ := setupOperationHandlerTest(t)

	payload := map[string]interface{}{
		"operation_type": "Invalid",
		"currency":       "USD",
		"quantity":       10.0,
		"trade_date":     "01.02.2026",
	}
	body, _ := json.Marshal(payload)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/operations", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")

	h.CreateOperation(c)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status: got %d want 400, body: %s", w.Code, w.Body.Bytes())
	}
}

func TestListOperations_Empty(t *testing.T) {
	t.Parallel()
	_, h, _ := setupOperationHandlerTest(t)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/operations", nil)

	h.ListOperations(c)
	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d want 200", w.Code)
	}
	var list []models.Operation
	if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("list length: got %d want 0", len(list))
	}
}
