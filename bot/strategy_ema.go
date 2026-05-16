package bot

import "github.com/AntipasBen23/project-backend/exchange"

type EMAConfig struct {
	FastPeriod int
	SlowPeriod int
}

func DefaultEMAConfig() EMAConfig {
	return EMAConfig{FastPeriod: 9, SlowPeriod: 21}
}

type EMAStrategy struct {
	Config EMAConfig
}

func NewEMAStrategy(cfg EMAConfig) *EMAStrategy {
	return &EMAStrategy{Config: cfg}
}

func (s *EMAStrategy) Name() string { return "EMA" }

func (s *EMAStrategy) Compute(candles []exchange.Candle) (Signal, map[string]float64) {
	if len(candles) < s.Config.SlowPeriod+1 {
		return SignalNone, nil
	}

	closes := make([]float64, len(candles))
	for i, c := range candles {
		closes[i] = c.Close
	}

	fastEMA := computeEMA(closes, s.Config.FastPeriod)
	slowEMA := computeEMA(closes, s.Config.SlowPeriod)

	prevFast := computeEMA(closes[:len(closes)-1], s.Config.FastPeriod)
	prevSlow := computeEMA(closes[:len(closes)-1], s.Config.SlowPeriod)

	indicators := map[string]float64{
		"fastEMA": fastEMA,
		"slowEMA": slowEMA,
	}

	crossedAbove := prevFast <= prevSlow && fastEMA > slowEMA
	crossedBelow := prevFast >= prevSlow && fastEMA < slowEMA

	if crossedAbove {
		return SignalBuy, indicators
	}
	if crossedBelow {
		return SignalSell, indicators
	}
	return SignalNone, indicators
}

func computeEMA(closes []float64, period int) float64 {
	if len(closes) < period {
		return 0
	}
	k := 2.0 / float64(period+1)
	ema := computeSMA(closes[:period], period)
	for _, v := range closes[period:] {
		ema = v*k + ema*(1-k)
	}
	return ema
}
