# TradeBot — Go Backend

Automated crypto trading bot engine for Binance Testnet.

## Stack
- Go 1.25
- Binance Testnet REST + WebSocket
- `golang.org/x/net/websocket` for real-time frontend feed

## Setup

```bash
# 1. Copy env file
cp .env.example .env
# Edit .env with your Binance Testnet credentials

# 2. Run
go run main.go
# Server starts on http://localhost:8080
```

Get free Testnet API keys at: https://testnet.binance.vision

## API Endpoints

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/status` | Bot state, uptime, P&L |
| POST | `/api/bot/start` | Start the bot |
| POST | `/api/bot/stop` | Stop the bot |
| POST | `/api/bot/pause` | Pause the bot |
| POST | `/api/bot/strategy` | Switch strategy `{"strategy":"RSI_MA"}` |
| GET | `/api/balance` | Testnet account balances |
| GET | `/api/trades` | Trade history |
| GET | `/api/pnl` | P&L summary |
| POST | `/api/backtest` | Run backtest |
| GET | `/api/settings` | Get current config |
| POST | `/api/settings` | Update config |
| GET | `/api/connectivity` | Test Binance connection |
| WS | `/ws` | Real-time event stream |

## WebSocket Events

Events broadcast to all connected clients:

| Event | Payload |
|-------|---------|
| `status` | Bot state object |
| `price` | `{ pair, price, candles }` |
| `indicators` | Strategy indicator values |
| `trade` | New open trade |
| `trade_closed` | Closed trade with P&L |
| `position` | Live position + unrealised P&L |
| `pnl` | Running P&L summary |
| `brain_log` | `{ time, message, type }` |
| `backtest_progress` | `{ percent, message }` |
| `backtest_result` | Full backtest result |

## Strategies

| Name | Logic |
|------|-------|
| `RSI_MA` | RSI < 30 + short MA crosses above long MA → BUY |
| `BOLLINGER` | Price touches lower band → BUY, upper band → SELL |
| `EMA` | Fast EMA crosses above slow EMA → BUY |

## Risk Management

- **Stop Loss**: exit if price drops X% below entry
- **Take Profit**: exit if price rises X% above entry  
- **Max Daily Loss**: bot auto-pauses if daily drawdown exceeds threshold
