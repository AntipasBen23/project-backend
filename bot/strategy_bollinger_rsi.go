package bot

import "github.com/AntipasBen23/project-backend/exchange"

// BollingerRSIStrategy fires a buy only when price is at/below the lower
// Bollinger Band AND RSI confirms oversold — double confirmation reduces
// false signals compared to either indicator alone.
type BollingerRSIConfig struct {
	BBPeriod   int
	BBStdDev   float64
	RSIPeriod  int
	RSIOversold  float64
	RSIOverbought float64
}

func DefaultBollingerRSIConfig() BollingerRSIConfig {
	return BollingerRSIConfig{
		BBPeriod:      20,
		BBStdDev:      2.0,
		RSIPeriod:     14,
		RSIOversold:   35,
		RSIOverbought: 65,
	}
}

type BollingerRSIStrategy struct {
	Config BollingerRSIConfig
}

func NewBollingerRSIStrategy(cfg BollingerRSIConfig) *BollingerRSIStrategy {
	return &BollingerRSIStrategy{Config: cfg}
}

func (s *BollingerRSIStrategy) Name() string { return "BOLLINGER_RSI" }

func (s *BollingerRSIStrategy) Compute(candles []exchange.Candle) (Signal, map[string]float64) {
	minLen := s.Config.BBPeriod + s.Config.RSIPeriod
	if len(candles) < minLen {
		return SignalNone, nil
	}

	closes := make([]float64, len(candles))
	for i, c := range candles {
		closes[i] = c.Close
	}

	upper, mid, lower := computeBollinger(closes, s.Config.BBPeriod, s.Config.BBStdDev)
	rsi := computeRSI(closes, s.Config.RSIPeriod)
	price := closes[len(closes)-1]

	indicators := map[string]float64{
		"rsi":       rsi,
		"upperBand": upper,
		"midBand":   mid,
		"lowerBand": lower,
	}

	// Both conditions must be true — double confirmation
	if price <= lower && rsi < s.Config.RSIOversold {
		return SignalBuy, indicators
	}
	if price >= upper && rsi > s.Config.RSIOverbought {
		return SignalSell, indicators
	}
	return SignalNone, indicators
}
