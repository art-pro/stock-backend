package services

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/art-pro/stock-backend/pkg/config"
	"github.com/art-pro/stock-backend/pkg/models"
)

type FairValueSourceEntry struct {
	FairValue float64 `json:"fair_value"`
	Source    string  `json:"source"`
	SourceURL string  `json:"source_url"`
	AsOf      string  `json:"as_of"`
}

type fairValueLLMResponse struct {
	Entries []FairValueSourceEntry `json:"entries"`
}

type NormalizedFairValueEntry struct {
	FairValue  float64
	Source     string
	RecordedAt time.Time
}

type FairValueCollector struct {
	cfg    *config.Config
	client *http.Client
}

func NewFairValueCollector(cfg *config.Config) *FairValueCollector {
	return &FairValueCollector{
		cfg: cfg,
		client: &http.Client{
			Timeout: 40 * time.Second,
		},
	}
}

func (c *FairValueCollector) CollectTrustedFairValues(stock *models.Stock) ([]NormalizedFairValueEntry, error) {
	var all []FairValueSourceEntry
	var errs []string

	if c.cfg.XAIAPIKey != "" {
		entries, err := c.collectFromGrok(stock)
		if err != nil {
			errs = append(errs, fmt.Sprintf("grok: %v", err))
		} else {
			all = append(all, entries...)
		}
	}

	if c.cfg.DeepseekAPIKey != "" {
		entries, err := c.collectFromDeepseek(stock)
		if err != nil {
			errs = append(errs, fmt.Sprintf("deepseek: %v", err))
		} else {
			all = append(all, entries...)
		}
	}

	if len(all) == 0 {
		if len(errs) > 0 {
			return nil, fmt.Errorf(strings.Join(errs, "; "))
		}
		return nil, fmt.Errorf("no LLM provider configured (XAI_API_KEY / DEEPSEEK_API_KEY)")
	}

	valid := make([]NormalizedFairValueEntry, 0, len(all))
	seen := map[string]struct{}{}
	now := time.Now()
	rejected := 0

	for _, entry := range all {
		normalized, ok := validateAndNormalizeEntry(entry, now)
		if !ok {
			rejected++
			continue
		}
		dupeKey := fmt.Sprintf("%.6f|%s|%s", normalized.FairValue, normalized.Source, normalized.RecordedAt.Format("2006-01-02"))
		if _, exists := seen[dupeKey]; exists {
			continue
		}
		seen[dupeKey] = struct{}{}
		valid = append(valid, normalized)
	}

	if len(valid) < 1 {
		errDetail := fmt.Sprintf("no valid trusted/recent entries (received=%d, rejected=%d)", len(all), rejected)
		if len(errs) > 0 {
			errDetail = errDetail + "; provider_errors=" + strings.Join(errs, " | ")
		}
		return nil, fmt.Errorf(errDetail)
	}

	return valid, nil
}

func (c *FairValueCollector) collectFromGrok(stock *models.Stock) ([]FairValueSourceEntry, error) {
	prompt := buildFairValuePrompt(stock)
	reqBody := map[string]interface{}{
		"model": "grok-4-fast-reasoning",
		"messages": []map[string]string{
			{
				"role":    "system",
				"content": "You are a strict financial data assistant. Use only trustworthy and recent sources. Return JSON only.",
			},
			{
				"role":    "user",
				"content": prompt,
			},
		},
		"stream": false,
	}
	return c.callLLM("https://api.x.ai/v1/chat/completions", c.cfg.XAIAPIKey, reqBody)
}

func (c *FairValueCollector) collectFromDeepseek(stock *models.Stock) ([]FairValueSourceEntry, error) {
	prompt := buildFairValuePrompt(stock)
	reqBody := map[string]interface{}{
		"model": "deepseek-reasoner",
		"messages": []map[string]string{
			{
				"role":    "system",
				"content": "You are a strict financial data assistant. Use only trustworthy and recent sources. Return JSON only.",
			},
			{
				"role":    "user",
				"content": prompt,
			},
		},
		"stream": false,
	}
	return c.callLLM("https://api.deepseek.com/v1/chat/completions", c.cfg.DeepseekAPIKey, reqBody)
}

func (c *FairValueCollector) callLLM(endpoint, apiKey string, body map[string]interface{}) ([]FairValueSourceEntry, error) {
	jsonData, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequest("POST", endpoint, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("call provider: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("provider status %d: %s", resp.StatusCode, string(respBody))
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return nil, fmt.Errorf("parse provider response: %w", err)
	}

	choices, ok := parsed["choices"].([]interface{})
	if !ok || len(choices) == 0 {
		return nil, fmt.Errorf("missing choices in response")
	}
	choice, ok := choices[0].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("invalid choice format")
	}
	message, ok := choice["message"].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("invalid message format")
	}
	content, ok := message["content"].(string)
	if !ok {
		return nil, fmt.Errorf("missing content")
	}

	content = extractJSONContent(content)

	var fvResp fairValueLLMResponse
	if err := json.Unmarshal([]byte(content), &fvResp); err != nil {
		return nil, fmt.Errorf("parse fair value JSON: %w", err)
	}

	return fvResp.Entries, nil
}

func extractJSONContent(content string) string {
	trimmed := strings.TrimSpace(content)
	if strings.HasPrefix(trimmed, "```") {
		trimmed = strings.TrimPrefix(trimmed, "```json")
		trimmed = strings.TrimPrefix(trimmed, "```")
		trimmed = strings.TrimSuffix(trimmed, "```")
	}
	return strings.TrimSpace(trimmed)
}

func buildFairValuePrompt(stock *models.Stock) string {
	return fmt.Sprintf(`Find fair value targets for this stock from multiple trustworthy and up-to-date sources.

Stock:
- Ticker: %s
- ISIN: %s
- Company: %s
- Currency: %s

STRICT RULES:
1) Use at least 3 sources.
2) Only use trustworthy sources such as: Reuters, Bloomberg, MarketScreener, Yahoo Finance, Morningstar, WSJ, MarketWatch.
3) Each source must have explicit recency. Reject stale data older than 90 days.
4) Return fair value/target price in stock currency (%s).
5) Do not invent URLs or dates.
6) Always return full absolute URL (include https://).
7) Return JSON only, no extra text.

JSON schema:
{
  "entries": [
    {
      "fair_value": 123.45,
      "source": "MarketScreener consensus target",
      "source_url": "https://...",
      "as_of": "YYYY-MM-DD"
    }
  ]
}`, stock.Ticker, stock.ISIN, stock.CompanyName, stock.Currency, stock.Currency)
}

func validateAndNormalizeEntry(entry FairValueSourceEntry, now time.Time) (NormalizedFairValueEntry, bool) {
	if entry.FairValue <= 0 || entry.FairValue > 10000000 {
		return NormalizedFairValueEntry{}, false
	}
	if strings.TrimSpace(entry.Source) == "" || strings.TrimSpace(entry.SourceURL) == "" || strings.TrimSpace(entry.AsOf) == "" {
		return NormalizedFairValueEntry{}, false
	}
	if !isTrustedSourceURL(entry.SourceURL) {
		return NormalizedFairValueEntry{}, false
	}

	asOf, ok := parseAsOfDate(entry.AsOf)
	if !ok {
		return NormalizedFairValueEntry{}, false
	}
	age := now.Sub(asOf)
	if age < -24*time.Hour || age > 90*24*time.Hour {
		return NormalizedFairValueEntry{}, false
	}

	return NormalizedFairValueEntry{
		FairValue:  entry.FairValue,
		Source:     fmt.Sprintf("%s (%s)", strings.TrimSpace(entry.Source), strings.TrimSpace(entry.SourceURL)),
		RecordedAt: asOf,
	}, true
}

func parseAsOfDate(raw string) (time.Time, bool) {
	layouts := []string{
		"2006-01-02",
		"2006/01/02",
		time.RFC3339,
		"2006-01-02 15:04:05",
		"2006-01-02T15:04:05",
		"Jan 2, 2006",
		"02 Jan 2006",
	}
	for _, layout := range layouts {
		if t, err := time.Parse(layout, strings.TrimSpace(raw)); err == nil {
			return t.UTC(), true
		}
	}
	return time.Time{}, false
}

func isTrustedSourceURL(rawURL string) bool {
	cleaned := strings.TrimSpace(rawURL)
	if cleaned == "" {
		return false
	}
	if !strings.HasPrefix(cleaned, "http://") && !strings.HasPrefix(cleaned, "https://") {
		cleaned = "https://" + cleaned
	}
	parsed, err := url.Parse(cleaned)
	if err != nil || parsed.Host == "" {
		return false
	}
	host := strings.ToLower(parsed.Host)
	host = strings.TrimPrefix(host, "www.")

	trustedDomains := []string{
		"reuters.com",
		"bloomberg.com",
		"marketscreener.com",
		"finance.yahoo.com",
		"morningstar.com",
		"wsj.com",
		"marketwatch.com",
		"tipranks.com",
		"seekingalpha.com",
		"nasdaq.com",
		"ft.com",
	}

	for _, domain := range trustedDomains {
		if host == domain || strings.HasSuffix(host, "."+domain) {
			return true
		}
	}
	return false
}

func Median(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	copied := append([]float64(nil), values...)
	sort.Float64s(copied)
	mid := len(copied) / 2
	if len(copied)%2 == 0 {
		return (copied[mid-1] + copied[mid]) / 2
	}
	return copied[mid]
}
