package bot

import (
	"fmt"
	"strconv"
	"time"

	"github.com/AntipasBen23/project-backend/exchange"
)

type Trade struct {
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

type OrderManager struct {
	client    *exchange.Client
	tradeSeq  int
}

func NewOrderManager(client *exchange.Client) *OrderManager {
	return &OrderManager{client: client}
}

func (o *OrderManager) PlaceBuy(pair, strategy string, quantity float64) (*Trade, error) {
	price := 0.0
	var orderErr error

	result, err := o.client.PlaceMarketOrder(pair, "BUY", quantity)
	if err != nil {
		orderErr = err
	} else if result.Price != "" {
		price, _ = strconv.ParseFloat(result.Price, 64)
	}

	// Always fall back to live market price so the trade is always created
	if price == 0 {
		price, err = o.client.GetCurrentPrice(pair)
		if err != nil {
			return nil, fmt.Errorf("buy order failed and cannot get price: %w", err)
		}
	}

	o.tradeSeq++
	trade := &Trade{
		ID:         fmt.Sprintf("T%d", o.tradeSeq),
		Pair:       pair,
		Side:       "BUY",
		Strategy:   strategy,
		EntryPrice: price,
		Quantity:   quantity,
		Status:     "OPEN",
		Timestamp:  time.Now(),
	}
	return trade, orderErr
}

func (o *OrderManager) PlaceSell(trade *Trade, reason string) error {
	result, err := o.client.PlaceMarketOrder(trade.Pair, "SELL", trade.Quantity)
	if err != nil {
		return fmt.Errorf("sell order failed: %w", err)
	}

	exitPrice := 0.0
	if result.Price != "" {
		exitPrice, _ = strconv.ParseFloat(result.Price, 64)
	}
	if exitPrice == 0 {
		exitPrice, err = o.client.GetCurrentPrice(trade.Pair)
		if err != nil {
			return err
		}
	}

	trade.ExitPrice = exitPrice
	trade.PnL = (exitPrice - trade.EntryPrice) * trade.Quantity
	trade.ExitReason = reason
	trade.Status = "CLOSED"
	return nil
}
