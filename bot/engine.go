package bot

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/AntipasBen23/project-backend/config"
	"github.com/AntipasBen23/project-backend/exchange"
)

type Signal int

const (
	SignalNone Signal = iota
	SignalBuy
	SignalSell
)

type State string

const (
	StateRunning State = "RUNNING"
	StateStopped State = "STOPPED"
	StatePaused  State = "PAUSED"
)

type Strategy interface {
	Name() string
	Compute(candles []exchange.Candle) (Signal, map[string]float64)
}

type BrainLog struct {
	Time    time.Time `json:"time"`
	Message string    `json:"message"`
	Type    string    `json:"type"` // "buy" | "sell" | "info" | "warn"
}

type Engine struct {
	mu              sync.RWMutex
	state           State
	strategy        Strategy
	riskMgr         *RiskManager
	orderMgr        *OrderManager
	client          *exchange.Client
	openTrade       *Trade
	trades          []*Trade
	brainLogs       []BrainLog
	totalPnL        float64
	winCount        int
	lossCount       int
	startTime       time.Time
	cancelFn        context.CancelFunc
	forceBuyOnStart bool
	BroadcastFn     func(event string, data interface{})
	LastCandles     []exchange.Candle
	LastPrice       float64
	Indicators      map[string]float64
}

func NewEngine(client *exchange.Client) *Engine {
	cfg := config.Get()
	return &Engine{
		state:      StateStopped,
		client:     client,
		riskMgr:    NewRiskManager(cfg.MaxDailyLoss),
		orderMgr:   NewOrderManager(client),
		trades:     make([]*Trade, 0),
		brainLogs:  make([]BrainLog, 0),
		Indicators: make(map[string]float64),
	}
}

func (e *Engine) SetStrategy(name string) {
	e.mu.Lock()
	switch name {
	case "RSI_MA":
		e.strategy = NewRSIMAStrategy(DefaultRSIMAConfig())
	case "BOLLINGER":
		e.strategy = NewBollingerStrategy(DefaultBollingerConfig())
	case "EMA":
		e.strategy = NewEMAStrategy(DefaultEMAConfig())
	default:
		e.strategy = NewRSIMAStrategy(DefaultRSIMAConfig())
	}
	config.Get().Strategy = name
	e.mu.Unlock()
	e.log(fmt.Sprintf("Strategy switched to %s", name), "info")
}

func (e *Engine) Start() error {
	e.mu.Lock()
	if e.state == StateRunning {
		e.mu.Unlock()
		return fmt.Errorf("bot already running")
	}
	if e.strategy == nil {
		e.SetStrategyLocked("RSI_MA")
	}
	e.state = StateRunning
	e.startTime = time.Now()
	e.forceBuyOnStart = true
	ctx, cancel := context.WithCancel(context.Background())
	e.cancelFn = cancel
	e.mu.Unlock()

	e.log("Bot started", "info")
	go e.runLoop(ctx)
	go e.tick()
	return nil
}

func (e *Engine) Stop() {
	// Close any open trade at current market price before stopping
	e.mu.RLock()
	hasOpen := e.openTrade != nil
	price := e.LastPrice
	e.mu.RUnlock()

	if hasOpen && price > 0 {
		e.closeTrade("BOT_STOPPED", price)
	}

	e.mu.Lock()
	if e.cancelFn != nil {
		e.cancelFn()
	}
	e.state = StateStopped
	e.openTrade = nil
	e.mu.Unlock()
	e.log("Bot stopped", "warn")
}

func (e *Engine) Pause() {
	e.mu.Lock()
	e.state = StatePaused
	e.mu.Unlock()
	e.log("Bot paused", "warn")
}

func (e *Engine) Resume() {
	e.mu.Lock()
	resumed := e.state == StatePaused
	if resumed {
		e.state = StateRunning
	}
	e.mu.Unlock()
	if resumed {
		e.log("Bot resumed", "info")
	}
}

func (e *Engine) SetStrategyLocked(name string) {
	switch name {
	case "RSI_MA":
		e.strategy = NewRSIMAStrategy(DefaultRSIMAConfig())
	case "BOLLINGER":
		e.strategy = NewBollingerStrategy(DefaultBollingerConfig())
	case "EMA":
		e.strategy = NewEMAStrategy(DefaultEMAConfig())
	default:
		e.strategy = NewRSIMAStrategy(DefaultRSIMAConfig())
	}
}

func (e *Engine) runLoop(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			e.mu.RLock()
			state := e.state
			e.mu.RUnlock()

			if state != StateRunning {
				continue
			}
			e.tick()
		}
	}
}

func (e *Engine) tick() {
	cfg := config.Get()

	candles, err := e.client.GetCandles(cfg.TradingPair, "1m", 100)
	if err != nil {
		log.Printf("failed to get candles: %v", err)
		return
	}

	price, err := e.client.GetCurrentPrice(cfg.TradingPair)
	if err != nil {
		log.Printf("failed to get price: %v", err)
		return
	}

	e.mu.Lock()
	e.LastCandles = candles
	e.LastPrice = price
	e.mu.Unlock()

	if e.BroadcastFn != nil {
		e.BroadcastFn("price", map[string]interface{}{
			"pair":    cfg.TradingPair,
			"price":   price,
			"candles": candles,
		})
		e.BroadcastFn("status", e.GetStatus())
	}

	e.mu.RLock()
	openTrade := e.openTrade
	strategy := e.strategy
	e.mu.RUnlock()

	// Check stop loss / take profit on open trade
	if openTrade != nil {
		if CheckStopLoss(openTrade.EntryPrice, price, cfg.StopLoss) {
			e.log(fmt.Sprintf("Stop loss triggered at $%.2f (entry $%.2f, -%.1f%%)",
				price, openTrade.EntryPrice, cfg.StopLoss), "sell")
			e.closeTrade("STOP_LOSS", price)
			return
		}
		if CheckTakeProfit(openTrade.EntryPrice, price, cfg.TakeProfit) {
			e.log(fmt.Sprintf("Take profit triggered at $%.2f (entry $%.2f, +%.1f%%)",
				price, openTrade.EntryPrice, cfg.TakeProfit), "buy")
			e.closeTrade("TAKE_PROFIT", price)
			return
		}
		// Already in a position, just broadcast unrealised PnL
		unrealisedPnL := (price - openTrade.EntryPrice) * openTrade.Quantity
		if e.BroadcastFn != nil {
			e.BroadcastFn("position", map[string]interface{}{
				"entryPrice":    openTrade.EntryPrice,
				"currentPrice":  price,
				"quantity":      openTrade.Quantity,
				"unrealisedPnL": unrealisedPnL,
				"stopLoss":      openTrade.EntryPrice * (1 - cfg.StopLoss/100),
				"takeProfit":    openTrade.EntryPrice * (1 + cfg.TakeProfit/100),
			})
		}
		return
	}

	if e.riskMgr.IsPaused() {
		return
	}

	signal, indicators := strategy.Compute(candles)
	e.mu.Lock()
	e.Indicators = indicators
	forced := e.forceBuyOnStart
	if forced {
		e.forceBuyOnStart = false
		signal = SignalBuy // always buy on start regardless of RSI
	}
	e.mu.Unlock()

	if e.BroadcastFn != nil {
		e.BroadcastFn("indicators", indicators)
	}

	// Log indicator snapshot every tick so Bot Brain shows the bot is alive
	if rsi, ok := indicators["rsi"]; ok {
		signalLabel := "NONE"
		if signal == SignalBuy {
			if forced {
				signalLabel = "BUY (start)"
			} else {
				signalLabel = "BUY"
			}
		} else if signal == SignalSell {
			signalLabel = "SELL"
		}
		e.log(fmt.Sprintf("RSI %.1f | MA9 $%.2f | MA21 $%.2f → %s",
			rsi, indicators["shortMA"], indicators["longMA"], signalLabel), "info")
	}

	switch signal {
	case SignalBuy:
		e.log(fmt.Sprintf("BUY signal fired — placing order for %.4f %s at $%.2f", cfg.TradeSize, cfg.TradingPair, price), "buy")
		trade, orderErr := e.orderMgr.PlaceBuy(cfg.TradingPair, strategy.Name(), cfg.TradeSize)
		if orderErr != nil {
			e.log(fmt.Sprintf("Binance order error (trade simulated at market): %v", orderErr), "warn")
		}
		if trade == nil {
			return
		}
		e.mu.Lock()
		e.openTrade = trade
		e.mu.Unlock()
		e.log(fmt.Sprintf("Trade opened at $%.2f. SL $%.2f | TP $%.2f",
			trade.EntryPrice, trade.EntryPrice*(1-cfg.StopLoss/100), trade.EntryPrice*(1+cfg.TakeProfit/100)), "buy")
		if e.BroadcastFn != nil {
			e.BroadcastFn("trade", trade)
		}

	case SignalSell:
		e.log("SELL signal fired — no open position to close", "sell")
	}
}

func (e *Engine) closeTrade(reason string, price float64) {
	e.mu.Lock()
	trade := e.openTrade
	e.mu.Unlock()
	if trade == nil {
		return
	}

	cfg := config.Get()
	_ = e.orderMgr.PlaceSell(trade, reason)

	// Override with simulated price if order returns zero
	if trade.ExitPrice == 0 {
		trade.ExitPrice = price
		trade.PnL = (price - trade.EntryPrice) * trade.Quantity
		trade.Status = "CLOSED"
		trade.ExitReason = reason
	}

	e.mu.Lock()
	e.openTrade = nil
	e.totalPnL += trade.PnL
	if trade.PnL > 0 {
		e.winCount++
	} else {
		e.lossCount++
	}
	e.trades = append([]*Trade{trade}, e.trades...)
	if len(e.trades) > 50 {
		e.trades = e.trades[:50]
	}
	e.mu.Unlock()

	e.riskMgr.UpdateDailyLoss(trade.PnL)

	sign := "+"
	if trade.PnL < 0 {
		sign = ""
	}
	e.log(fmt.Sprintf("Trade closed at $%.2f. P&L: %s$%.2f (%s%.2f%%)",
		trade.ExitPrice, sign, trade.PnL, sign, trade.PnL/trade.EntryPrice/trade.Quantity*100), "sell")

	if e.BroadcastFn != nil {
		e.BroadcastFn("trade_closed", trade)
		e.BroadcastFn("pnl", e.getPnLSnapshot())
	}

	if e.riskMgr.IsPaused() {
		e.log(fmt.Sprintf("Daily loss limit reached ($%.2f). Bot auto-paused.", cfg.MaxDailyLoss), "warn")
		e.mu.Lock()
		e.state = StatePaused
		e.mu.Unlock()
		if e.BroadcastFn != nil {
			e.BroadcastFn("status", e.GetStatus())
		}
	}
}

func (e *Engine) log(msg string, logType string) {
	entry := BrainLog{Time: time.Now(), Message: msg, Type: logType}
	e.mu.Lock()
	e.brainLogs = append(e.brainLogs, entry)
	if len(e.brainLogs) > 200 {
		e.brainLogs = e.brainLogs[len(e.brainLogs)-200:]
	}
	e.mu.Unlock()
	log.Printf("[%s] %s", logType, msg)
	if e.BroadcastFn != nil {
		e.BroadcastFn("brain_log", entry)
	}
}

func (e *Engine) GetStatus() map[string]interface{} {
	e.mu.RLock()
	defer e.mu.RUnlock()

	stratName := "RSI_MA"
	if e.strategy != nil {
		stratName = e.strategy.Name()
	}

	uptime := ""
	if !e.startTime.IsZero() {
		uptime = time.Since(e.startTime).Round(time.Second).String()
	}

	total := e.winCount + e.lossCount
	winRate := 0.0
	if total > 0 {
		winRate = float64(e.winCount) / float64(total) * 100
	}

	return map[string]interface{}{
		"state":          string(e.state),
		"activePair":     config.Get().TradingPair,
		"activeStrategy": stratName,
		"uptime":         uptime,
		"totalTrades":    total,
		"totalPnl":       e.totalPnL,
		"winRate":        winRate,
	}
}

func (e *Engine) GetTrades() []*Trade {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.trades
}

func (e *Engine) GetBrainLogs() []BrainLog {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.brainLogs
}

func (e *Engine) GetOpenTrade() *Trade {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.openTrade
}

func (e *Engine) GetLastCandles() []exchange.Candle {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.LastCandles
}

func (e *Engine) getPnLSnapshot() map[string]interface{} {
	total := e.winCount + e.lossCount
	winRate := 0.0
	if total > 0 {
		winRate = float64(e.winCount) / float64(total) * 100
	}
	return map[string]interface{}{
		"totalPnl":    e.totalPnL,
		"winRate":     winRate,
		"totalTrades": total,
	}
}

func (e *Engine) GetPnL() map[string]interface{} {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.getPnLSnapshot()
}
