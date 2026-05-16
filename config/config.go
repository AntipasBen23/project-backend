package config

import (
	"os"
	"sync"
)

type Config struct {
	APIKey    string
	APISecret string

	BaseURL   string
	WsBaseURL string

	TradingPair  string
	Strategy     string
	TradeSize    float64
	StopLoss     float64
	TakeProfit   float64
	MaxDailyLoss float64
}

var (
	instance *Config
	once     sync.Once
)

func Get() *Config {
	once.Do(func() {
		instance = &Config{
			APIKey:       getEnv("BINANCE_API_KEY", ""),
			APISecret:    getEnv("BINANCE_API_SECRET", ""),
			BaseURL:      "https://testnet.binance.vision",
			WsBaseURL:    "wss://testnet.binance.vision",
			TradingPair:  "BTCUSDT",
			Strategy:     "RSI_MA",
			TradeSize:    0.0002,
			StopLoss:     2.0,
			TakeProfit:   3.0,
			MaxDailyLoss: 5.0,
		}
	})
	return instance
}

func Update(key, secret, pair, strategy string, tradeSize, stopLoss, takeProfit, maxDailyLoss float64) {
	cfg := Get()
	if key != "" {
		cfg.APIKey = key
	}
	if secret != "" {
		cfg.APISecret = secret
	}
	if pair != "" {
		cfg.TradingPair = pair
	}
	if strategy != "" {
		cfg.Strategy = strategy
	}
	if tradeSize > 0 {
		cfg.TradeSize = tradeSize
	}
	if stopLoss > 0 {
		cfg.StopLoss = stopLoss
	}
	if takeProfit > 0 {
		cfg.TakeProfit = takeProfit
	}
	if maxDailyLoss > 0 {
		cfg.MaxDailyLoss = maxDailyLoss
	}
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
