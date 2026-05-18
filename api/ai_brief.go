package api

import (
	"fmt"
	"net/http"
	"time"
)

func (s *Server) handleAIBrief(w http.ResponseWriter, r *http.Request) {
	if s.engine.LastPrice == 0 {
		s.engine.EnsureLiveContext()
	}

	cfg := s.engine.GetStatus()
	price := s.engine.LastPrice
	indicators := s.engine.Indicators

	symbol := cfg["activePair"]
	strategy := cfg["activeStrategy"]
	rsi := indicators["rsi"]
	shortMA := indicators["shortMA"]
	longMA := indicators["longMA"]

	priceContext := ""
	if price > 0 {
		maAlign := "bullish (MA9 above MA21)"
		if shortMA < longMA {
			maAlign = "bearish (MA9 below MA21)"
		}
		priceContext = fmt.Sprintf(`
Current market data:
- %s at $%.2f
- RSI (14): %.1f
- MA9: $%.2f | MA21: $%.2f — %s
- Active strategy: %s`,
			symbol, price, rsi, shortMA, longMA, maAlign, strategy)
	}

	prompt := fmt.Sprintf(`Generate a concise market brief for a crypto trader starting their session.%s

Write 2-3 professional sentences covering: current market momentum and conditions, key price levels or indicators to watch, and a clear overall trading bias for this session. Direct and professional tone. No bullet points, no headers.`,
		priceContext,
	)

	result, err := callOpenAIMessages([]openAIMessage{
		{Role: "system", Content: "You are a professional cryptocurrency market analyst delivering a concise morning brief. Be direct, specific, and professional."},
		{Role: "user", Content: prompt},
	}, 200)
	if err != nil {
		writeError(w, err.Error(), http.StatusServiceUnavailable)
		return
	}

	writeJSON(w, map[string]interface{}{
		"brief":     result,
		"pair":      symbol,
		"price":     price,
		"timestamp": time.Now(),
	})
}
