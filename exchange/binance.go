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
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var raw [][]interface{}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, err
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

	var info AccountInfo
	if err := json.Unmarshal(body, &info); err != nil {
		return nil, err
	}
	return &info, nil
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

	var result OrderResult
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, err
	}
	return &result, nil
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
