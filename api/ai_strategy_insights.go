package api

import (
	"fmt"
	"net/http"
	"time"
)

func (s *Server) handleStrategyInsights(w http.ResponseWriter, r *http.Request) {
	trades := s.engine.GetTrades()

	closed := make([]map[string]interface{}, 0)
	for _, t := range trades {
		if t.Status == "CLOSED" {
			closed = append(closed, map[string]interface{}{
				"entry":    t.EntryPrice,
				"exit":     t.ExitPrice,
				"pnl":      t.PnL,
				"reason":   t.ExitReason,
				"strategy": t.Strategy,
				"pair":     t.Pair,
			})
		}
	}

	if len(closed) < 2 {
		writeError(w, "need at least 2 closed trades for analysis", http.StatusBadRequest)
		return
	}

	summary := ""
	wins, losses := 0, 0
	for i, t := range closed {
		outcome := "WIN"
		if t["pnl"].(float64) < 0 {
			outcome = "LOSS"
			losses++
		} else {
			wins++
		}
		pct := 0.0
		entry := t["entry"].(float64)
		exit := t["exit"].(float64)
		if entry > 0 {
			pct = (exit - entry) / entry * 100
		}
		summary += fmt.Sprintf("Trade %d: %s | Entry $%.2f → Exit $%.2f | P&L %+.2f%% | Exit: %s | Strategy: %s\n",
			i+1, outcome, entry, exit, pct, t["reason"], t["strategy"])
	}

	prompt := fmt.Sprintf(`You are analysing the closed trade history of a crypto trading bot to find patterns and suggest strategy improvements.

Trade history (%d trades — %d wins, %d losses):
%s

Write 3-4 sentences identifying specific patterns in what caused wins vs losses, which exit conditions are working, and one concrete suggestion to improve the strategy. Reference actual numbers from the trades. Be direct and professional. No bullet points, no headers.`,
		len(closed), wins, losses, summary)

	result, err := callOpenAIMessages([]openAIMessage{
		{Role: "system", Content: "You are a quantitative trading analyst. Analyse trade outcome data and deliver specific, actionable strategy insights based on the patterns you observe."},
		{Role: "user", Content: prompt},
	}, 350)
	if err != nil {
		writeError(w, err.Error(), http.StatusServiceUnavailable)
		return
	}

	writeJSON(w, map[string]interface{}{
		"insights":   result,
		"tradeCount": len(closed),
		"wins":       wins,
		"losses":     losses,
		"winRate":    float64(wins) / float64(len(closed)) * 100,
		"timestamp":  time.Now(),
	})
}
