package strategy

import (
	"time"

	"github.com/gopherchan2006/go-algo-trader/indicator"
)

const (
	Symbol    = "XRPUSDT"
	BaseCoin  = "XRP"
	RiskPct   = 0.25
	FeeRate   = 0.001
	PollEvery = 15 * time.Second
	LogPath   = "trades.log"

	RSIPeriod     = 14
	RSIOversold   = 35.0
	RSIOverbought = 65.0

	BBPeriod = 20
	BBMult   = 2.0

	EMAFastPeriod = 9
	EMASlowPeriod = 21

	StopLossPct   = 0.02
	TakeProfitPct = 0.05

	QtyDecimals = 2

	MinPrices = 22
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

type Indicators struct {
	Price   float64
	EMAFast float64
	EMASlow float64
	RSI     float64
	BB      indicator.Bands
}

func Compute(prices []float64) Indicators {
	return Indicators{
		Price:   prices[len(prices)-1],
		EMAFast: indicator.EMA(prices, EMAFastPeriod),
		EMASlow: indicator.EMA(prices, EMASlowPeriod),
		RSI:     indicator.RSI(prices, RSIPeriod),
		BB:      indicator.BollingerBands(prices, BBPeriod, BBMult),
	}
}

func Evaluate(ind Indicators, entryPrice float64, inPosition bool) Signal {
	if inPosition {
		if ind.Price <= entryPrice*(1-StopLossPct) {
			return SignalStopLoss
		}
		if ind.Price >= entryPrice*(1+TakeProfitPct) {
			return SignalTakeProfit
		}
		if ind.Price >= ind.BB.Upper && ind.RSI > RSIOverbought {
			return SignalSell
		}
		return SignalNone
	}
	if ind.Price <= ind.BB.Lower && ind.RSI < RSIOversold && ind.EMAFast > ind.EMASlow {
		return SignalBuy
	}
	return SignalNone
}
