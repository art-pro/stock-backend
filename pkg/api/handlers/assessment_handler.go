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

	// Create the comprehensive prompt based on your strategy
	prompt := h.buildAssessmentPrompt(ticker)

	// Build Grok API request
	reqBody := map[string]interface{}{
		"model": "grok-4-fast-reasoning",
		"messages": []map[string]string{
			{
				"role":    "system",
				"content": "You are a financial advisor and investment consultant using a probabilistic strategy. You provide detailed stock analysis following the Kelly Criterion framework. Always provide complete, structured analysis.",
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

	// Create the comprehensive prompt based on your strategy
	prompt := h.buildAssessmentPrompt(ticker)

	// Build Deepseek API request
	reqBody := map[string]interface{}{
		"model": "deepseek-chat",
		"messages": []map[string]string{
			{
				"role":    "system",
				"content": "You are a financial advisor and investment consultant using a probabilistic strategy. You provide detailed stock analysis following the Kelly Criterion framework. Always provide complete, structured analysis.",
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

// buildAssessmentPrompt creates the comprehensive prompt for stock assessment
func (h *AssessmentHandler) buildAssessmentPrompt(ticker string) string {
	return fmt.Sprintf(`You are a financial advisor and investment consultant using a probabilistic strategy. For the stock %s, follow these steps:

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

Use real market data and provide specific numbers for all calculations. Be conservative with probability estimates and avoid hype.`, ticker, ticker)
}