package bot

import (
	"sync"
	"time"
)

type RiskManager struct {
	mu           sync.Mutex
	dailyLoss    float64
	dailyLossMax float64
	resetDate    time.Time
	paused       bool
}

func NewRiskManager(maxDailyLoss float64) *RiskManager {
	return &RiskManager{
		dailyLossMax: maxDailyLoss,
		resetDate:    today(),
	}
}

func (r *RiskManager) UpdateDailyLoss(pnl float64) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	if !sameDay(r.resetDate, time.Now()) {
		r.dailyLoss = 0
		r.resetDate = today()
		r.paused = false
	}

	if pnl < 0 {
		r.dailyLoss += -pnl
	}

	if r.dailyLoss >= r.dailyLossMax {
		r.paused = true
		return true
	}
	return false
}

func (r *RiskManager) IsPaused() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.paused
}

func (r *RiskManager) DailyLoss() float64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.dailyLoss
}

func (r *RiskManager) SetMaxDailyLoss(v float64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.dailyLossMax = v
}

func (r *RiskManager) Resume() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.paused = false
}

func CheckStopLoss(entryPrice, currentPrice, stopLossPct float64) bool {
	return currentPrice <= entryPrice*(1-stopLossPct/100)
}

func CheckTakeProfit(entryPrice, currentPrice, takeProfitPct float64) bool {
	return currentPrice >= entryPrice*(1+takeProfitPct/100)
}

func today() time.Time {
	now := time.Now()
	return time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
}

func sameDay(a, b time.Time) bool {
	ay, am, ad := a.Date()
	by, bm, bd := b.Date()
	return ay == by && am == bm && ad == bd
}
