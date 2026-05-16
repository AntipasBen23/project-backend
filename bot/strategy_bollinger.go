package bot

import (
	"math"

	"github.com/AntipasBen23/project-backend/exchange"
)

type BollingerConfig struct {
	Period     int
	StdDevMult float64
}

func DefaultBollingerConfig() BollingerConfig {
	return BollingerConfig{Period: 20, StdDevMult: 1.0}
}

type BollingerStrategy struct {
	Config BollingerConfig
}

func NewBollingerStrategy(cfg BollingerConfig) *BollingerStrategy {
	return &BollingerStrategy{Config: cfg}
}

func (s *BollingerStrategy) Name() string { return "BOLLINGER" }

func (s *BollingerStrategy) Compute(candles []exchange.Candle) (Signal, map[string]float64) {
	if len(candles) < s.Config.Period {
		return SignalNone, nil
	}

	closes := make([]float64, len(candles))
	for i, c := range candles {
		closes[i] = c.Close
	}

	upper, mid, lower := computeBollinger(closes, s.Config.Period, s.Config.StdDevMult)
	price := closes[len(closes)-1]

	indicators := map[string]float64{
		"upperBand": upper,
		"midBand":   mid,
		"lowerBand": lower,
	}

	if price <= lower {
		return SignalBuy, indicators
	}
	if price >= upper {
		return SignalSell, indicators
	}
	return SignalNone, indicators
}

func computeBollinger(closes []float64, period int, mult float64) (upper, mid, lower float64) {
	slice := closes[len(closes)-period:]
	sum := 0.0
	for _, v := range slice {
		sum += v
	}
	mid = sum / float64(period)

	variance := 0.0
	for _, v := range slice {
		d := v - mid
		variance += d * d
	}
	stddev := math.Sqrt(variance / float64(period))

	upper = mid + mult*stddev
	lower = mid - mult*stddev
	return
}
