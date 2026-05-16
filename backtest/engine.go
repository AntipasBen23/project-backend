package backtest

import (
	"fmt"
	"math"
	"time"

	"github.com/AntipasBen23/project-backend/bot"
	"github.com/AntipasBen23/project-backend/exchange"
)

type Config struct {
	Symbol         string  `json:"symbol"`
	Interval       string  `json:"interval"`
	Strategy       string  `json:"strategy"`
	StartDate      string  `json:"startDate"`
	EndDate        string  `json:"endDate"`
	InitialCapital float64 `json:"initialCapital"`
	TradeSize      float64 `json:"tradeSize"`
	StopLoss       float64 `json:"stopLoss"`
	TakeProfit     float64 `json:"takeProfit"`
	UseRisk        bool    `json:"useRisk"`
}

type EquityPoint struct {
	Time  time.Time `json:"time"`
	Value float64   `json:"value"`
}

type Result struct {
	TotalReturn  float64       `json:"totalReturn"`
	WinRate      float64       `json:"winRate"`
	TotalTrades  int           `json:"totalTrades"`
	MaxDrawdown  float64       `json:"maxDrawdown"`
	SharpeRatio  float64       `json:"sharpeRatio"`
	EquityCurve  []EquityPoint `json:"equityCurve"`
	Trades       []TradeResult `json:"trades"`
}

type TradeResult struct {
	ID         string    `json:"id"`
	Pair       string    `json:"pair"`
	Side       string    `json:"side"`
	Strategy   string    `json:"strategy"`
	EntryPrice float64   `json:"entryPrice"`
	ExitPrice  float64   `json:"exitPrice"`
	Quantity   float64   `json:"quantity"`
	PnL        float64   `json:"pnl"`
	ExitReason string    `json:"exitReason"`
	Status     string    `json:"status"`
	Timestamp  time.Time `json:"timestamp"`
}

type Engine struct {
	client   *exchange.Client
	Progress func(pct int, msg string)
}

func New(client *exchange.Client) *Engine {
	return &Engine{client: client}
}

func (e *Engine) Run(cfg Config) (*Result, error) {
	if e.Progress != nil {
		e.Progress(5, "Fetching historical candles...")
	}

	start, err := time.Parse("2006-01-02", cfg.StartDate)
	if err != nil {
		return nil, fmt.Errorf("invalid start date: %w", err)
	}
	end, err := time.Parse("2006-01-02", cfg.EndDate)
	if err != nil {
		return nil, fmt.Errorf("invalid end date: %w", err)
	}

	candles, err := e.client.GetHistoricalCandles(
		cfg.Symbol, cfg.Interval,
		start.UnixMilli(), end.UnixMilli(),
	)
	if err != nil || len(candles) == 0 {
		// Fall back to recent candles for demo
		candles, err = e.client.GetCandles(cfg.Symbol, cfg.Interval, 500)
		if err != nil {
			return nil, err
		}
	}

	if e.Progress != nil {
		e.Progress(20, fmt.Sprintf("Loaded %d candles. Running strategy...", len(candles)))
	}

	strategy := buildStrategy(cfg.Strategy)
	capital := cfg.InitialCapital
	equity := []EquityPoint{{Time: time.UnixMilli(candles[0].OpenTime), Value: capital}}

	var trades []TradeResult
	var openTrade *TradeResult
	returns := []float64{}
	peak := capital
	maxDrawdown := 0.0
	winCount := 0

	windowSize := 100
	total := len(candles)

	for i := windowSize; i < total; i++ {
		window := candles[max(0, i-windowSize) : i+1]
		sig, _ := strategy.Compute(window)

		price := candles[i].Close
		ts := time.UnixMilli(candles[i].OpenTime)

		// Check risk on open trade
		if openTrade != nil {
			exitReason := ""
			if cfg.UseRisk && bot.CheckStopLoss(openTrade.EntryPrice, price, cfg.StopLoss) {
				exitReason = "STOP_LOSS"
			} else if cfg.UseRisk && bot.CheckTakeProfit(openTrade.EntryPrice, price, cfg.TakeProfit) {
				exitReason = "TAKE_PROFIT"
			} else if sig == bot.SignalSell {
				exitReason = "SIGNAL"
			}

			if exitReason != "" {
				pnl := (price - openTrade.EntryPrice) * openTrade.Quantity
				capital += pnl
				openTrade.ExitPrice = price
				openTrade.PnL = pnl
				openTrade.ExitReason = exitReason
				openTrade.Status = "CLOSED"
				entryPrice := openTrade.EntryPrice
				qty := openTrade.Quantity
				trades = append(trades, *openTrade)
				openTrade = nil

				if entryPrice*qty != 0 {
					ret := pnl / (entryPrice * qty)
					returns = append(returns, ret)
				}
				if pnl > 0 {
					winCount++
				}
			}
		}

		if openTrade == nil && sig == bot.SignalBuy && capital > 0 {
			qty := cfg.TradeSize
			if qty == 0 {
				qty = capital / price * 0.1
			}
			t := &TradeResult{
				ID:         fmt.Sprintf("BT%d", len(trades)+1),
				Pair:       cfg.Symbol,
				Side:       "BUY",
				Strategy:   cfg.Strategy,
				EntryPrice: price,
				Quantity:   qty,
				Status:     "OPEN",
				Timestamp:  ts,
			}
			openTrade = t
		}

		if capital > peak {
			peak = capital
		}
		dd := (peak - capital) / peak * 100
		if dd > maxDrawdown {
			maxDrawdown = dd
		}

		if i%10 == 0 {
			equity = append(equity, EquityPoint{Time: ts, Value: capital})
		}

		if e.Progress != nil && i%100 == 0 {
			pct := 20 + int(float64(i)/float64(total)*70)
			e.Progress(pct, fmt.Sprintf("Processing candle %d/%d", i, total))
		}
	}

	// Close open trade at end
	if openTrade != nil {
		lastPrice := candles[len(candles)-1].Close
		pnl := (lastPrice - openTrade.EntryPrice) * openTrade.Quantity
		capital += pnl
		openTrade.ExitPrice = lastPrice
		openTrade.PnL = pnl
		openTrade.ExitReason = "SIGNAL"
		openTrade.Status = "CLOSED"
		trades = append(trades, *openTrade)
	}

	equity = append(equity, EquityPoint{Time: time.Now(), Value: capital})

	totalReturn := (capital - cfg.InitialCapital) / cfg.InitialCapital * 100
	winRate := 0.0
	if len(trades) > 0 {
		winRate = float64(winCount) / float64(len(trades)) * 100
	}

	sharpe := computeSharpe(returns)

	if e.Progress != nil {
		e.Progress(100, "Backtest complete!")
	}

	return &Result{
		TotalReturn: totalReturn,
		WinRate:     winRate,
		TotalTrades: len(trades),
		MaxDrawdown: maxDrawdown,
		SharpeRatio: sharpe,
		EquityCurve: equity,
		Trades:      trades,
	}, nil
}

func buildStrategy(name string) bot.Strategy {
	switch name {
	case "BOLLINGER":
		return bot.NewBollingerStrategy(bot.DefaultBollingerConfig())
	case "EMA":
		return bot.NewEMAStrategy(bot.DefaultEMAConfig())
	default:
		return bot.NewRSIMAStrategy(bot.DefaultRSIMAConfig())
	}
}

func computeSharpe(returns []float64) float64 {
	if len(returns) < 2 {
		return 0
	}
	mean := 0.0
	for _, r := range returns {
		mean += r
	}
	mean /= float64(len(returns))

	variance := 0.0
	for _, r := range returns {
		d := r - mean
		variance += d * d
	}
	variance /= float64(len(returns))
	stddev := math.Sqrt(variance)
	if stddev == 0 {
		return 0
	}
	return mean / stddev * math.Sqrt(252)
}

