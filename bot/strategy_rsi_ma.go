package bot

import "github.com/AntipasBen23/project-backend/exchange"

type RSIMAConfig struct {
	RSIPeriod  int
	ShortMA    int
	LongMA     int
	RSIOverbought float64
	RSIOversold   float64
}

func DefaultRSIMAConfig() RSIMAConfig {
	return RSIMAConfig{
		RSIPeriod:     14,
		ShortMA:       9,
		LongMA:        21,
		RSIOverbought: 70,
		RSIOversold:   30,
	}
}

type RSIMAStrategy struct {
	Config RSIMAConfig
}

func NewRSIMAStrategy(cfg RSIMAConfig) *RSIMAStrategy {
	return &RSIMAStrategy{Config: cfg}
}

func (s *RSIMAStrategy) Name() string { return "RSI_MA" }

func (s *RSIMAStrategy) Compute(candles []exchange.Candle) (signal Signal, indicators map[string]float64) {
	if len(candles) < s.Config.LongMA+s.Config.RSIPeriod {
		return SignalNone, nil
	}

	closes := make([]float64, len(candles))
	for i, c := range candles {
		closes[i] = c.Close
	}

	rsi := computeRSI(closes, s.Config.RSIPeriod)
	shortMA := computeSMA(closes, s.Config.ShortMA)
	longMA := computeSMA(closes, s.Config.LongMA)

	n := len(closes)
	prevShortMA := computeSMAPrev(closes[:n-1], s.Config.ShortMA)
	prevLongMA := computeSMAPrev(closes[:n-1], s.Config.LongMA)

	indicators = map[string]float64{
		"rsi":       rsi,
		"shortMA":   shortMA,
		"longMA":    longMA,
	}

	crossedAbove := prevShortMA <= prevLongMA && shortMA > longMA
	crossedBelow := prevShortMA >= prevLongMA && shortMA < longMA

	// Fire on MA crossover confirmed by RSI direction (not waiting for extreme levels)
	if crossedAbove && rsi < 55 {
		return SignalBuy, indicators
	}
	if crossedBelow && rsi > 45 {
		return SignalSell, indicators
	}
	return SignalNone, indicators
}

func computeRSI(closes []float64, period int) float64 {
	if len(closes) < period+1 {
		return 50
	}
	gains, losses := 0.0, 0.0
	for i := len(closes) - period; i < len(closes); i++ {
		diff := closes[i] - closes[i-1]
		if diff > 0 {
			gains += diff
		} else {
			losses += -diff
		}
	}
	avgGain := gains / float64(period)
	avgLoss := losses / float64(period)
	if avgLoss == 0 {
		return 100
	}
	rs := avgGain / avgLoss
	return 100 - (100 / (1 + rs))
}

func computeSMA(closes []float64, period int) float64 {
	if len(closes) < period {
		return 0
	}
	sum := 0.0
	for _, v := range closes[len(closes)-period:] {
		sum += v
	}
	return sum / float64(period)
}

func computeSMAPrev(closes []float64, period int) float64 {
	if len(closes) < period {
		return 0
	}
	return computeSMA(closes, period)
}
