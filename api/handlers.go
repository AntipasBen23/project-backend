package api

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/AntipasBen23/project-backend/backtest"
	"github.com/AntipasBen23/project-backend/bot"
	"github.com/AntipasBen23/project-backend/config"
	"github.com/AntipasBen23/project-backend/exchange"
	"golang.org/x/net/websocket"
)

type Hub struct {
	mu      sync.Mutex
	clients map[*websocket.Conn]bool
}

func newHub() *Hub {
	return &Hub{clients: make(map[*websocket.Conn]bool)}
}

func (h *Hub) add(ws *websocket.Conn) {
	h.mu.Lock()
	h.clients[ws] = true
	h.mu.Unlock()
}

func (h *Hub) remove(ws *websocket.Conn) {
	h.mu.Lock()
	delete(h.clients, ws)
	h.mu.Unlock()
}

func (h *Hub) broadcast(event string, data interface{}) {
	msg, _ := json.Marshal(map[string]interface{}{
		"event": event,
		"data":  data,
	})
	h.mu.Lock()
	defer h.mu.Unlock()
	for ws := range h.clients {
		if err := websocket.Message.Send(ws, string(msg)); err != nil {
			delete(h.clients, ws)
		}
	}
}

func (h *Hub) sendTo(ws *websocket.Conn, event string, data interface{}) {
	msg, _ := json.Marshal(map[string]interface{}{
		"event": event,
		"data":  data,
	})
	websocket.Message.Send(ws, string(msg))
}

type Server struct {
	engine  *bot.Engine
	client  *exchange.Client
	backtester *backtest.Engine
	hub     *Hub
}

func NewServer(engine *bot.Engine, client *exchange.Client) *Server {
	hub := newHub()
	engine.BroadcastFn = hub.broadcast
	backtester := backtest.New(client)
	backtester.Progress = func(pct int, msg string) {
		hub.broadcast("backtest_progress", map[string]interface{}{
			"percent": pct,
			"message": msg,
		})
	}
	return &Server{
		engine:     engine,
		client:     client,
		backtester: backtester,
		hub:        hub,
	}
}

func (s *Server) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/status", cors(s.handleStatus))
	mux.HandleFunc("/api/bot/start", cors(s.handleStart))
	mux.HandleFunc("/api/bot/stop", cors(s.handleStop))
	mux.HandleFunc("/api/bot/pause", cors(s.handlePause))
	mux.HandleFunc("/api/bot/strategy", cors(s.handleStrategy))
	mux.HandleFunc("/api/balance", cors(s.handleBalance))
	mux.HandleFunc("/api/trades", cors(s.handleTrades))
	mux.HandleFunc("/api/pnl", cors(s.handlePnL))
	mux.HandleFunc("/api/backtest", cors(s.handleBacktest))
	mux.HandleFunc("/api/settings", cors(s.handleSettings))
	mux.HandleFunc("/api/connectivity", cors(s.handleConnectivity))
	mux.HandleFunc("/api/candles", cors(s.handleCandles))
	mux.Handle("/ws", websocket.Handler(s.handleWS))
}

func (s *Server) handleWS(ws *websocket.Conn) {
	s.hub.add(ws)
	defer s.hub.remove(ws)

	// Send initial state to this client only
	cfg := config.Get()
	s.hub.sendTo(ws, "status", s.engine.GetStatus())
	s.hub.sendTo(ws, "pnl", s.engine.GetPnL())
	for _, entry := range s.engine.GetBrainLogs() {
		s.hub.sendTo(ws, "brain_log", entry)
	}
	for _, trade := range s.engine.GetTrades() {
		s.hub.sendTo(ws, "trade_closed", trade)
	}

	// Always push current candles so the chart populates immediately
	go func() {
		candles, err := s.client.GetCandles(cfg.TradingPair, "1m", 100)
		if err == nil && len(candles) > 0 {
			price := candles[len(candles)-1].Close
			s.hub.sendTo(ws, "price", map[string]interface{}{
				"pair":    cfg.TradingPair,
				"price":   price,
				"candles": candles,
			})
		}
	}()

	// Keep alive — read until disconnect
	buf := make([]byte, 512)
	for {
		if _, err := ws.Read(buf); err != nil {
			break
		}
	}
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, s.engine.GetStatus())
}

func (s *Server) handleStart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := s.engine.Start(); err != nil {
		writeError(w, err.Error(), http.StatusBadRequest)
		return
	}
	s.hub.broadcast("status", s.engine.GetStatus())
	writeJSON(w, map[string]string{"status": "started"})
}

func (s *Server) handleStop(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	s.engine.Stop()
	s.hub.broadcast("status", s.engine.GetStatus())
	writeJSON(w, map[string]string{"status": "stopped"})
}

func (s *Server) handlePause(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	s.engine.Pause()
	s.hub.broadcast("status", s.engine.GetStatus())
	writeJSON(w, map[string]string{"status": "paused"})
}

func (s *Server) handleStrategy(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		Strategy string `json:"strategy"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, "invalid body", http.StatusBadRequest)
		return
	}
	s.engine.SetStrategy(body.Strategy)
	s.hub.broadcast("status", s.engine.GetStatus())
	writeJSON(w, map[string]string{"strategy": body.Strategy})
}

func (s *Server) handleBalance(w http.ResponseWriter, r *http.Request) {
	info, err := s.client.GetAccountInfo()
	if err != nil {
		// Demo fallback
		writeJSON(w, map[string]interface{}{
			"balances": []map[string]interface{}{
				{"asset": "USDT", "free": 10000.0, "locked": 0.0},
				{"asset": "BTC", "free": 0.001, "locked": 0.0},
				{"asset": "ETH", "free": 0.1, "locked": 0.0},
			},
		})
		return
	}
	filtered := []map[string]interface{}{}
	for _, b := range info.Balances {
		if b.Free > 0 || b.Locked > 0 {
			filtered = append(filtered, map[string]interface{}{
				"asset":  b.Asset,
				"free":   b.Free,
				"locked": b.Locked,
			})
		}
	}
	writeJSON(w, map[string]interface{}{"balances": filtered})
}

func (s *Server) handleTrades(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, s.engine.GetTrades())
}

func (s *Server) handlePnL(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, s.engine.GetPnL())
}

func (s *Server) handleBacktest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var cfg backtest.Config
	if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
		writeError(w, "invalid body", http.StatusBadRequest)
		return
	}
	if cfg.InitialCapital == 0 {
		cfg.InitialCapital = 1000
	}
	if cfg.TradeSize == 0 {
		cfg.TradeSize = 0.001
	}
	if cfg.Interval == "" {
		cfg.Interval = "1h"
	}

	go func() {
		result, err := s.backtester.Run(cfg)
		if err != nil {
			s.hub.broadcast("backtest_error", err.Error())
			log.Printf("backtest error: %v", err)
			return
		}
		s.hub.broadcast("backtest_result", result)
	}()

	writeJSON(w, map[string]string{"status": "running"})
}

func (s *Server) handleSettings(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		cfg := config.Get()
		writeJSON(w, map[string]interface{}{
			"tradingPair":  cfg.TradingPair,
			"strategy":     cfg.Strategy,
			"tradeSize":    cfg.TradeSize,
			"stopLoss":     cfg.StopLoss,
			"takeProfit":   cfg.TakeProfit,
			"maxDailyLoss": cfg.MaxDailyLoss,
		})
		return
	}
	if r.Method == http.MethodPost {
		var body struct {
			APIKey       string  `json:"apiKey"`
			APISecret    string  `json:"apiSecret"`
			TradingPair  string  `json:"tradingPair"`
			Strategy     string  `json:"strategy"`
			TradeSize    float64 `json:"tradeSize"`
			StopLoss     float64 `json:"stopLoss"`
			TakeProfit   float64 `json:"takeProfit"`
			MaxDailyLoss float64 `json:"maxDailyLoss"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeError(w, "invalid body", http.StatusBadRequest)
			return
		}
		config.Update(body.APIKey, body.APISecret, body.TradingPair, body.Strategy,
			body.TradeSize, body.StopLoss, body.TakeProfit, body.MaxDailyLoss)
		writeJSON(w, map[string]string{"status": "ok"})
		return
	}
	http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
}

func (s *Server) handleCandles(w http.ResponseWriter, r *http.Request) {
	symbol := r.URL.Query().Get("symbol")
	if symbol == "" {
		symbol = config.Get().TradingPair
	}
	interval := r.URL.Query().Get("interval")
	if interval == "" {
		interval = "1m"
	}
	limit := 100
	if l, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && l > 0 {
		limit = l
	}
	candles, err := s.client.GetCandles(symbol, interval, limit)
	if err != nil {
		writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, candles)
}

func (s *Server) handleConnectivity(w http.ResponseWriter, r *http.Request) {
	err := s.client.TestConnectivity()
	if err != nil {
		writeJSON(w, map[string]interface{}{
			"connected": false,
			"error":     err.Error(),
			"timestamp": time.Now(),
		})
		return
	}
	writeJSON(w, map[string]interface{}{
		"connected": true,
		"timestamp": time.Now(),
	})
}

func cors(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusOK)
			return
		}
		next(w, r)
	}
}

func writeJSON(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("json encode error: %v", err)
	}
}

func writeError(w http.ResponseWriter, msg string, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	fmt.Fprintf(w, `{"error":%q}`, msg)
}
