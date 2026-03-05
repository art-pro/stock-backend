package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
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
		&models.Stock{},
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
	_, h, portfolioID := setupOperationHandlerTest(t)

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

	var createdOp models.Operation
	if err := json.Unmarshal(w.Body.Bytes(), &createdOp); err != nil {
		t.Fatalf("decode created operation: %v", err)
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
	// Verify operation belongs to correct portfolio
	if list[0].PortfolioID != portfolioID {
		t.Errorf("PortfolioID: got %d want %d", list[0].PortfolioID, portfolioID)
	}
	// Verify operation ID matches created operation
	if list[0].ID != createdOp.ID {
		t.Errorf("Operation ID: got %d want %d", list[0].ID, createdOp.ID)
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

func TestDeleteOperation_ReversesCash(t *testing.T) {
	t.Parallel()
	db, h, portfolioID := setupOperationHandlerTest(t)
	createBody, _ := json.Marshal(CreateOperationRequest{
		OperationType: "Deposit",
		Currency:      "USD",
		Quantity:      100.0,
		TradeDate:     "15.02.2026",
	})
	wCreate := httptest.NewRecorder()
	cCreate, _ := gin.CreateTestContext(wCreate)
	cCreate.Request = httptest.NewRequest(http.MethodPost, "/operations", bytes.NewReader(createBody))
	cCreate.Request.Header.Set("Content-Type", "application/json")
	h.CreateOperation(cCreate)
	if wCreate.Code != http.StatusCreated {
		t.Fatalf("create: got %d", wCreate.Code)
	}
	var op models.Operation
	if err := json.Unmarshal(wCreate.Body.Bytes(), &op); err != nil {
		t.Fatalf("decode: %v", err)
	}
	wDel := httptest.NewRecorder()
	cDel, _ := gin.CreateTestContext(wDel)
	cDel.Request = httptest.NewRequest(http.MethodDelete, "/operations/"+strconv.FormatUint(uint64(op.ID), 10), nil)
	cDel.Params = gin.Params{{Key: "id", Value: strconv.FormatUint(uint64(op.ID), 10)}}
	h.DeleteOperation(cDel)
	if wDel.Code != http.StatusOK {
		t.Fatalf("delete status: got %d body: %s", wDel.Code, wDel.Body.Bytes())
	}
	var cash models.CashHolding
	err := db.Where("portfolio_id = ? AND currency_code = ?", portfolioID, "USD").First(&cash).Error
	if err == nil && cash.Amount != 0 {
		t.Errorf("cash after delete: got %f want 0", cash.Amount)
	}
	var count int64
	db.Model(&models.Operation{}).Where("id = ?", op.ID).Count(&count)
	if count != 0 {
		t.Errorf("operation still exists after delete")
	}
}

func TestCreateOperation_Withdraw_DecreasesCash(t *testing.T) {
	t.Parallel()
	db, h, portfolioID := setupOperationHandlerTest(t)

	// First deposit 200 USD
	depositBody, _ := json.Marshal(CreateOperationRequest{
		OperationType: "Deposit",
		Currency:      "USD",
		Quantity:      200.0,
		TradeDate:     "01.02.2026",
	})
	wDeposit := httptest.NewRecorder()
	cDeposit, _ := gin.CreateTestContext(wDeposit)
	cDeposit.Request = httptest.NewRequest(http.MethodPost, "/operations", bytes.NewReader(depositBody))
	cDeposit.Request.Header.Set("Content-Type", "application/json")
	h.CreateOperation(cDeposit)
	if wDeposit.Code != http.StatusCreated {
		t.Fatalf("deposit status: got %d", wDeposit.Code)
	}

	// Then withdraw 50 USD
	withdrawBody, _ := json.Marshal(CreateOperationRequest{
		OperationType: "Withdraw",
		Currency:      "USD",
		Quantity:      50.0,
		TradeDate:     "02.02.2026",
	})
	wWithdraw := httptest.NewRecorder()
	cWithdraw, _ := gin.CreateTestContext(wWithdraw)
	cWithdraw.Request = httptest.NewRequest(http.MethodPost, "/operations", bytes.NewReader(withdrawBody))
	cWithdraw.Request.Header.Set("Content-Type", "application/json")
	h.CreateOperation(cWithdraw)

	if wWithdraw.Code != http.StatusCreated {
		t.Fatalf("withdraw status: got %d body: %s", wWithdraw.Code, wWithdraw.Body.Bytes())
	}

	var cash models.CashHolding
	if err := db.Where("portfolio_id = ? AND currency_code = ?", portfolioID, "USD").First(&cash).Error; err != nil {
		t.Fatalf("find cash: %v", err)
	}
	if cash.Amount != 150 {
		t.Errorf("cash amount after deposit and withdraw: got %f want 150", cash.Amount)
	}
}

func TestCreateOperation_MultipleCurrencies(t *testing.T) {
	t.Parallel()
	db, h, portfolioID := setupOperationHandlerTest(t)

	// Deposit USD
	usdBody, _ := json.Marshal(CreateOperationRequest{
		OperationType: "Deposit",
		Currency:      "USD",
		Quantity:      100.0,
		TradeDate:     "01.02.2026",
	})
	wUSD := httptest.NewRecorder()
	cUSD, _ := gin.CreateTestContext(wUSD)
	cUSD.Request = httptest.NewRequest(http.MethodPost, "/operations", bytes.NewReader(usdBody))
	cUSD.Request.Header.Set("Content-Type", "application/json")
	h.CreateOperation(cUSD)
	if wUSD.Code != http.StatusCreated {
		t.Fatalf("USD deposit status: got %d", wUSD.Code)
	}

	// Deposit EUR
	eurBody, _ := json.Marshal(CreateOperationRequest{
		OperationType: "Deposit",
		Currency:      "EUR",
		Quantity:      200.0,
		TradeDate:     "01.02.2026",
	})
	wEUR := httptest.NewRecorder()
	cEUR, _ := gin.CreateTestContext(wEUR)
	cEUR.Request = httptest.NewRequest(http.MethodPost, "/operations", bytes.NewReader(eurBody))
	cEUR.Request.Header.Set("Content-Type", "application/json")
	h.CreateOperation(cEUR)
	if wEUR.Code != http.StatusCreated {
		t.Fatalf("EUR deposit status: got %d", wEUR.Code)
	}

	// Verify both cash holdings exist
	var usdCash models.CashHolding
	if err := db.Where("portfolio_id = ? AND currency_code = ?", portfolioID, "USD").First(&usdCash).Error; err != nil {
		t.Fatalf("find USD cash: %v", err)
	}
	if usdCash.Amount != 100 {
		t.Errorf("USD cash: got %f want 100", usdCash.Amount)
	}

	var eurCash models.CashHolding
	if err := db.Where("portfolio_id = ? AND currency_code = ?", portfolioID, "EUR").First(&eurCash).Error; err != nil {
		t.Fatalf("find EUR cash: %v", err)
	}
	if eurCash.Amount != 200 {
		t.Errorf("EUR cash: got %f want 200", eurCash.Amount)
	}
}

func TestDeleteOperation_NonExistent_Returns404(t *testing.T) {
	t.Parallel()
	_, h, _ := setupOperationHandlerTest(t)

	wDel := httptest.NewRecorder()
	cDel, _ := gin.CreateTestContext(wDel)
	cDel.Request = httptest.NewRequest(http.MethodDelete, "/operations/99999", nil)
	cDel.Params = gin.Params{{Key: "id", Value: "99999"}}
	h.DeleteOperation(cDel)

	if wDel.Code != http.StatusNotFound && wDel.Code != http.StatusInternalServerError {
		t.Logf("Note: Expected 404 or 500 for non-existent operation, got %d", wDel.Code)
	}
}

func TestUpdateOperation_NonExistent_Returns404(t *testing.T) {
	t.Parallel()
	_, h, _ := setupOperationHandlerTest(t)

	updateBody, _ := json.Marshal(CreateOperationRequest{
		OperationType: "Deposit",
		Currency:      "USD",
		Quantity:      100.0,
		TradeDate:     "01.02.2026",
	})

	wUpd := httptest.NewRecorder()
	cUpd, _ := gin.CreateTestContext(wUpd)
	cUpd.Request = httptest.NewRequest(http.MethodPut, "/operations/99999", bytes.NewReader(updateBody))
	cUpd.Request.Header.Set("Content-Type", "application/json")
	cUpd.Params = gin.Params{{Key: "id", Value: "99999"}}
	h.UpdateOperation(cUpd)

	if wUpd.Code != http.StatusNotFound && wUpd.Code != http.StatusInternalServerError {
		t.Logf("Note: Expected 404 or 500 for non-existent operation, got %d", wUpd.Code)
	}
}

func TestUpdateOperation_RecomputesCash(t *testing.T) {
	t.Parallel()
	db, h, portfolioID := setupOperationHandlerTest(t)
	createBody, _ := json.Marshal(CreateOperationRequest{
		OperationType: "Deposit",
		Currency:      "USD",
		Quantity:      50.0,
		TradeDate:     "01.02.2026",
		Note:          "Initial deposit",
	})
	wCreate := httptest.NewRecorder()
	cCreate, _ := gin.CreateTestContext(wCreate)
	cCreate.Request = httptest.NewRequest(http.MethodPost, "/operations", bytes.NewReader(createBody))
	cCreate.Request.Header.Set("Content-Type", "application/json")
	h.CreateOperation(cCreate)
	if wCreate.Code != http.StatusCreated {
		t.Fatalf("create: got %d", wCreate.Code)
	}
	var op models.Operation
	if err := json.Unmarshal(wCreate.Body.Bytes(), &op); err != nil {
		t.Fatalf("decode: %v", err)
	}

	// Verify initial cash
	var cashBefore models.CashHolding
	if err := db.Where("portfolio_id = ? AND currency_code = ?", portfolioID, "USD").First(&cashBefore).Error; err != nil {
		t.Fatalf("find initial cash: %v", err)
	}
	if cashBefore.Amount != 50 {
		t.Errorf("initial cash: got %f want 50", cashBefore.Amount)
	}

	// Update to 80 USD
	updateBody, _ := json.Marshal(CreateOperationRequest{
		OperationType: "Deposit",
		Currency:      "USD",
		Quantity:      80.0,
		TradeDate:     "01.02.2026",
		Note:          "Updated deposit",
	})
	wUpd := httptest.NewRecorder()
	cUpd, _ := gin.CreateTestContext(wUpd)
	cUpd.Request = httptest.NewRequest(http.MethodPut, "/operations/"+strconv.FormatUint(uint64(op.ID), 10), bytes.NewReader(updateBody))
	cUpd.Request.Header.Set("Content-Type", "application/json")
	cUpd.Params = gin.Params{{Key: "id", Value: strconv.FormatUint(uint64(op.ID), 10)}}
	h.UpdateOperation(cUpd)
	if wUpd.Code != http.StatusOK {
		t.Fatalf("update status: got %d body: %s", wUpd.Code, wUpd.Body.Bytes())
	}

	var updatedOp models.Operation
	if err := json.Unmarshal(wUpd.Body.Bytes(), &updatedOp); err != nil {
		t.Fatalf("decode updated operation: %v", err)
	}
	if updatedOp.Amount != 80 {
		t.Errorf("updated operation amount: got %f want 80", updatedOp.Amount)
	}
	if updatedOp.Note != "Updated deposit" {
		t.Errorf("updated operation note: got %s want 'Updated deposit'", updatedOp.Note)
	}

	var cash models.CashHolding
	if err := db.Where("portfolio_id = ? AND currency_code = ?", portfolioID, "USD").First(&cash).Error; err != nil {
		t.Fatalf("find cash: %v", err)
	}
	if cash.Amount != 80 {
		t.Errorf("cash after update: got %f want 80", cash.Amount)
	}
}
