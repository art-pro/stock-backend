package handlers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/art-pro/stock-backend/pkg/config"
	"github.com/art-pro/stock-backend/pkg/database"
	"github.com/art-pro/stock-backend/pkg/models"
	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog"
	"gorm.io/gorm"
)

// AssessmentHandler handles stock assessment requests
type AssessmentHandler struct {
	db     *gorm.DB
	cfg    *config.Config
	logger zerolog.Logger
	client *http.Client
}

// AssessmentRequest represents the request for stock assessment
type AssessmentRequest struct {
	Ticker               string  `json:"ticker" binding:"required"`
	Source               string  `json:"source" binding:"required,oneof=grok deepseek"`
	CompanyName          string  `json:"company_name,omitempty"`
	CurrentPrice         float64 `json:"current_price,omitempty"`
	Currency             string  `json:"currency,omitempty"`
	RebalanceHint        string  `json:"rebalance_hint,omitempty"`         // Dashboard: sector rebalance hint
	ConcentrationHint    string  `json:"concentration_hint,omitempty"`     // Dashboard: concentration & tail risk
	SuggestedActionsHint string  `json:"suggested_actions_hint,omitempty"` // Dashboard: suggested next actions
}

// AssessmentResponse represents the response containing assessment
type AssessmentResponse struct {
	Assessment string `json:"assessment"`
}

type AssessmentCompareRequest struct {
	Ticker             string `json:"ticker" binding:"required"`
	GrokAssessment     string `json:"grok_assessment" binding:"required"`
	DeepseekAssessment string `json:"deepseek_assessment" binding:"required"`
}

type AssessmentCompareRow struct {
	Key      string `json:"key"`
	Label    string `json:"label"`
	Grok     string `json:"grok"`
	Deepseek string `json:"deepseek"`
}

type assessmentCompareLLMResult struct {
	Grok     map[string]string `json:"grok"`
	Deepseek map[string]string `json:"deepseek"`
}

// NewAssessmentHandler creates a new assessment handler
func NewAssessmentHandler(db *gorm.DB, cfg *config.Config, logger zerolog.Logger) *AssessmentHandler {
	return &AssessmentHandler{
		db:     db,
		cfg:    cfg,
		logger: logger,
		client: &http.Client{
			Timeout: 120 * time.Second, // Longer timeout for AI analysis
		},
	}
}

// ExtractFromImagesRequest represents the request for image extraction
type ExtractFromImagesRequest struct {
	Images []string `json:"images" binding:"required,max=10"` // Max 10 images
	Source string   `json:"source,omitempty"`                 // "grok" or "deepseek"
}

const (
	maxImageCount     = 10
	maxImageSizeBytes = 10 * 1024 * 1024 // 10 MB per image (base64)
)

// ExtractFromImages extracts stock data from uploaded images
func (h *AssessmentHandler) ExtractFromImages(c *gin.Context) {
	var req ExtractFromImagesRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Validate image count
	if len(req.Images) > maxImageCount {
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("Maximum %d images allowed", maxImageCount)})
		return
	}

	// Validate individual image sizes
	for i, img := range req.Images {
		if len(img) > maxImageSizeBytes {
			c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("Image %d exceeds maximum size of 10 MB", i+1)})
			return
		}
	}

	h.logger.Info().Int("image_count", len(req.Images)).Str("source", req.Source).Msg("Processing images for stock extraction")

	// Create prompt for vision analysis
	prompt := `Extract stock portfolio data strictly from the provided screenshots. Do NOT use any external knowledge or data. Return ONLY a JSON array of objects with this exact schema:
[
  {
    "ticker": "SYMBOL",
    "company_name": "Company Name",
    "current_price": 123.45 (number),
    "shares_owned": 10 (number, optional - use 0 if not found)
  }
]
Rules:
1. Extract ONLY data visible in the image.
2. If a value (like shares owned) is not visible, use 0.
3. The "current_price" should be taken from the "Last" column if available.
4. Do not invent or hallucinate tickers or prices.
5. Return strictly raw JSON array, no markdown formatting.`

	var content string
	var err error

	// Determine which provider to use based on request and configuration
	// Default to Grok unless Deepseek is requested or Grok is not configured
	useGrok := true
	if req.Source == "deepseek" {
		useGrok = false
	} else if h.cfg.XAIAPIKey == "" && h.cfg.DeepseekAPIKey != "" {
		useGrok = false
	}

	if useGrok {
		if h.cfg.XAIAPIKey == "" {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Grok API key not configured"})
			return
		}
		content, err = h.extractWithGrokVision(req.Images, prompt)
	} else {
		if h.cfg.DeepseekAPIKey == "" {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Deepseek API key not configured"})
			return
		}
		content, err = h.extractWithDeepseekVision(req.Images, prompt)
	}

	if err != nil {
		h.logger.Error().Err(err).Bool("use_grok", useGrok).Msg("Vision processing failed")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to process images: " + err.Error()})
		return
	}

	// Clean up response (remove markdown code blocks if present)
	content = strings.TrimSpace(content)
	if strings.HasPrefix(content, "```json") {
		content = strings.TrimPrefix(content, "```json")
		content = strings.TrimSuffix(content, "```")
	} else if strings.HasPrefix(content, "```") {
		content = strings.TrimPrefix(content, "```")
		content = strings.TrimSuffix(content, "```")
	}
	content = strings.TrimSpace(content)

	// Parse result to ensure it's valid JSON
	var result interface{}
	if err := json.Unmarshal([]byte(content), &result); err != nil {
		h.logger.Error().Err(err).Str("content", content).Msg("Failed to parse extracted JSON")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to parse extracted data"})
		return
	}

	c.JSON(http.StatusOK, result)
}

// extractWithGrokVision uses Grok's vision capabilities to extract data
func (h *AssessmentHandler) extractWithGrokVision(images []string, prompt string) (string, error) {
	// Prepare messages with image content
	messages := []map[string]interface{}{
		{
			"role": "user",
			"content": []map[string]interface{}{
				{
					"type": "text",
					"text": prompt,
				},
			},
		},
	}

	// Add images to the content array
	contentList := messages[0]["content"].([]map[string]interface{})
	for _, imgBase64 := range images {
		// Ensure base64 string has data URI prefix
		if !strings.HasPrefix(imgBase64, "data:image") {
			// Assume jpeg if not specified, though frontend should send full data URI
			imgBase64 = "data:image/jpeg;base64," + imgBase64
		}

		contentList = append(contentList, map[string]interface{}{
			"type": "image_url",
			"image_url": map[string]string{
				"url": imgBase64,
			},
		})
	}
	messages[0]["content"] = contentList

	reqBody := map[string]interface{}{
		"model":       "grok-2-vision-latest", // Use appropriate vision model name
		"messages":    messages,
		"stream":      false,
		"temperature": 0.1, // Low temperature for data extraction
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequest("POST", "https://api.x.ai/v1/chat/completions", bytes.NewBuffer(jsonData))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+h.cfg.XAIAPIKey)

	resp, err := h.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to call Grok API: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("Grok API returned status %d: %s", resp.StatusCode, string(body))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response: %w", err)
	}

	var grokResp map[string]interface{}
	if err := json.Unmarshal(body, &grokResp); err != nil {
		return "", fmt.Errorf("failed to parse response: %w", err)
	}

	choices, ok := grokResp["choices"].([]interface{})
	if !ok || len(choices) == 0 {
		return "", fmt.Errorf("no choices in response")
	}

	choice, ok := choices[0].(map[string]interface{})
	if !ok {
		return "", fmt.Errorf("invalid choice format")
	}

	message, ok := choice["message"].(map[string]interface{})
	if !ok {
		return "", fmt.Errorf("invalid message format")
	}

	content, ok := message["content"].(string)
	if !ok {
		return "", fmt.Errorf("invalid content format")
	}

	return content, nil
}

// extractWithDeepseekVision uses Deepseek's capabilities to extract data from images
func (h *AssessmentHandler) extractWithDeepseekVision(images []string, prompt string) (string, error) {
	// Prepare messages with image content
	messages := []map[string]interface{}{
		{
			"role": "user",
			"content": []map[string]interface{}{
				{
					"type": "text",
					"text": prompt,
				},
			},
		},
	}

	// Add images to the content array
	contentList := messages[0]["content"].([]map[string]interface{})
	for _, imgBase64 := range images {
		// Ensure base64 string has data URI prefix
		if !strings.HasPrefix(imgBase64, "data:image") {
			// Assume jpeg if not specified, though frontend should send full data URI
			imgBase64 = "data:image/jpeg;base64," + imgBase64
		}

		contentList = append(contentList, map[string]interface{}{
			"type": "image_url",
			"image_url": map[string]string{
				"url": imgBase64,
			},
		})
	}
	messages[0]["content"] = contentList

	reqBody := map[string]interface{}{
		"model":       "deepseek-chat", // Assuming V3/multimodal capability
		"messages":    messages,
		"stream":      false,
		"temperature": 0.1,
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("failed to marshal request: %w", err)
	}

	// Deepseek API endpoint
	req, err := http.NewRequest("POST", "https://api.deepseek.com/chat/completions", bytes.NewBuffer(jsonData))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+h.cfg.DeepseekAPIKey)

	resp, err := h.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to call Deepseek API: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("Deepseek API returned status %d: %s", resp.StatusCode, string(body))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response: %w", err)
	}

	var deepseekResp map[string]interface{}
	if err := json.Unmarshal(body, &deepseekResp); err != nil {
		return "", fmt.Errorf("failed to parse response: %w", err)
	}

	choices, ok := deepseekResp["choices"].([]interface{})
	if !ok || len(choices) == 0 {
		return "", fmt.Errorf("no choices in response")
	}

	choice, ok := choices[0].(map[string]interface{})
	if !ok {
		return "", fmt.Errorf("invalid choice format")
	}

	message, ok := choice["message"].(map[string]interface{})
	if !ok {
		return "", fmt.Errorf("invalid message format")
	}

	content, ok := message["content"].(string)
	if !ok {
		return "", fmt.Errorf("invalid content format")
	}

	return content, nil
}

// RequestAssessment generates a stock assessment using AI
func (h *AssessmentHandler) RequestAssessment(c *gin.Context) {
	var req AssessmentRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Convert ticker to uppercase
	req.Ticker = strings.ToUpper(req.Ticker)

	h.logger.Info().
		Str("ticker", req.Ticker).
		Str("source", req.Source).
		Msg("Generating stock assessment")

	var assessment string
	var err error

	switch req.Source {
	case "grok":
		assessment, err = h.generateGrokAssessment(req.Ticker, req.CompanyName, req.CurrentPrice, req.Currency, req.RebalanceHint, req.ConcentrationHint, req.SuggestedActionsHint)
	case "deepseek":
		assessment, err = h.generateDeepseekAssessment(req.Ticker, req.CompanyName, req.CurrentPrice, req.Currency, req.RebalanceHint, req.ConcentrationHint, req.SuggestedActionsHint)
	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid source. Must be 'grok' or 'deepseek'"})
		return
	}

	if err != nil {
		h.logger.Error().Err(err).
			Str("ticker", req.Ticker).
			Str("source", req.Source).
			Msg("Failed to generate assessment")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to generate assessment: " + err.Error()})
		return
	}

	// Persist one latest assessment per ticker+source (replace old with new).
	if err := h.upsertAssessment(req.Ticker, req.Source, assessment); err != nil {
		h.logger.Error().Err(err).Msg("Failed to persist assessment")
		// Continue anyway - don't fail the request if DB persistence fails
	} else {
		// Rebuild and persist diff whenever a new source assessment is saved.
		if err := h.regenerateAndPersistAssessmentDiff(req.Ticker); err != nil {
			h.logger.Warn().Err(err).Str("ticker", req.Ticker).Msg("Failed to regenerate persisted assessment diff")
		}
	}

	c.JSON(http.StatusOK, AssessmentResponse{
		Assessment: assessment,
	})
}

// GetRecentAssessments returns recent assessments
func (h *AssessmentHandler) GetRecentAssessments(c *gin.Context) {
	var assessments []models.Assessment

	// Get the last 20 assessments, ordered by creation time
	if err := h.db.Order("created_at DESC").Limit(20).Find(&assessments).Error; err != nil {
		h.logger.Error().Err(err).Msg("Failed to fetch recent assessments")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch assessments"})
		return
	}

	c.JSON(http.StatusOK, assessments)
}

// GetAssessmentsByTicker returns saved assessments for a ticker (optionally filtered by source).
func (h *AssessmentHandler) GetAssessmentsByTicker(c *gin.Context) {
	ticker := strings.ToUpper(strings.TrimSpace(c.Param("ticker")))
	if ticker == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Ticker is required"})
		return
	}

	source := strings.ToLower(strings.TrimSpace(c.Query("source")))
	if source != "" && source != "grok" && source != "deepseek" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid source. Must be 'grok' or 'deepseek'"})
		return
	}

	limit := 50
	if rawLimit := strings.TrimSpace(c.Query("limit")); rawLimit != "" {
		parsedLimit, err := strconv.Atoi(rawLimit)
		if err != nil || parsedLimit <= 0 || parsedLimit > 200 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "limit must be an integer between 1 and 200"})
			return
		}
		limit = parsedLimit
	}

	query := h.db.Where("ticker = ?", ticker).Order("created_at DESC").Limit(limit)
	if source != "" {
		query = query.Where("source = ?", source)
	}

	var assessments []models.Assessment
	if err := query.Find(&assessments).Error; err != nil {
		h.logger.Error().Err(err).Str("ticker", ticker).Msg("Failed to fetch assessments by ticker")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch assessments"})
		return
	}

	c.JSON(http.StatusOK, assessments)
}

// GetAssessmentDiffByTicker returns the latest persisted Grok-vs-Deepseek diff for a ticker.
func (h *AssessmentHandler) GetAssessmentDiffByTicker(c *gin.Context) {
	ticker := strings.ToUpper(strings.TrimSpace(c.Param("ticker")))
	if ticker == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Ticker is required"})
		return
	}

	var diff models.AssessmentDiff
	if err := h.db.Where("ticker = ?", ticker).First(&diff).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			c.JSON(http.StatusOK, gin.H{"rows": []AssessmentCompareRow{}})
			return
		}
		h.logger.Error().Err(err).Str("ticker", ticker).Msg("Failed to fetch assessment diff by ticker")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch assessment diff"})
		return
	}

	var rows []AssessmentCompareRow
	if err := json.Unmarshal([]byte(diff.RowsJSON), &rows); err != nil {
		h.logger.Error().Err(err).Str("ticker", ticker).Msg("Failed to parse persisted assessment diff")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to parse assessment diff"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"rows": rows})
}

// CompareAssessments extracts comparable fields from Grok and Deepseek summaries.
func (h *AssessmentHandler) CompareAssessments(c *gin.Context) {
	var req AssessmentCompareRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	ticker := strings.ToUpper(strings.TrimSpace(req.Ticker))
	rows, err := h.extractAssessmentCompareRows(ticker, req.GrokAssessment, req.DeepseekAssessment)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to compare assessments: " + err.Error()})
		return
	}

	if err := h.persistAssessmentDiff(ticker, rows); err != nil {
		h.logger.Warn().Err(err).Str("ticker", ticker).Msg("Failed to persist assessment diff from compare endpoint")
	}

	c.JSON(http.StatusOK, gin.H{"rows": rows})
}

// GetAssessmentById returns a specific assessment by ID
func (h *AssessmentHandler) GetAssessmentById(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid assessment ID"})
		return
	}

	var assessment models.Assessment
	if err := h.db.First(&assessment, id).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			c.JSON(http.StatusNotFound, gin.H{"error": "Assessment not found"})
			return
		}
		h.logger.Error().Err(err).Msg("Failed to fetch assessment")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch assessment"})
		return
	}

	c.JSON(http.StatusOK, assessment)
}

// BatchAssessmentRequest is the request for batch assessment.
type BatchAssessmentRequest struct {
	Tickers []string `json:"tickers" binding:"required,max=10"`
	Source  string   `json:"source"` // "grok" or "deepseek", default grok
}

// BatchAssessmentItem is one result in the batch assessment response.
type BatchAssessmentItem struct {
	Ticker         string `json:"ticker"`
	AssessmentText string `json:"assessment_text"`
	Source         string `json:"source"`
}

// BatchAssessment runs LLM assessment for multiple tickers; returns text only (no DB write).
func (h *AssessmentHandler) BatchAssessment(c *gin.Context) {
	var req BatchAssessmentRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	source := strings.ToLower(strings.TrimSpace(req.Source))
	if source == "" {
		source = "grok"
	}
	if source != "grok" && source != "deepseek" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "source must be 'grok' or 'deepseek'"})
		return
	}
	if (source == "grok" && h.cfg.XAIAPIKey == "") || (source == "deepseek" && h.cfg.DeepseekAPIKey == "") {
		c.JSON(http.StatusBadRequest, gin.H{"error": "requested LLM API key not configured"})
		return
	}

	portfolioID, err := h.resolvePortfolioID(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid portfolio_id"})
		return
	}

	// Load stocks for context (ticker -> company name, price, currency)
	stocksByTicker := make(map[string]*models.Stock)
	var stocks []models.Stock
	if err := h.db.Where("portfolio_id = ? AND ticker IN ?", portfolioID, req.Tickers).Find(&stocks).Error; err == nil {
		for i := range stocks {
			stocksByTicker[stocks[i].Ticker] = &stocks[i]
		}
	}
	portfolioData, cashData, _ := h.fetchPortfolioContextForPortfolio(portfolioID)
	systemContent := "You are a financial advisor using a probabilistic strategy. Provide a concise stock assessment (EV, Kelly, Add/Hold/Trim/Sell, key risks). Use the most recent market data. One structured paragraph per ticker."

	var results []BatchAssessmentItem
	var mu sync.Mutex
	var wg sync.WaitGroup
	for _, ticker := range req.Tickers {
		ticker := ticker
		wg.Add(1)
		go func() {
			defer wg.Done()
			var companyName string
			var currentPrice float64
			var currency string
			if s := stocksByTicker[ticker]; s != nil {
				companyName = s.CompanyName
				currentPrice = s.CurrentPrice
				currency = s.Currency
			}
			prompt := h.buildAssessmentPrompt(ticker, companyName, currentPrice, currency, portfolioData, cashData, "", "", "")
			text, err := h.callChatCompletion(systemContent, prompt, source)
			if err != nil {
				h.logger.Warn().Err(err).Str("ticker", ticker).Msg("Batch assessment failed for ticker")
				text = "Assessment unavailable: " + err.Error()
			}
			mu.Lock()
			results = append(results, BatchAssessmentItem{Ticker: ticker, AssessmentText: text, Source: source})
			mu.Unlock()
		}()
	}
	wg.Wait()

	// Preserve request order
	order := make(map[string]int)
	for i, t := range req.Tickers {
		order[t] = i
	}
	// results may be in random order; sort by original ticker order
	sorted := make([]BatchAssessmentItem, len(results))
	for _, r := range results {
		sorted[order[r.Ticker]] = r
	}

	c.JSON(http.StatusOK, gin.H{"assessments": sorted})
}

// ExplainAssessmentRequest can identify a stock by ID or by ticker + metrics.
type ExplainAssessmentRequest struct {
	StockID     *uint    `json:"stock_id"`
	Ticker      string   `json:"ticker"`
	EV          *float64 `json:"ev"`
	Upside      *float64 `json:"upside"`
	Downside    *float64 `json:"downside"`
	Probability *float64 `json:"probability"`
	Assessment  string   `json:"assessment"`
}

// ExplainAssessment returns a short LLM explanation of why the model recommends Add/Hold/Trim/Sell.
func (h *AssessmentHandler) ExplainAssessment(c *gin.Context) {
	var req ExplainAssessmentRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	var ev, upside, downside, prob float64
	var ticker, assessment string
	source := "grok"
	if h.cfg.XAIAPIKey == "" {
		source = "deepseek"
	}
	if h.cfg.XAIAPIKey == "" && h.cfg.DeepseekAPIKey == "" {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "No LLM API key configured"})
		return
	}

	if req.StockID != nil {
		portfolioID, err := h.resolvePortfolioID(c)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid portfolio_id"})
			return
		}
		var stock models.Stock
		if err := h.db.Where("id = ? AND portfolio_id = ?", *req.StockID, portfolioID).First(&stock).Error; err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "Stock not found"})
			return
		}
		ticker = stock.Ticker
		ev = stock.ExpectedValue
		upside = stock.UpsidePotential
		downside = stock.DownsideRisk
		prob = stock.ProbabilityPositive
		assessment = stock.Assessment
	} else {
		if req.Ticker == "" || req.EV == nil || req.Upside == nil || req.Downside == nil || req.Probability == nil || req.Assessment == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Provide stock_id or (ticker, ev, upside, downside, probability, assessment)"})
			return
		}
		ticker = req.Ticker
		ev = *req.EV
		upside = *req.Upside
		downside = *req.Downside
		prob = *req.Probability
		assessment = req.Assessment
	}

	userContent := fmt.Sprintf(`Given the following metrics for %s:
- Expected value (EV): %.2f%%
- Upside potential: %.2f%%
- Downside risk: %.2f%%
- Probability of positive outcome: %.2f
- Recommendation: %s

In one short paragraph, explain why the model recommends this action (Add/Hold/Trim/Sell). Do not repeat the numbers; focus on the logic.`, ticker, ev, upside, downside, prob, assessment)

	systemContent := "You are a concise financial analyst. Explain the rationale behind a probabilistic investment recommendation in one short paragraph."
	text, err := h.callChatCompletion(systemContent, userContent, source)
	if err != nil {
		h.logger.Error().Err(err).Str("ticker", ticker).Msg("Explain assessment failed")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to generate explanation: " + err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"text": text})
}

// SectorSummaryRequest is the request for sector/theme summary.
type SectorSummaryRequest struct {
	PortfolioID *uint    `json:"portfolio_id"`
	Sector      string   `json:"sector"`
	Tickers     []string `json:"tickers"`
}

// SectorSummary returns a short LLM narrative for a sector or list of tickers.
func (h *AssessmentHandler) SectorSummary(c *gin.Context) {
	var req SectorSummaryRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	source := "grok"
	if h.cfg.XAIAPIKey == "" {
		source = "deepseek"
	}
	if h.cfg.XAIAPIKey == "" && h.cfg.DeepseekAPIKey == "" {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "No LLM API key configured"})
		return
	}

	var tickers []string
	var sectorName string
	if len(req.Tickers) > 0 {
		tickers = req.Tickers
		sectorName = req.Sector
		if sectorName == "" {
			sectorName = "selected tickers"
		}
	} else if req.Sector != "" {
		portfolioID, err := h.resolvePortfolioID(c)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid portfolio_id"})
			return
		}
		if req.PortfolioID != nil {
			portfolioID = *req.PortfolioID
		}
		var stocks []models.Stock
		if err := h.db.Where("portfolio_id = ? AND LOWER(sector) = LOWER(?)", portfolioID, req.Sector).Find(&stocks).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to load sector stocks"})
			return
		}
		for _, s := range stocks {
			tickers = append(tickers, s.Ticker)
		}
		sectorName = req.Sector
		if len(tickers) == 0 {
			c.JSON(http.StatusOK, gin.H{"text": "No stocks in this sector in the portfolio."})
			return
		}
	} else {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Provide sector or tickers"})
		return
	}

	if len(tickers) == 0 {
		c.JSON(http.StatusOK, gin.H{"text": "No tickers to summarise."})
		return
	}

	userContent := fmt.Sprintf(`Summarise the following sector/theme for the given tickers.

Sector/theme: %s
Tickers: %s

Provide a short narrative (2–4 sentences) covering: outlook, main risks, and how the sector fits with typical portfolio targets (diversification, sector limits). Use current market context.`, sectorName, strings.Join(tickers, ", "))

	systemContent := "You are a concise portfolio analyst. Summarise a sector or theme in a short narrative: outlook, risks, and fit with targets."
	text, err := h.callChatCompletion(systemContent, userContent, source)
	if err != nil {
		h.logger.Error().Err(err).Str("sector", sectorName).Msg("Sector summary failed")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to generate summary: " + err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"text": text})
}

// generateGrokAssessment generates assessment using Grok AI
func (h *AssessmentHandler) generateGrokAssessment(ticker, companyName string, currentPrice float64, currency string, rebalanceHint, concentrationHint, suggestedActionsHint string) (string, error) {
	if h.cfg.XAIAPIKey == "" {
		return "", fmt.Errorf("Grok AI API key not configured")
	}

	// Fetch portfolio data for context
	portfolioData, cashData, err := h.fetchPortfolioContext()
	if err != nil {
		h.logger.Warn().Err(err).Msg("Failed to fetch portfolio context, continuing without it")
	}

	// Create the comprehensive prompt based on your strategy (includes dashboard hints when provided)
	prompt := h.buildAssessmentPrompt(ticker, companyName, currentPrice, currency, portfolioData, cashData, rebalanceHint, concentrationHint, suggestedActionsHint)

	// Build Grok API request
	reqBody := map[string]interface{}{
		"model": "grok-4-1-fast-reasoning-latest",
		"messages": []map[string]string{
			{
				"role":    "system",
				"content": "You are a financial advisor and investment consultant using a probabilistic strategy. You provide detailed stock analysis following the Kelly Criterion framework. Always provide complete, structured analysis. Use the most recent market data available and indicate data freshness in your analysis.",
			},
			{
				"role":    "user",
				"content": prompt,
			},
		},
		"stream": false,
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequest("POST", "https://api.x.ai/v1/chat/completions", bytes.NewBuffer(jsonData))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+h.cfg.XAIAPIKey)

	resp, err := h.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to call Grok API: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("Grok API returned status %d: %s", resp.StatusCode, string(body))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response: %w", err)
	}

	var grokResp map[string]interface{}
	if err := json.Unmarshal(body, &grokResp); err != nil {
		return "", fmt.Errorf("failed to parse response: %w", err)
	}

	// Extract the content from the response
	choices, ok := grokResp["choices"].([]interface{})
	if !ok || len(choices) == 0 {
		return "", fmt.Errorf("no choices in response")
	}

	choice, ok := choices[0].(map[string]interface{})
	if !ok {
		return "", fmt.Errorf("invalid choice format")
	}

	message, ok := choice["message"].(map[string]interface{})
	if !ok {
		return "", fmt.Errorf("invalid message format")
	}

	content, ok := message["content"].(string)
	if !ok {
		return "", fmt.Errorf("invalid content format")
	}

	return content, nil
}

// generateDeepseekAssessment generates assessment using Deepseek AI
func (h *AssessmentHandler) generateDeepseekAssessment(ticker, companyName string, currentPrice float64, currency string, rebalanceHint, concentrationHint, suggestedActionsHint string) (string, error) {
	if h.cfg.DeepseekAPIKey == "" {
		return "", fmt.Errorf("Deepseek AI API key not configured")
	}

	// Fetch portfolio data for context
	portfolioData, cashData, err := h.fetchPortfolioContext()
	if err != nil {
		h.logger.Warn().Err(err).Msg("Failed to fetch portfolio context, continuing without it")
	}

	// Create the comprehensive prompt based on your strategy (includes dashboard hints when provided)
	prompt := h.buildAssessmentPrompt(ticker, companyName, currentPrice, currency, portfolioData, cashData, rebalanceHint, concentrationHint, suggestedActionsHint)

	// Build Deepseek API request
	reqBody := map[string]interface{}{
		"model": "deepseek-reasoner",
		"messages": []map[string]string{
			{
				"role":    "system",
				"content": "You are a financial advisor and investment consultant using a probabilistic strategy. You provide detailed stock analysis following the Kelly Criterion framework. Always provide complete, structured analysis. Use the most recent market data available and indicate data freshness in your analysis.",
			},
			{
				"role":    "user",
				"content": prompt,
			},
		},
		"stream": false,
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequest("POST", "https://api.deepseek.com/v1/chat/completions", bytes.NewBuffer(jsonData))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+h.cfg.DeepseekAPIKey)

	resp, err := h.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to call Deepseek API: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("Deepseek API returned status %d: %s", resp.StatusCode, string(body))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response: %w", err)
	}

	var deepseekResp map[string]interface{}
	if err := json.Unmarshal(body, &deepseekResp); err != nil {
		return "", fmt.Errorf("failed to parse response: %w", err)
	}

	// Extract the content from the response
	choices, ok := deepseekResp["choices"].([]interface{})
	if !ok || len(choices) == 0 {
		return "", fmt.Errorf("no choices in response")
	}

	choice, ok := choices[0].(map[string]interface{})
	if !ok {
		return "", fmt.Errorf("invalid choice format")
	}

	message, ok := choice["message"].(map[string]interface{})
	if !ok {
		return "", fmt.Errorf("invalid message format")
	}

	content, ok := message["content"].(string)
	if !ok {
		return "", fmt.Errorf("invalid content format")
	}

	return content, nil
}

// resolvePortfolioID returns portfolio_id from query or default.
func (h *AssessmentHandler) resolvePortfolioID(c *gin.Context) (uint, error) {
	if portfolioIDParam := c.Query("portfolio_id"); portfolioIDParam != "" {
		parsed, err := strconv.ParseUint(portfolioIDParam, 10, 32)
		if err != nil {
			return 0, err
		}
		return uint(parsed), nil
	}
	return database.GetDefaultPortfolioID(h.db)
}

// callChatCompletion calls Grok or Deepseek chat API and returns the assistant content.
func (h *AssessmentHandler) callChatCompletion(systemContent, userContent, source string) (string, error) {
	var url string
	var apiKey string
	var model string
	if source == "deepseek" {
		url = "https://api.deepseek.com/v1/chat/completions"
		apiKey = h.cfg.DeepseekAPIKey
		model = "deepseek-reasoner"
	} else {
		url = "https://api.x.ai/v1/chat/completions"
		apiKey = h.cfg.XAIAPIKey
		model = "grok-4-1-fast-reasoning-latest"
	}
	if apiKey == "" {
		return "", fmt.Errorf("%s API key not configured", source)
	}
	reqBody := map[string]interface{}{
		"model": model,
		"messages": []map[string]string{
			{"role": "system", "content": systemContent},
			{"role": "user", "content": userContent},
		},
		"stream": false,
	}
	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("marshal request: %w", err)
	}
	req, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)
	resp, err := h.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("call API: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("API status %d: %s", resp.StatusCode, string(body))
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read response: %w", err)
	}
	var parsed map[string]interface{}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return "", fmt.Errorf("parse response: %w", err)
	}
	choices, ok := parsed["choices"].([]interface{})
	if !ok || len(choices) == 0 {
		return "", fmt.Errorf("no choices in response")
	}
	choice, ok := choices[0].(map[string]interface{})
	if !ok {
		return "", fmt.Errorf("invalid choice format")
	}
	message, ok := choice["message"].(map[string]interface{})
	if !ok {
		return "", fmt.Errorf("invalid message format")
	}
	content, ok := message["content"].(string)
	if !ok {
		return "", fmt.Errorf("invalid content format")
	}
	return content, nil
}

// fetchPortfolioContext retrieves current portfolio and cash data for assessment context (all portfolios).
func (h *AssessmentHandler) fetchPortfolioContext() ([]models.Stock, []models.CashHolding, error) {
	var portfolioStocks []models.Stock
	if err := h.db.Where("shares_owned > 0").Find(&portfolioStocks).Error; err != nil {
		return nil, nil, fmt.Errorf("failed to fetch portfolio stocks: %w", err)
	}
	var cashHoldings []models.CashHolding
	if err := h.db.Find(&cashHoldings).Error; err != nil {
		return nil, nil, fmt.Errorf("failed to fetch cash holdings: %w", err)
	}
	return portfolioStocks, cashHoldings, nil
}

// fetchPortfolioContextForPortfolio retrieves portfolio and cash for a given portfolio_id.
func (h *AssessmentHandler) fetchPortfolioContextForPortfolio(portfolioID uint) ([]models.Stock, []models.CashHolding, error) {
	var portfolioStocks []models.Stock
	if err := h.db.Where("portfolio_id = ? AND shares_owned > 0", portfolioID).Find(&portfolioStocks).Error; err != nil {
		return nil, nil, fmt.Errorf("failed to fetch portfolio stocks: %w", err)
	}
	var cashHoldings []models.CashHolding
	if err := h.db.Where("portfolio_id = ?", portfolioID).Find(&cashHoldings).Error; err != nil {
		return nil, nil, fmt.Errorf("failed to fetch cash holdings: %w", err)
	}
	return portfolioStocks, cashHoldings, nil
}

// buildPortfolioContext creates a formatted string describing the current portfolio
func (h *AssessmentHandler) buildPortfolioContext(portfolio []models.Stock, cashHoldings []models.CashHolding) string {
	context := "\n\n## CURRENT PORTFOLIO CONTEXT\n\n"

	if len(portfolio) == 0 {
		context += "**Current Portfolio:** Empty (no owned stocks)\n\n"
	} else {
		context += "**Current Portfolio (Owned Stocks):**\n\n"
		context += "| Ticker | Company | Sector | Shares | Avg Price | Current Price | Position Value | Weight | EV | Assessment |\n"
		context += "|--------|---------|--------|--------|-----------|---------------|----------------|--------|----|------------|\n"

		totalPortfolioValue := 0.0
		for _, stock := range portfolio {
			positionValue := float64(stock.SharesOwned) * stock.CurrentPrice
			totalPortfolioValue += positionValue
		}

		sectorAllocations := make(map[string]float64)

		for _, stock := range portfolio {
			positionValue := float64(stock.SharesOwned) * stock.CurrentPrice
			weightPercent := (positionValue / totalPortfolioValue) * 100

			context += fmt.Sprintf("| %s | %s | %s | %d | €%.2f | €%.2f | €%.0f | %.1f%% | %.1f%% | %s |\n",
				stock.Ticker,
				stock.CompanyName,
				stock.Sector,
				stock.SharesOwned,
				stock.AvgPriceLocal,
				stock.CurrentPrice,
				positionValue,
				weightPercent,
				stock.ExpectedValue,
				stock.Assessment)

			// Track sector allocations
			sectorAllocations[stock.Sector] += weightPercent
		}

		context += "\n**Current Sector Allocations:**\n"
		for sector, allocation := range sectorAllocations {
			context += fmt.Sprintf("- %s: %.1f%%\n", sector, allocation)
		}
		context += fmt.Sprintf("\n**Total Portfolio Value:** €%.0f\n", totalPortfolioValue)
	}

	// Add cash holdings
	if len(cashHoldings) == 0 {
		context += "\n**Available Cash:** No cash holdings recorded\n"
	} else {
		context += "\n**Available Cash:**\n"
		totalCash := 0.0
		for _, cash := range cashHoldings {
			if cash.CurrencyCode == "EUR" {
				// For EUR (base currency), use actual amount
				context += fmt.Sprintf("- %s: %.0f (€%.0f)\n", cash.CurrencyCode, cash.Amount, cash.Amount)
				totalCash += cash.Amount
			} else {
				// For other currencies, show both original and EUR value
				context += fmt.Sprintf("- %s: %.0f (€%.0f)\n", cash.CurrencyCode, cash.Amount, cash.USDValue)
				totalCash += cash.USDValue
			}
		}
		context += fmt.Sprintf("\n**Total Available Cash:** €%.0f\n", totalCash)
	}

	context += "\n**IMPORTANT:** Consider this portfolio context when making recommendations. Analyze:\n"
	context += "- How this new position would affect sector diversification\n"
	context += "- Whether current sector allocations exceed targets (Healthcare 30-35%, Tech 15%, etc.)\n"
	context += "- If sufficient cash is available for the recommended position size\n"
	context += "- How this fits with the overall portfolio risk and Kelly utilization\n"

	return context
}

// buildAssessmentPrompt creates the comprehensive prompt for stock assessment
func (h *AssessmentHandler) buildAssessmentPrompt(ticker, companyName string, currentPrice float64, currency string, portfolio []models.Stock, cashHoldings []models.CashHolding, rebalanceHint, concentrationHint, suggestedActionsHint string) string {
	// Build portfolio context string
	portfolioContext := h.buildPortfolioContext(portfolio, cashHoldings)
	// Append dashboard hints when provided by the frontend (Sector rebalance hint, Concentration & tail risk, Suggested next actions)
	if rebalanceHint != "" || concentrationHint != "" || suggestedActionsHint != "" {
		portfolioContext += "\n\n## DASHBOARD HINTS (current portfolio state)\n\n"
		if rebalanceHint != "" {
			portfolioContext += "**Sector rebalance hint:** " + rebalanceHint + "\n\n"
		}
		if concentrationHint != "" {
			portfolioContext += "**Concentration & tail risk:** " + concentrationHint + "\n\n"
		}
		if suggestedActionsHint != "" {
			portfolioContext += "**Suggested next actions:** " + suggestedActionsHint + "\n\n"
		}
		portfolioContext += "Consider these hints when making recommendations (e.g. sector fit, concentration, and existing sell/trim/buy-zone actions).\n"
	}
	// Get current date
	currentDate := time.Now().Format("January 2, 2006")

	// Build additional stock info
	stockInfo := ""
	if companyName != "" {
		stockInfo += fmt.Sprintf("\n**Company Name:** %s", companyName)
	}
	if currentPrice > 0 && currency != "" {
		stockInfo += fmt.Sprintf("\n**Current Price:** %.2f %s (user-provided)", currentPrice, currency)
	}

	return fmt.Sprintf(`CURRENT DATE: %s
%s
IMPORTANT: Please use the most recent available market data and financial information. Access current stock prices, latest quarterly earnings, recent analyst reports, and up-to-date fundamental metrics. If any data appears outdated, please indicate when the information was last updated.

You are a financial advisor and investment consultant using a probabilistic strategy. For the stock %s, follow these steps:

1. Collect data: current price, fair value (median consensus target), upside %% = ((fair value - current price) / current price) * 100, downside %% (calibrate by beta: -15%% <0.5, -20%% 0.5–1, -25%% 1–1.5, -30%% >1.5), p (0.5–0.7 based on ratings), volatility, P/E, EPS growth, debt-to-EBITDA, dividend yield.

2. Calculate EV = (p * upside %%) + ((1-p) * downside %%).

3. Calculate b = upside %% / |downside %%|, Kelly f* = ((b * p) - (1-p)) / b, ½-Kelly = f*/2 capped at 15%%.

4. Assess: Add (EV >7%%), Hold (EV >0%%), Trim (EV <3%%), Sell (EV <0%%).

5. Recommend buy zone (prices for EV >7%%), laddered entries if Add. Align with sector targets (Healthcare 30–35%%, Tech 15%%, etc.).

Output in structured format with EV, Kelly, assessment, and notes. Use conservative p; avoid hype.

Core Philosophy:
My investment approach is built on probabilistic reasoning, expected value optimization, and risk control via the Kelly criterion. The strategy aims to maximize long-term portfolio growth while minimizing the probability of ruin. It is grounded in three key principles:

1. Probabilistic Thinking – all investment decisions are made by assessing probabilities, not certainties. Every scenario (growth, stagnation, decline) is assigned a probability rather than treated as binary "yes/no".

2. Expected Value (EV) – an investment is only valid if the expected value is positive, accounting for both the potential upside and downside.

3. Kelly Criterion (½-Kelly Implementation) – position sizing is determined mathematically based on the Kelly formula, but only half of the optimal position is used to limit drawdowns and smooth volatility.

Decision-Making Framework:
For every asset, the model should follow these steps:

Collect Fundamental and Market Data:
• Current price and fair value estimate
• Upside potential (%%) and downside risk (%%)
• Probability of positive outcome (p)
• Volatility (σ)
• P/E ratio, EPS growth rate, debt-to-EBITDA, dividend yield

Portfolio Construction Rules:
• Diversification: include multiple sectors with positive EV to capture the "long tail" of outperformers.
• Maximum single-position weight: 15%% (only for extremely high-conviction, low-volatility assets like Novo Nordisk).
• Typical range: 3–6%% per stock, depending on EV, volatility, and risk correlation.
• Avoid overexposure to any one sector, region, or currency.
• Cash buffer: always maintain 8–12%% of total portfolio in cash for high-EV opportunities during corrections.

Execution and Risk Management Rules:
1. Enter only within the defined "EV buy zone." Optimal buy zones correspond to the range where EV > 7%% and downside risk < 10%%. Avoid buying into EV < 3%% or after strong rallies.

2. Add positions gradually ("laddered entries"). Divide entries into 2–3 limit orders across a price range to average in probabilistically.

3. Never average down mechanically. Only average down if EV increases and probability of success remains >55%%.

4. Position trimming: If EV drops below +3%% (e.g., due to overvaluation), trim or take profits.

5. Portfolio rebalancing: Review weights quarterly. Maintain overall Kelly usage between 0.75–0.85 (not fully leveraged).

6. Hold cash strategically. Cash has optional value during corrections. Reinvest only when market-wide EV turns positive again.

Behavioral and Philosophical Anchors:
• Avoid emotional reactions to drawdowns. Evaluate situations through EV changes, not price changes.
• Loss ≠ mistake if EV was positive at entry. Focus on process, not short-term results.
• Never chase hype or "narratives." Wait for probabilistic edge.
• Diversify into "future rocket stocks" (2%% of positions) to capture asymmetric long-tail gains.

Target Portfolio Metrics:
Expected Value (EV): +10–11%% (Portfolio-wide mathematical expectation)
Volatility (σ): 11–13%% (Moderate risk level)
Sharpe Ratio (EV/σ): 0.8–0.9 (Efficient balance of risk/reward)
Kelly Utilization: 0.75–0.85 (Safe use of probabilistic leverage)
Max drawdown tolerance: ≤15%% (Controlled downside risk)

Summary Principle: "Every investment must be a probabilistic bet with a positive expected value, diversified across independent opportunities, and sized according to Kelly to maximize long-term growth without emotional interference."

Please provide a detailed assessment for %s following the template format similar to the NVIDIA analysis example, including:

- Step 1: Data Collection & Fundamental Analysis
- Step 2: Conservative Parameter Estimation
- Step 3: Expected Value Calculation
- Step 4: Kelly Criterion Sizing
- Step 5: Assessment
- Step 6: Buy Zone & Strategic Context
- Recommendation & Action Plan
- Risk Management Notes
- Final Assessment

Use real market data and provide specific numbers for all calculations. Be conservative with probability estimates and avoid hype.

%s`, currentDate, stockInfo, ticker, ticker, portfolioContext)
}

func assessmentCompareFieldSpec() []struct {
	Key   string
	Label string
} {
	return []struct {
		Key   string
		Label string
	}{
		{"current_price", "Current Price"},
		{"fair_value_estimate", "Fair Value Estimate"},
		{"upside_potential", "Upside Potential"},
		{"beta", "Beta"},
		{"downside_risk", "Downside Risk (D)"},
		{"probability_positive", "Probability of Positive Outcome (p)"},
		{"volatility", "Volatility (σ)"},
		{"forward_pe_ratio", "Forward P/E Ratio"},
		{"eps_growth", "EPS Growth"},
		{"debt_to_ebitda_ttm", "Debt-to-EBITDA (TTM)"},
		{"dividend_yield", "Dividend Yield"},
		{"expected_value_calculation", "Expected Value (EV) Calculation"},
		{"kelly_criterion_sizing", "Kelly Criterion Sizing"},
		{"buy_zone", "Buy Zone"},
		{"final_assessment", "Final Assessment"},
	}
}

func (h *AssessmentHandler) upsertAssessment(ticker, source, text string) error {
	ticker = strings.ToUpper(strings.TrimSpace(ticker))
	source = strings.ToLower(strings.TrimSpace(source))

	var existing models.Assessment
	err := h.db.Where("ticker = ? AND source = ?", ticker, source).First(&existing).Error
	if err == nil {
		if updateErr := h.db.Model(&existing).Updates(map[string]interface{}{
			"assessment": text,
			"status":     "completed",
			"updated_at": time.Now(),
		}).Error; updateErr != nil {
			return updateErr
		}
		// Clean up any legacy duplicates from older behavior.
		return h.db.Where("ticker = ? AND source = ? AND id <> ?", ticker, source, existing.ID).Delete(&models.Assessment{}).Error
	}
	if err != gorm.ErrRecordNotFound {
		return err
	}

	record := models.Assessment{
		Ticker:     ticker,
		Source:     source,
		Assessment: text,
		Status:     "completed",
		CreatedAt:  time.Now(),
	}
	return h.db.Create(&record).Error
}

func (h *AssessmentHandler) extractAssessmentCompareRows(ticker, grokAssessment, deepseekAssessment string) ([]AssessmentCompareRow, error) {
	source := "grok"
	if h.cfg.XAIAPIKey == "" && h.cfg.DeepseekAPIKey != "" {
		source = "deepseek"
	}
	if h.cfg.XAIAPIKey == "" && h.cfg.DeepseekAPIKey == "" {
		return nil, fmt.Errorf("No LLM API key configured")
	}

	systemContent := "You are a financial data extraction assistant. Extract only values explicitly present in text. If a field is absent, return 'N/A'. For final assessment return only ADD, SELL, or HOLD if clearly stated, otherwise N/A."
	userContent := fmt.Sprintf(`Extract the requested fields from two stock assessment summaries for ticker %s.

Return STRICT JSON with this exact shape:
{
  "grok": {
    "current_price": "...",
    "fair_value_estimate": "...",
    "upside_potential": "...",
    "beta": "...",
    "downside_risk": "...",
    "probability_positive": "...",
    "volatility": "...",
    "forward_pe_ratio": "...",
    "eps_growth": "...",
    "debt_to_ebitda_ttm": "...",
    "dividend_yield": "...",
    "expected_value_calculation": "...",
    "kelly_criterion_sizing": "...",
    "buy_zone": "...",
    "final_assessment": "ADD|SELL|HOLD|N/A"
  },
  "deepseek": {
    "current_price": "...",
    "fair_value_estimate": "...",
    "upside_potential": "...",
    "beta": "...",
    "downside_risk": "...",
    "probability_positive": "...",
    "volatility": "...",
    "forward_pe_ratio": "...",
    "eps_growth": "...",
    "debt_to_ebitda_ttm": "...",
    "dividend_yield": "...",
    "expected_value_calculation": "...",
    "kelly_criterion_sizing": "...",
    "buy_zone": "...",
    "final_assessment": "ADD|SELL|HOLD|N/A"
  }
}

Rules:
- Keep values compact and human-readable.
- Preserve units/percent signs if present.
- Do not invent missing data.
- Output raw JSON only, no markdown.

GROK SUMMARY:
%s

DEEPSEEK SUMMARY:
%s
`, ticker, grokAssessment, deepseekAssessment)

	content, err := h.callChatCompletion(systemContent, userContent, source)
	if err != nil {
		return nil, err
	}

	content = strings.TrimSpace(content)
	if strings.HasPrefix(content, "```json") {
		content = strings.TrimPrefix(content, "```json")
		content = strings.TrimSuffix(content, "```")
	} else if strings.HasPrefix(content, "```") {
		content = strings.TrimPrefix(content, "```")
		content = strings.TrimSuffix(content, "```")
	}
	content = strings.TrimSpace(content)

	var parsed assessmentCompareLLMResult
	if err := json.Unmarshal([]byte(content), &parsed); err != nil {
		h.logger.Error().Err(err).Str("content", content).Msg("Failed to parse assessment comparison JSON")
		return nil, fmt.Errorf("Failed to parse comparison output")
	}

	fieldSpec := assessmentCompareFieldSpec()
	rows := make([]AssessmentCompareRow, 0, len(fieldSpec))
	for _, f := range fieldSpec {
		grokValue := "N/A"
		deepseekValue := "N/A"
		if parsed.Grok != nil {
			if value := strings.TrimSpace(parsed.Grok[f.Key]); value != "" {
				grokValue = value
			}
		}
		if parsed.Deepseek != nil {
			if value := strings.TrimSpace(parsed.Deepseek[f.Key]); value != "" {
				deepseekValue = value
			}
		}
		rows = append(rows, AssessmentCompareRow{
			Key:      f.Key,
			Label:    f.Label,
			Grok:     grokValue,
			Deepseek: deepseekValue,
		})
	}

	return rows, nil
}

func (h *AssessmentHandler) persistAssessmentDiff(ticker string, rows []AssessmentCompareRow) error {
	payload, err := json.Marshal(rows)
	if err != nil {
		return err
	}

	var existing models.AssessmentDiff
	err = h.db.Where("ticker = ?", ticker).First(&existing).Error
	if err == nil {
		return h.db.Model(&existing).Updates(map[string]interface{}{
			"rows_json":  string(payload),
			"updated_at": time.Now(),
		}).Error
	}
	if err != gorm.ErrRecordNotFound {
		return err
	}

	record := models.AssessmentDiff{
		Ticker:   ticker,
		RowsJSON: string(payload),
	}
	return h.db.Create(&record).Error
}

func (h *AssessmentHandler) regenerateAndPersistAssessmentDiff(ticker string) error {
	ticker = strings.ToUpper(strings.TrimSpace(ticker))
	var records []models.Assessment
	if err := h.db.Where("ticker = ? AND source IN ? AND status = ?", ticker, []string{"grok", "deepseek"}, "completed").Find(&records).Error; err != nil {
		return err
	}

	var grokText string
	var deepseekText string
	for _, record := range records {
		switch strings.ToLower(record.Source) {
		case "grok":
			grokText = record.Assessment
		case "deepseek":
			deepseekText = record.Assessment
		}
	}

	if strings.TrimSpace(grokText) == "" || strings.TrimSpace(deepseekText) == "" {
		return nil
	}

	rows, err := h.extractAssessmentCompareRows(ticker, grokText, deepseekText)
	if err != nil {
		return err
	}
	return h.persistAssessmentDiff(ticker, rows)
}

// cleanupOldAssessments removes assessments beyond the most recent 20
func (h *AssessmentHandler) cleanupOldAssessments() {
	// Count total assessments
	var count int64
	if err := h.db.Model(&models.Assessment{}).Count(&count).Error; err != nil {
		h.logger.Error().Err(err).Msg("Failed to count assessments")
		return
	}

	// If we have more than 20, delete the oldest ones
	if count > 20 {
		// Get IDs of assessments to delete (keep the most recent 20)
		var idsToDelete []uint
		if err := h.db.Model(&models.Assessment{}).
			Select("id").
			Order("created_at ASC").
			Limit(int(count-20)).
			Pluck("id", &idsToDelete).Error; err != nil {
			h.logger.Error().Err(err).Msg("Failed to get assessment IDs for cleanup")
			return
		}

		// Delete the old assessments
		if len(idsToDelete) > 0 {
			if err := h.db.Where("id IN ?", idsToDelete).Delete(&models.Assessment{}).Error; err != nil {
				h.logger.Error().Err(err).Msg("Failed to delete old assessments")
			} else {
				h.logger.Info().
					Int("deleted", len(idsToDelete)).
					Int64("total_remaining", 20).
					Msg("Cleaned up old assessments")
			}
		}
	}
}
