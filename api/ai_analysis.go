package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

type claudeRequest struct {
	Model     string           `json:"model"`
	MaxTokens int              `json:"max_tokens"`
	Messages  []claudeMessage  `json:"messages"`
	System    string           `json:"system"`
}

type claudeMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type claudeResponse struct {
	Content []struct {
		Text string `json:"text"`
	} `json:"content"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

var anthropicClient = &http.Client{Timeout: 30 * time.Second}

func callClaude(prompt string) (string, error) {
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		return "", fmt.Errorf("ANTHROPIC_API_KEY not configured")
	}

	reqBody := claudeRequest{
		Model:     "claude-haiku-4-5-20251001",
		MaxTokens: 350,
		System:    "You are a professional cryptocurrency trading analyst. Provide concise, insightful technical analysis. Be direct, use specific price levels, and keep your response to 3-4 sentences.",
		Messages: []claudeMessage{
			{Role: "user", Content: prompt},
		},
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return "", err
	}

	req, err := http.NewRequest("POST", "https://api.anthropic.com/v1/messages", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("x-api-key", apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")
	req.Header.Set("content-type", "application/json")

	resp, err := anthropicClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	var cr claudeResponse
	if err := json.Unmarshal(respBody, &cr); err != nil {
		return "", err
	}
	if cr.Error != nil {
		return "", fmt.Errorf("claude error: %s", cr.Error.Message)
	}
	if len(cr.Content) == 0 {
		return "", fmt.Errorf("empty response from claude")
	}
	return strings.TrimSpace(cr.Content[0].Text), nil
}

func (s *Server) handleAIAnalysis(w http.ResponseWriter, r *http.Request) {
	cfg := s.engine.GetStatus()
	price := s.engine.LastPrice
	indicators := s.engine.Indicators
	openTrade := s.engine.GetOpenTrade()

	symbol := cfg["activePair"]
	strategy := cfg["activeStrategy"]
	state := cfg["state"]

	rsi := indicators["rsi"]
	shortMA := indicators["shortMA"]
	longMA := indicators["longMA"]
	upper := indicators["upperBand"]
	lower := indicators["lowerBand"]
	mid := indicators["midBand"]
	fastEMA := indicators["fastEMA"]
	slowEMA := indicators["slowEMA"]

	// Build indicator section based on active strategy
	var indicatorLines strings.Builder
	switch strategy {
	case "BOLLINGER":
		indicatorLines.WriteString(fmt.Sprintf("- Bollinger Upper Band: $%.2f\n", upper))
		indicatorLines.WriteString(fmt.Sprintf("- Bollinger Mid Band:   $%.2f\n", mid))
		indicatorLines.WriteString(fmt.Sprintf("- Bollinger Lower Band: $%.2f\n", lower))
	case "EMA":
		indicatorLines.WriteString(fmt.Sprintf("- Fast EMA: $%.2f\n", fastEMA))
		indicatorLines.WriteString(fmt.Sprintf("- Slow EMA: $%.2f\n", slowEMA))
	default:
		maAlign := "MA9 > MA21 (bullish)"
		if shortMA < longMA {
			maAlign = "MA9 < MA21 (bearish)"
		}
		indicatorLines.WriteString(fmt.Sprintf("- RSI (14): %.1f\n", rsi))
		indicatorLines.WriteString(fmt.Sprintf("- MA9: $%.2f\n", shortMA))
		indicatorLines.WriteString(fmt.Sprintf("- MA21: $%.2f\n", longMA))
		indicatorLines.WriteString(fmt.Sprintf("- MA Alignment: %s\n", maAlign))
	}

	// Open position info
	positionInfo := "None"
	if openTrade != nil {
		unrealised := (price - openTrade.EntryPrice) * openTrade.Quantity
		sign := "+"
		if unrealised < 0 {
			sign = ""
		}
		positionInfo = fmt.Sprintf("LONG at $%.2f | Unrealised P&L: %s$%.2f | SL: $%.2f | TP: $%.2f",
			openTrade.EntryPrice, sign, unrealised,
			openTrade.EntryPrice*(1-0.02),
			openTrade.EntryPrice*(1+0.03),
		)
	}

	prompt := fmt.Sprintf(
		`Analyze this live crypto market data and give a professional 3-4 sentence technical analysis:

Symbol: %s
Current Price: $%.2f
Bot Status: %s
Active Strategy: %s

Technical Indicators:
%s
Open Position: %s

Cover: current momentum signal, key price levels to watch, and position/trade outlook.`,
		symbol, price, state, strategy,
		indicatorLines.String(),
		positionInfo,
	)

	analysis, err := callClaude(prompt)
	if err != nil {
		writeError(w, err.Error(), http.StatusServiceUnavailable)
		return
	}

	writeJSON(w, map[string]interface{}{
		"analysis":  analysis,
		"price":     price,
		"symbol":    symbol,
		"strategy":  strategy,
		"timestamp": time.Now(),
	})
}
