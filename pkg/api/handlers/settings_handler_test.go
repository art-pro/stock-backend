package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/art-pro/stock-backend/pkg/models"
	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func setupSettingsHandlerTest(t *testing.T) (*gorm.DB, *SettingsHandler, uint) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	dbPath := filepath.Join(t.TempDir(), "settings-handler-test.db")
	db, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&models.User{}, &models.UserSettings{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	user := models.User{Username: "testuser", Password: "hashed"}
	if err := db.Create(&user).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}
	h := NewSettingsHandler(db, zerolog.Nop())
	return db, h, user.ID
}

func TestGetSectorTargets_NotFound(t *testing.T) {
	t.Parallel()
	db, h, uid := setupSettingsHandlerTest(t)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Set("user_id", uid)
	c.Request = httptest.NewRequest(http.MethodGet, "/settings/sector-targets", nil)

	h.GetSectorTargets(c)

	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d want 200", w.Code)
	}
	var out struct {
		Rows interface{} `json:"rows"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out.Rows != nil {
		t.Errorf("rows: got %v want nil", out.Rows)
	}

	// Verify no settings record was created
	var count int64
	db.Model(&models.UserSettings{}).Where("user_id = ? AND setting_key = ?", uid, "sector_targets").Count(&count)
	if count != 0 {
		t.Errorf("expected no settings record, got count %d", count)
	}
}

func TestSaveSectorTargets_ThenGet(t *testing.T) {
	t.Parallel()
	db, h, uid := setupSettingsHandlerTest(t)

	payload := SectorTargetsPayload{
		Rows: []SectorTargetRow{
			{Sector: "Healthcare", Min: 25, Max: 30, Rationale: "Resilient EV."},
			{Sector: "Cash", Min: 8, Max: 12, Rationale: "Dry powder."},
		},
	}
	body, _ := json.Marshal(payload)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Set("user_id", uid)
	c.Request = httptest.NewRequest(http.MethodPost, "/settings/sector-targets", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")

	h.SaveSectorTargets(c)

	if w.Code != http.StatusOK {
		t.Fatalf("save status: got %d want 200, body: %s", w.Code, w.Body.Bytes())
	}

	// Verify settings record was created in DB
	var setting models.UserSettings
	if err := db.Where("user_id = ? AND setting_key = ?", uid, "sector_targets").First(&setting).Error; err != nil {
		t.Fatalf("find settings record: %v", err)
	}
	if setting.Value == "" {
		t.Errorf("expected non-empty setting value")
	}

	w2 := httptest.NewRecorder()
	c2, _ := gin.CreateTestContext(w2)
	c2.Set("user_id", uid)
	c2.Request = httptest.NewRequest(http.MethodGet, "/settings/sector-targets", nil)

	h.GetSectorTargets(c2)

	if w2.Code != http.StatusOK {
		t.Fatalf("get status: got %d want 200", w2.Code)
	}
	var got SectorTargetsPayload
	if err := json.Unmarshal(w2.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode get response: %v", err)
	}
	if len(got.Rows) != 2 {
		t.Fatalf("rows length: got %d want 2", len(got.Rows))
	}
	if got.Rows[0].Sector != "Healthcare" || got.Rows[0].Min != 25 || got.Rows[0].Max != 30 {
		t.Errorf("first row: got %+v want {Healthcare 25 30 Resilient EV.}", got.Rows[0])
	}
	if got.Rows[0].Rationale != "Resilient EV." {
		t.Errorf("first row rationale: got %s want 'Resilient EV.'", got.Rows[0].Rationale)
	}
	if got.Rows[1].Sector != "Cash" || got.Rows[1].Min != 8 || got.Rows[1].Max != 12 {
		t.Errorf("second row: got %+v want {Cash 8 12 Dry powder.}", got.Rows[1])
	}
	if got.Rows[1].Rationale != "Dry powder." {
		t.Errorf("second row rationale: got %s want 'Dry powder.'", got.Rows[1].Rationale)
	}
}

func TestSaveSectorTargets_EmptyRows_Returns400(t *testing.T) {
	t.Parallel()
	_, h, uid := setupSettingsHandlerTest(t)

	body := []byte(`{"rows":[]}`)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Set("user_id", uid)
	c.Request = httptest.NewRequest(http.MethodPost, "/settings/sector-targets", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")

	h.SaveSectorTargets(c)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status: got %d want 400", w.Code)
	}
}

func TestGetSectorTargets_NoUserID_Returns401(t *testing.T) {
	t.Parallel()
	_, h, _ := setupSettingsHandlerTest(t)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/settings/sector-targets", nil)

	h.GetSectorTargets(c)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status: got %d want 401", w.Code)
	}
}

func TestSaveSectorTargets_NoUserID_Returns401(t *testing.T) {
	t.Parallel()
	_, h, _ := setupSettingsHandlerTest(t)

	payload := SectorTargetsPayload{
		Rows: []SectorTargetRow{
			{Sector: "Technology", Min: 20, Max: 30, Rationale: "Growth"},
		},
	}
	body, _ := json.Marshal(payload)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/settings/sector-targets", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")

	h.SaveSectorTargets(c)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status: got %d want 401", w.Code)
	}
}

func TestSaveSectorTargets_UpdateExisting(t *testing.T) {
	t.Parallel()
	db, h, uid := setupSettingsHandlerTest(t)

	// First save
	payload1 := SectorTargetsPayload{
		Rows: []SectorTargetRow{
			{Sector: "Technology", Min: 20, Max: 30, Rationale: "Initial"},
		},
	}
	body1, _ := json.Marshal(payload1)

	w1 := httptest.NewRecorder()
	c1, _ := gin.CreateTestContext(w1)
	c1.Set("user_id", uid)
	c1.Request = httptest.NewRequest(http.MethodPost, "/settings/sector-targets", bytes.NewReader(body1))
	c1.Request.Header.Set("Content-Type", "application/json")

	h.SaveSectorTargets(c1)

	if w1.Code != http.StatusOK {
		t.Fatalf("first save status: got %d want 200", w1.Code)
	}

	// Second save (update)
	payload2 := SectorTargetsPayload{
		Rows: []SectorTargetRow{
			{Sector: "Technology", Min: 25, Max: 35, Rationale: "Updated"},
			{Sector: "Healthcare", Min: 15, Max: 20, Rationale: "Added"},
		},
	}
	body2, _ := json.Marshal(payload2)

	w2 := httptest.NewRecorder()
	c2, _ := gin.CreateTestContext(w2)
	c2.Set("user_id", uid)
	c2.Request = httptest.NewRequest(http.MethodPost, "/settings/sector-targets", bytes.NewReader(body2))
	c2.Request.Header.Set("Content-Type", "application/json")

	h.SaveSectorTargets(c2)

	if w2.Code != http.StatusOK {
		t.Fatalf("second save status: got %d want 200", w2.Code)
	}

	// Verify only one settings record exists
	var count int64
	db.Model(&models.UserSettings{}).Where("user_id = ? AND setting_key = ?", uid, "sector_targets").Count(&count)
	if count != 1 {
		t.Errorf("expected 1 settings record, got %d", count)
	}

	// Verify updated values
	w3 := httptest.NewRecorder()
	c3, _ := gin.CreateTestContext(w3)
	c3.Set("user_id", uid)
	c3.Request = httptest.NewRequest(http.MethodGet, "/settings/sector-targets", nil)

	h.GetSectorTargets(c3)

	if w3.Code != http.StatusOK {
		t.Fatalf("get status: got %d want 200", w3.Code)
	}
	var got SectorTargetsPayload
	if err := json.Unmarshal(w3.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got.Rows) != 2 {
		t.Fatalf("rows length: got %d want 2", len(got.Rows))
	}
	if got.Rows[0].Min != 25 || got.Rows[0].Max != 35 || got.Rows[0].Rationale != "Updated" {
		t.Errorf("Technology row not updated: got %+v", got.Rows[0])
	}
}

func TestSaveSectorTargets_InvalidJSON_Returns400(t *testing.T) {
	t.Parallel()
	_, h, uid := setupSettingsHandlerTest(t)

	body := []byte(`{"rows": "not an array"}`)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Set("user_id", uid)
	c.Request = httptest.NewRequest(http.MethodPost, "/settings/sector-targets", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")

	h.SaveSectorTargets(c)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status: got %d want 400", w.Code)
	}
}
