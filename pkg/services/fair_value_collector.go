package services

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"sort"
	"strconv"
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
			Timeout: 120 * time.Second,
		},
	}
}

func (c *FairValueCollector) CollectTrustedFairValues(ctx context.Context, stock *models.Stock) ([]NormalizedFairValueEntry, error) {
	var all []FairValueSourceEntry
	var errs []string

	if c.cfg.XAIAPIKey != "" {
		entries, err := c.collectFromGrok(ctx, stock)
		if err != nil {
			errs = append(errs, fmt.Sprintf("grok: %v", err))
		} else {
			for _, e := range entries {
				if strings.TrimSpace(e.Source) == "" {
					e.Source = "Grok"
				} else {
					e.Source = "Grok | " + e.Source
				}
				all = append(all, e)
			}
		}
	}

	if c.cfg.DeepseekAPIKey != "" {
		entries, err := c.collectFromDeepseek(ctx, stock)
		if err != nil {
			errs = append(errs, fmt.Sprintf("deepseek: %v", err))
		} else {
			for _, e := range entries {
				if strings.TrimSpace(e.Source) == "" {
					e.Source = "Deepseek"
				} else {
					e.Source = "Deepseek | " + e.Source
				}
				all = append(all, e)
			}
		}
	}

	if len(all) == 0 {
		if len(errs) > 0 {
			return nil, fmt.Errorf(strings.Join(errs, "; "))
		}
		return nil, fmt.Errorf("no LLM provider configured (XAI_API_KEY / DEEPSEEK_API_KEY)")
	}

	valid := make([]NormalizedFairValueEntry, 0, len(all))
	now := time.Now().UTC()

	for _, entry := range all {
		normalized, ok := normalizeLLMEntry(entry, now)
		if !ok {
			continue
		}
		valid = append(valid, normalized)
	}

	if len(valid) < 1 {
		errDetail := fmt.Sprintf("no usable fair value entries (received=%d)", len(all))
		if len(errs) > 0 {
			errDetail = errDetail + "; provider_errors=" + strings.Join(errs, " | ")
		}
		return nil, fmt.Errorf(errDetail)
	}

	return valid, nil
}

func (c *FairValueCollector) collectFromGrok(ctx context.Context, stock *models.Stock) ([]FairValueSourceEntry, error) {
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
	return c.callLLM(ctx, "https://api.x.ai/v1/chat/completions", c.cfg.XAIAPIKey, reqBody)
}

func (c *FairValueCollector) collectFromDeepseek(ctx context.Context, stock *models.Stock) ([]FairValueSourceEntry, error) {
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
	return c.callLLM(ctx, "https://api.deepseek.com/v1/chat/completions", c.cfg.DeepseekAPIKey, reqBody)
}

func (c *FairValueCollector) callLLM(ctx context.Context, endpoint, apiKey string, body map[string]interface{}) ([]FairValueSourceEntry, error) {
	jsonData, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewBuffer(jsonData))
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

	entries, err := parseFairValueEntries(content)
	if err != nil {
		return nil, fmt.Errorf("parse fair value JSON: %w", err)
	}

	return entries, nil
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

func parseFairValueEntries(content string) ([]FairValueSourceEntry, error) {
	trimmed := extractJSONContent(content)
	candidates := []string{trimmed}

	if start := strings.Index(trimmed, "{"); start >= 0 {
		if end := strings.LastIndex(trimmed, "}"); end > start {
			candidates = append(candidates, trimmed[start:end+1])
		}
	}
	if start := strings.Index(trimmed, "["); start >= 0 {
		if end := strings.LastIndex(trimmed, "]"); end > start {
			candidates = append(candidates, trimmed[start:end+1])
		}
	}

	for _, candidate := range candidates {
		if entries, ok := parseEntriesFromJSONCandidate(candidate); ok && len(entries) > 0 {
			return entries, nil
		}
	}

	if entries := parseEntriesFromPipeText(trimmed); len(entries) > 0 {
		return entries, nil
	}

	return nil, fmt.Errorf("could not parse entries from provider content")
}

func parseEntriesFromJSONCandidate(candidate string) ([]FairValueSourceEntry, bool) {
	var wrapped fairValueLLMResponse
	if err := json.Unmarshal([]byte(candidate), &wrapped); err == nil && len(wrapped.Entries) > 0 {
		return wrapped.Entries, true
	}

	var direct []FairValueSourceEntry
	if err := json.Unmarshal([]byte(candidate), &direct); err == nil && len(direct) > 0 {
		return direct, true
	}

	var obj map[string]interface{}
	if err := json.Unmarshal([]byte(candidate), &obj); err == nil {
		for _, key := range []string{"entries", "data", "results", "sources", "targets"} {
			raw, exists := obj[key]
			if !exists {
				continue
			}
			items, ok := raw.([]interface{})
			if !ok {
				continue
			}
			entries := make([]FairValueSourceEntry, 0, len(items))
			for _, item := range items {
				itemMap, ok := item.(map[string]interface{})
				if !ok {
					continue
				}
				if entry, ok := coerceEntry(itemMap); ok {
					entries = append(entries, entry)
				}
			}
			if len(entries) > 0 {
				return entries, true
			}
		}
	}

	return nil, false
}

func coerceEntry(item map[string]interface{}) (FairValueSourceEntry, bool) {
	readString := func(keys ...string) string {
		for _, key := range keys {
			if value, exists := item[key]; exists {
				if s, ok := value.(string); ok {
					return strings.TrimSpace(s)
				}
			}
		}
		return ""
	}

	readFloat := func(keys ...string) (float64, bool) {
		for _, key := range keys {
			value, exists := item[key]
			if !exists {
				continue
			}
			switch v := value.(type) {
			case float64:
				return v, true
			case int:
				return float64(v), true
			case string:
				cleaned := strings.TrimSpace(strings.ReplaceAll(v, ",", ""))
				cleaned = strings.TrimPrefix(cleaned, "$")
				if f, err := strconv.ParseFloat(cleaned, 64); err == nil {
					return f, true
				}
			}
		}
		return 0, false
	}

	fairValue, ok := readFloat("fair_value", "fairValue", "target_price", "targetPrice", "value", "price")
	if !ok {
		return FairValueSourceEntry{}, false
	}

	entry := FairValueSourceEntry{
		FairValue: fairValue,
		Source:    readString("source", "provider", "name"),
		SourceURL: readString("source_url", "sourceUrl", "url", "link"),
		AsOf:      readString("as_of", "asOf", "date", "updated_at", "published_at"),
	}
	return entry, true
}

func parseEntriesFromPipeText(content string) []FairValueSourceEntry {
	lines := strings.Split(content, "\n")
	datePattern := regexp.MustCompile(`\d{4}-\d{2}-\d{2}`)
	numberPattern := regexp.MustCompile(`[-+]?\d+(?:,\d{3})*(?:\.\d+)?`)

	entries := make([]FairValueSourceEntry, 0)
	for _, rawLine := range lines {
		line := strings.TrimSpace(rawLine)
		if line == "" || !strings.Contains(line, "|") {
			continue
		}
		if strings.Contains(line, "---") {
			continue
		}

		partsRaw := strings.Split(line, "|")
		parts := make([]string, 0, len(partsRaw))
		for _, p := range partsRaw {
			t := strings.TrimSpace(p)
			if t != "" {
				parts = append(parts, t)
			}
		}
		if len(parts) < 3 {
			continue
		}

		source := ""
		sourceURL := ""
		asOf := ""
		fairValue := 0.0
		hasFairValue := false

		for _, part := range parts {
			lower := strings.ToLower(part)
			if strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://") || strings.Contains(lower, ".com/") {
				if sourceURL == "" {
					sourceURL = part
				}
				continue
			}
			if asOf == "" {
				if d := datePattern.FindString(part); d != "" {
					asOf = d
				}
			}
			if !hasFairValue {
				if n := numberPattern.FindString(strings.ReplaceAll(part, "$", "")); n != "" {
					n = strings.ReplaceAll(n, ",", "")
					if f, err := strconv.ParseFloat(n, 64); err == nil && f > 0 {
						fairValue = f
						hasFairValue = true
					}
				}
			}
			if source == "" && !strings.Contains(lower, "fair") && !strings.Contains(lower, "value") && !strings.Contains(lower, "date") {
				source = part
			}
		}

		if hasFairValue {
			entries = append(entries, FairValueSourceEntry{
				FairValue: fairValue,
				Source:    source,
				SourceURL: sourceURL,
				AsOf:      asOf,
			})
		}
	}

	return entries
}

func buildFairValuePrompt(stock *models.Stock) string {
	now := time.Now().UTC()
	currentMonth := now.Format("January")
	currentYear := now.Format("2006")

	return fmt.Sprintf(`Find fair value targets for this stock from multiple trustworthy and up-to-date sources.

Stock:
- Ticker: %s
- ISIN: %s
- Company: %s
- Currency: %s

STRICT RULES:
1) Use between 10 and 15 sources from the web.
2) Only use trustworthy sources such as: Reuters, Bloomberg, MarketScreener, Yahoo Finance, Morningstar, WSJ, MarketWatch.
3) Source date must be from %s %s (current month and year), and each entry must include explicit date.
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
}`, stock.Ticker, stock.ISIN, stock.CompanyName, stock.Currency, currentMonth, currentYear, stock.Currency)
}

func normalizeLLMEntry(entry FairValueSourceEntry, now time.Time) (NormalizedFairValueEntry, bool) {
	if entry.FairValue <= 0 || entry.FairValue > 10000000 {
		return NormalizedFairValueEntry{}, false
	}
	source := strings.TrimSpace(entry.Source)
	if source == "" {
		source = "Unknown source"
	}
	if strings.TrimSpace(entry.SourceURL) != "" {
		source = fmt.Sprintf("%s (%s)", source, strings.TrimSpace(entry.SourceURL))
	}

	recordedAt, ok := parseAsOfDate(entry.AsOf)
	if !ok {
		return NormalizedFairValueEntry{}, false
	}

	// Strict freshness: source data must be from current month and year.
	if recordedAt.Year() != now.Year() || recordedAt.Month() != now.Month() {
		return NormalizedFairValueEntry{}, false
	}

	return NormalizedFairValueEntry{
		FairValue:  entry.FairValue,
		Source:     source,
		RecordedAt: recordedAt,
	}, true
}

func parseAsOfDate(raw string) (time.Time, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, false
	}

	// Extract date part when LLM returns "as of 2026-02-11".
	re := regexp.MustCompile(`\d{4}-\d{2}-\d{2}`)
	if match := re.FindString(raw); match != "" {
		raw = match
	}

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
		if t, err := time.Parse(layout, raw); err == nil {
			return t.UTC(), true
		}
	}
	return time.Time{}, false
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
