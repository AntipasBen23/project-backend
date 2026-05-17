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

type openAIRequest struct {
	Model     string          `json:"model"`
	MaxTokens int             `json:"max_tokens"`
	Messages  []openAIMessage `json:"messages"`
}

type openAIMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type openAIResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

var openAIClient = &http.Client{Timeout: 30 * time.Second}

func callOpenAI(prompt string) (string, error) {
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		return "", fmt.Errorf("OPENAI_API_KEY not configured")
	}

	reqBody := openAIRequest{
		Model:     "gpt-4o-mini",
		MaxTokens: 350,
		Messages: []openAIMessage{
			{
				Role:    "system",
				Content: "You are a professional cryptocurrency trading analyst. Provide concise, insightful technical analysis. Be direct, use specific price levels, and keep your response to 3-4 sentences.",
			},
			{Role: "user", Content: prompt},
		},
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return "", err
	}

	req, err := http.NewRequest("POST", "https://api.openai.com/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := openAIClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	var cr openAIResponse
	if err := json.Unmarshal(respBody, &cr); err != nil {
		return "", err
	}
	if cr.Error != nil {
		return "", fmt.Errorf("openai error: %s", cr.Error.Message)
	}
	if len(cr.Choices) == 0 {
		return "", fmt.Errorf("empty response from openai")
	}
	return strings.TrimSpace(cr.Choices[0].Message.Content), nil
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

	analysis, err := callOpenAI(prompt)
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
