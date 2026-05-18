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

type chatRequest struct {
	Message string `json:"message"`
	History []struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	} `json:"history"`
}

func (s *Server) handleAIChat(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req chatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Message == "" {
		writeError(w, "message required", http.StatusBadRequest)
		return
	}

	// Build live market context for system prompt
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

	positionInfo := "No open position"
	if openTrade != nil {
		unrealised := (price - openTrade.EntryPrice) * openTrade.Quantity
		sign := "+"
		if unrealised < 0 {
			sign = ""
		}
		positionInfo = fmt.Sprintf("Long at $%.2f | Unrealised P&L: %s$%.4f | SL: $%.2f | TP: $%.2f",
			openTrade.EntryPrice, sign, unrealised,
			openTrade.EntryPrice*0.98, openTrade.EntryPrice*1.03)
	}

	systemMsg := fmt.Sprintf(`You are an expert cryptocurrency trading assistant with real-time access to live market data. You are concise, professional, and direct. Answer in 2-4 sentences unless a detailed explanation is genuinely required. Do not use bullet points for conversational responses.

Live Market Data (as of %s UTC):
- Pair: %s at $%.2f
- Bot: %s | Strategy: %s
- RSI (14): %.1f
- MA9: $%.2f | MA21: $%.2f
- Bollinger Upper: $%.2f | Lower: $%.2f
- Open Position: %s`,
		time.Now().UTC().Format("15:04:05"),
		symbol, price, state, strategy,
		rsi, shortMA, longMA,
		upper, lower,
		positionInfo,
	)

	// Build message history for context
	messages := []openAIMessage{{Role: "system", Content: systemMsg}}
	for _, h := range req.History {
		if h.Role == "user" || h.Role == "assistant" {
			messages = append(messages, openAIMessage{Role: h.Role, Content: h.Content})
		}
	}
	messages = append(messages, openAIMessage{Role: "user", Content: req.Message})

	reply, err := callOpenAIMessages(messages, 300)
	if err != nil {
		writeError(w, err.Error(), http.StatusServiceUnavailable)
		return
	}

	writeJSON(w, map[string]interface{}{
		"reply":     reply,
		"timestamp": time.Now(),
	})
}

func callOpenAIMessages(messages []openAIMessage, maxTokens int) (string, error) {
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		return "", fmt.Errorf("OPENAI_API_KEY not configured")
	}

	reqBody := openAIRequest{
		Model:     "gpt-4o-mini",
		MaxTokens: maxTokens,
		Messages:  messages,
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
