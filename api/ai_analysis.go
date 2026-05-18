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
	Model          string            `json:"model"`
	MaxTokens      int               `json:"max_tokens"`
	Messages       []openAIMessage   `json:"messages"`
	ResponseFormat map[string]string `json:"response_format,omitempty"`
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

type aiResult struct {
	Analysis       string `json:"analysis"`
	Recommendation string `json:"recommendation"` // "favorable" | "not_favorable"
	Reasoning      string `json:"reasoning"`
}

var openAIClient = &http.Client{Timeout: 30 * time.Second}

const systemPrompt = `You are a professional cryptocurrency trading analyst. Analyze the provided live market data and respond with a JSON object containing exactly these three fields:
- "analysis": a 3-4 sentence professional technical analysis covering current momentum, key price levels to watch, and market context
- "recommendation": either "favorable" or "not_favorable" based on whether conditions support entering a trade right now
- "reasoning": one clear, professional sentence explaining the recommendation

Return only the JSON object, no additional text.`

func callOpenAI(prompt string) (aiResult, error) {
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		return aiResult{}, fmt.Errorf("OPENAI_API_KEY not configured")
	}

	reqBody := openAIRequest{
		Model:          "gpt-4o-mini",
		MaxTokens:      500,
		ResponseFormat: map[string]string{"type": "json_object"},
		Messages: []openAIMessage{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: prompt},
		},
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return aiResult{}, err
	}

	req, err := http.NewRequest("POST", "https://api.openai.com/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return aiResult{}, err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := openAIClient.Do(req)
	if err != nil {
		return aiResult{}, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return aiResult{}, err
	}

	var cr openAIResponse
	if err := json.Unmarshal(respBody, &cr); err != nil {
		return aiResult{}, err
	}
	if cr.Error != nil {
		return aiResult{}, fmt.Errorf("openai error: %s", cr.Error.Message)
	}
	if len(cr.Choices) == 0 {
		return aiResult{}, fmt.Errorf("empty response from openai")
	}

	var result aiResult
	if err := json.Unmarshal([]byte(strings.TrimSpace(cr.Choices[0].Message.Content)), &result); err != nil {
		return aiResult{}, fmt.Errorf("failed to parse AI response: %w", err)
	}
	if result.Recommendation != "favorable" && result.Recommendation != "not_favorable" {
		result.Recommendation = "not_favorable"
	}
	return result, nil
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
		`Analyze this live crypto market data:

Symbol: %s
Current Price: $%.2f
Bot Status: %s
Active Strategy: %s

Technical Indicators:
%s
Open Position: %s

Provide your analysis and a clear recommendation on whether conditions are currently favourable or unfavourable for entering a trade.`,
		symbol, price, state, strategy,
		indicatorLines.String(),
		positionInfo,
	)

	result, err := callOpenAI(prompt)
	if err != nil {
		writeError(w, err.Error(), http.StatusServiceUnavailable)
		return
	}

	writeJSON(w, map[string]interface{}{
		"analysis":       result.Analysis,
		"recommendation": result.Recommendation,
		"reasoning":      result.Reasoning,
		"price":          price,
		"symbol":         symbol,
		"strategy":       strategy,
		"timestamp":      time.Now(),
	})
}
