package handlers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/artpro/assessapp/pkg/config"
	"github.com/artpro/assessapp/pkg/models"
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
	Ticker string `json:"ticker" binding:"required"`
	Source string `json:"source" binding:"required,oneof=grok deepseek"`
}

// AssessmentResponse represents the response containing assessment
type AssessmentResponse struct {
	Assessment string `json:"assessment"`
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
	Images []string `json:"images" binding:"required"`
	Source string   `json:"source,omitempty"` // "grok" or "deepseek"
}

// ExtractFromImages extracts stock data from uploaded images
func (h *AssessmentHandler) ExtractFromImages(c *gin.Context) {
	var req ExtractFromImagesRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
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
3. Do not invent or hallucinate tickers or prices.
4. Return strictly raw JSON array, no markdown formatting.`

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
	defer resp.Body.Close()

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
	defer resp.Body.Close()

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
		assessment, err = h.generateGrokAssessment(req.Ticker)
	case "deepseek":
		assessment, err = h.generateDeepseekAssessment(req.Ticker)
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

	// Save assessment to database for history
	assessmentRecord := models.Assessment{
		Ticker:     req.Ticker,
		Source:     req.Source,
		Assessment: assessment,
		Status:     "completed",
		CreatedAt:  time.Now(),
	}

	if err := h.db.Create(&assessmentRecord).Error; err != nil {
		h.logger.Error().Err(err).Msg("Failed to save assessment to database")
		// Continue anyway - don't fail the request if we can't save to DB
	} else {
		// Clean up old assessments to keep only the most recent 20
		h.cleanupOldAssessments()
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

// generateGrokAssessment generates assessment using Grok AI
func (h *AssessmentHandler) generateGrokAssessment(ticker string) (string, error) {
	if h.cfg.XAIAPIKey == "" {
		return "", fmt.Errorf("Grok AI API key not configured")
	}

	// Fetch portfolio data for context
	portfolioData, cashData, err := h.fetchPortfolioContext()
	if err != nil {
		h.logger.Warn().Err(err).Msg("Failed to fetch portfolio context, continuing without it")
	}

	// Create the comprehensive prompt based on your strategy
	prompt := h.buildAssessmentPrompt(ticker, portfolioData, cashData)

	// Build Grok API request
	reqBody := map[string]interface{}{
		"model": "grok-2-1212",
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
	defer resp.Body.Close()

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
func (h *AssessmentHandler) generateDeepseekAssessment(ticker string) (string, error) {
	if h.cfg.DeepseekAPIKey == "" {
		return "", fmt.Errorf("Deepseek AI API key not configured")
	}

	// Fetch portfolio data for context
	portfolioData, cashData, err := h.fetchPortfolioContext()
	if err != nil {
		h.logger.Warn().Err(err).Msg("Failed to fetch portfolio context, continuing without it")
	}

	// Create the comprehensive prompt based on your strategy
	prompt := h.buildAssessmentPrompt(ticker, portfolioData, cashData)

	// Build Deepseek API request
	reqBody := map[string]interface{}{
		"model": "deepseek-chat",
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
	defer resp.Body.Close()

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

// fetchPortfolioContext retrieves current portfolio and cash data for assessment context
func (h *AssessmentHandler) fetchPortfolioContext() ([]models.Stock, []models.CashHolding, error) {
	// Fetch owned stocks (portfolio)
	var portfolioStocks []models.Stock
	if err := h.db.Where("shares_owned > 0").Find(&portfolioStocks).Error; err != nil {
		return nil, nil, fmt.Errorf("failed to fetch portfolio stocks: %w", err)
	}

	// Fetch cash holdings
	var cashHoldings []models.CashHolding
	if err := h.db.Find(&cashHoldings).Error; err != nil {
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
func (h *AssessmentHandler) buildAssessmentPrompt(ticker string, portfolio []models.Stock, cashHoldings []models.CashHolding) string {
	// Build portfolio context string
	portfolioContext := h.buildPortfolioContext(portfolio, cashHoldings)
	// Get current date
	currentDate := time.Now().Format("January 2, 2006")

	return fmt.Sprintf(`CURRENT DATE: %s

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

%s`, currentDate, ticker, ticker, portfolioContext)
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
