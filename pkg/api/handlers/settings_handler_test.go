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
	_, h, uid := setupSettingsHandlerTest(t)

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
}

func TestSaveSectorTargets_ThenGet(t *testing.T) {
	t.Parallel()
	_, h, uid := setupSettingsHandlerTest(t)

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
		t.Errorf("first row: got %+v", got.Rows[0])
	}
	if got.Rows[1].Sector != "Cash" || got.Rows[1].Min != 8 || got.Rows[1].Max != 12 {
		t.Errorf("second row: got %+v", got.Rows[1])
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
