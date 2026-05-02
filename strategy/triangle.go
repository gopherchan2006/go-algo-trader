package strategy

import (
	"time"

	"github.com/gopherchan2006/go-triangle-detector/pkg/triangle"
)

const (
	Symbol        = "BTCUSDT"
	BaseCoin      = "BTC"
	RiskPct       = 0.25
	FeeRate       = 0.001
	PollEvery     = 5 * time.Minute
	LogPath       = "trades.log"
	QtyDecimals   = 6
	StopLossPct   = 0.05
	TakeProfitPct = 0.08

	KlineInterval     = "5"
	KlineLimit        = 80
	CooldownAfterSell = 10 * time.Minute
)

type Signal int

const (
	SignalNone Signal = iota
	SignalBuy
	SignalSell
	SignalStopLoss
	SignalTakeProfit
)

func (s Signal) String() string {
	switch s {
	case SignalBuy:
		return "BUY"
	case SignalSell:
		return "SELL"
	case SignalStopLoss:
		return "STOP_LOSS"
	case SignalTakeProfit:
		return "TAKE_PROFIT"
	default:
		return "NONE"
	}
}

func Evaluate(
	result triangle.Result,
	price float64,
	entryPrice float64,
	inPosition bool,
	lastSellTime time.Time,
) Signal {
	if inPosition {
		if price <= entryPrice*(1-StopLossPct) {
			return SignalStopLoss
		}
		if price >= entryPrice*(1+TakeProfitPct) {
			return SignalTakeProfit
		}
		return SignalNone
	}
	if !lastSellTime.IsZero() && time.Since(lastSellTime) < CooldownAfterSell {
		return SignalNone
	}
	if result.Found {
		return SignalBuy
	}
	return SignalNone
}
