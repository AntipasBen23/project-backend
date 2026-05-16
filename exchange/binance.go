package exchange

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/AntipasBen23/project-backend/config"
)

type Candle struct {
	OpenTime  int64   `json:"openTime"`
	Open      float64 `json:"open"`
	High      float64 `json:"high"`
	Low       float64 `json:"low"`
	Close     float64 `json:"close"`
	Volume    float64 `json:"volume"`
	CloseTime int64   `json:"closeTime"`
}

type OrderResult struct {
	Symbol              string `json:"symbol"`
	OrderID             int64  `json:"orderId"`
	Status              string `json:"status"`
	Price               string `json:"price"`
	ExecutedQty         string `json:"executedQty"`
	CummulativeQuoteQty string `json:"cummulativeQuoteQty"`
	Side                string `json:"side"`
	Type                string `json:"type"`
}

type Balance struct {
	Asset  string  `json:"asset"`
	Free   float64 `json:"free"`
	Locked float64 `json:"locked"`
}

type AccountInfo struct {
	Balances []Balance `json:"balances"`
}

type Client struct {
	httpClient *http.Client
}

func NewClient() *Client {
	return &Client{
		httpClient: &http.Client{Timeout: 10 * time.Second},
	}
}

func (c *Client) GetCandles(symbol, interval string, limit int) ([]Candle, error) {
	cfg := config.Get()
	endpoint := fmt.Sprintf("%s/api/v3/klines?symbol=%s&interval=%s&limit=%d",
		cfg.BaseURL, symbol, interval, limit)

	resp, err := c.httpClient.Get(endpoint)
	if err != nil {
		return nil, fmt.Errorf("klines request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("binance klines HTTP %d: %s", resp.StatusCode, string(body))
	}

	var raw [][]interface{}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("klines parse error (body: %.200s): %w", string(body), err)
	}

	candles := make([]Candle, 0, len(raw))
	for _, r := range raw {
		candle := Candle{
			OpenTime:  int64(r[0].(float64)),
			CloseTime: int64(r[6].(float64)),
		}
		candle.Open, _ = strconv.ParseFloat(r[1].(string), 64)
		candle.High, _ = strconv.ParseFloat(r[2].(string), 64)
		candle.Low, _ = strconv.ParseFloat(r[3].(string), 64)
		candle.Close, _ = strconv.ParseFloat(r[4].(string), 64)
		candle.Volume, _ = strconv.ParseFloat(r[5].(string), 64)
		candles = append(candles, candle)
	}
	return candles, nil
}

func (c *Client) GetHistoricalCandles(symbol, interval string, startTime, endTime int64) ([]Candle, error) {
	cfg := config.Get()
	var allCandles []Candle
	current := startTime

	for current < endTime {
		endpoint := fmt.Sprintf("%s/api/v3/klines?symbol=%s&interval=%s&limit=1000&startTime=%d&endTime=%d",
			cfg.BaseURL, symbol, interval, current, endTime)

		resp, err := c.httpClient.Get(endpoint)
		if err != nil {
			return nil, err
		}
		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return nil, err
		}

		var raw [][]interface{}
		if err := json.Unmarshal(body, &raw); err != nil {
			return nil, err
		}
		if len(raw) == 0 {
			break
		}

		for _, r := range raw {
			candle := Candle{
				OpenTime:  int64(r[0].(float64)),
				CloseTime: int64(r[6].(float64)),
			}
			candle.Open, _ = strconv.ParseFloat(r[1].(string), 64)
			candle.High, _ = strconv.ParseFloat(r[2].(string), 64)
			candle.Low, _ = strconv.ParseFloat(r[3].(string), 64)
			candle.Close, _ = strconv.ParseFloat(r[4].(string), 64)
			candle.Volume, _ = strconv.ParseFloat(r[5].(string), 64)
			allCandles = append(allCandles, candle)
		}
		current = int64(raw[len(raw)-1][6].(float64)) + 1
	}
	return allCandles, nil
}

func (c *Client) GetAccountInfo() (*AccountInfo, error) {
	cfg := config.Get()
	if cfg.APIKey == "" {
		return nil, fmt.Errorf("no API key configured")
	}

	params := url.Values{}
	params.Set("timestamp", strconv.FormatInt(time.Now().UnixMilli(), 10))
	params.Set("recvWindow", "5000")

	sig := sign(params.Encode(), cfg.APISecret)
	params.Set("signature", sig)

	req, err := http.NewRequest("GET",
		fmt.Sprintf("%s/api/v3/account?%s", cfg.BaseURL, params.Encode()), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-MBX-APIKEY", cfg.APIKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("binance HTTP %d: %s", resp.StatusCode, string(body))
	}

	// Binance returns free/locked as JSON strings, not numbers — parse with raw struct
	var raw struct {
		Balances []struct {
			Asset  string `json:"asset"`
			Free   string `json:"free"`
			Locked string `json:"locked"`
		} `json:"balances"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, err
	}

	info := &AccountInfo{}
	for _, b := range raw.Balances {
		free, _ := strconv.ParseFloat(b.Free, 64)
		locked, _ := strconv.ParseFloat(b.Locked, 64)
		info.Balances = append(info.Balances, Balance{
			Asset:  b.Asset,
			Free:   free,
			Locked: locked,
		})
	}
	return info, nil
}

func (c *Client) PlaceMarketOrder(symbol, side string, quantity float64) (*OrderResult, error) {
	cfg := config.Get()
	params := url.Values{}
	params.Set("symbol", symbol)
	params.Set("side", side)
	params.Set("type", "MARKET")
	params.Set("quantity", strconv.FormatFloat(quantity, 'f', 6, 64))
	params.Set("timestamp", strconv.FormatInt(time.Now().UnixMilli(), 10))
	params.Set("recvWindow", "5000")

	sig := sign(params.Encode(), cfg.APISecret)
	params.Set("signature", sig)

	req, err := http.NewRequest("POST",
		fmt.Sprintf("%s/api/v3/order", cfg.BaseURL),
		strings.NewReader(params.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-MBX-APIKEY", cfg.APIKey)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("binance order HTTP %d: %s", resp.StatusCode, string(body))
	}

	var result OrderResult
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

type OrderHistoryEntry struct {
	Symbol      string  `json:"symbol"`
	OrderID     int64   `json:"orderId"`
	Side        string  `json:"side"`
	Type        string  `json:"type"`
	Status      string  `json:"status"`
	Price       float64 `json:"price"`
	OrigQty     float64 `json:"origQty"`
	ExecutedQty float64 `json:"executedQty"`
	QuoteQty    float64 `json:"quoteQty"`
	Time        int64   `json:"time"`
}

func (c *Client) GetOrderHistory(symbol string, limit int) ([]OrderHistoryEntry, error) {
	cfg := config.Get()
	if cfg.APIKey == "" {
		return nil, fmt.Errorf("no API key configured")
	}

	params := url.Values{}
	params.Set("symbol", symbol)
	params.Set("limit", strconv.Itoa(limit))
	params.Set("timestamp", strconv.FormatInt(time.Now().UnixMilli(), 10))
	params.Set("recvWindow", "5000")

	sig := sign(params.Encode(), cfg.APISecret)
	params.Set("signature", sig)

	req, err := http.NewRequest("GET",
		fmt.Sprintf("%s/api/v3/allOrders?%s", cfg.BaseURL, params.Encode()), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-MBX-APIKEY", cfg.APIKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("binance allOrders HTTP %d: %s", resp.StatusCode, string(body))
	}

	var raw []struct {
		Symbol      string `json:"symbol"`
		OrderID     int64  `json:"orderId"`
		Side        string `json:"side"`
		Type        string `json:"type"`
		Status      string `json:"status"`
		Price       string `json:"price"`
		OrigQty     string `json:"origQty"`
		ExecutedQty string `json:"executedQty"`
		QuoteQty    string `json:"cummulativeQuoteQty"`
		Time        int64  `json:"time"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, err
	}

	entries := make([]OrderHistoryEntry, 0, len(raw))
	for _, r := range raw {
		price, _ := strconv.ParseFloat(r.Price, 64)
		origQty, _ := strconv.ParseFloat(r.OrigQty, 64)
		execQty, _ := strconv.ParseFloat(r.ExecutedQty, 64)
		quoteQty, _ := strconv.ParseFloat(r.QuoteQty, 64)
		entries = append(entries, OrderHistoryEntry{
			Symbol:      r.Symbol,
			OrderID:     r.OrderID,
			Side:        r.Side,
			Type:        r.Type,
			Status:      r.Status,
			Price:       price,
			OrigQty:     origQty,
			ExecutedQty: execQty,
			QuoteQty:    quoteQty,
			Time:        r.Time,
		})
	}
	// Most recent first
	for i, j := 0, len(entries)-1; i < j; i, j = i+1, j-1 {
		entries[i], entries[j] = entries[j], entries[i]
	}
	return entries, nil
}

func (c *Client) TestConnectivity() error {
	cfg := config.Get()
	resp, err := c.httpClient.Get(fmt.Sprintf("%s/api/v3/ping", cfg.BaseURL))
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}

func (c *Client) GetCurrentPrice(symbol string) (float64, error) {
	cfg := config.Get()
	resp, err := c.httpClient.Get(fmt.Sprintf("%s/api/v3/ticker/price?symbol=%s", cfg.BaseURL, symbol))
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, err
	}

	var result struct {
		Price string `json:"price"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return 0, err
	}

	return strconv.ParseFloat(result.Price, 64)
}

func sign(payload, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(payload))
	return fmt.Sprintf("%x", mac.Sum(nil))
}
