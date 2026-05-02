package strategy

import (
	"time"

	"github.com/gopherchan2006/go-algo-trader/indicator"
)

const (
	Symbol    = "BTCUSDT"
	BaseCoin  = "BTC"
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

	QtyDecimals = 6

	MinPrices = 22

	// После закрытия позиции не открывать новую в течение этого времени
	CooldownAfterSell = 60 * time.Second
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
	PrevRSI float64
	BB      indicator.Bands
}

func Compute(prices []float64) Indicators {
	prevRSI := 50.0
	if len(prices) > 1 {
		prevRSI = indicator.RSI(prices[:len(prices)-1], RSIPeriod)
	}
	return Indicators{
		Price:   prices[len(prices)-1],
		EMAFast: indicator.EMA(prices, EMAFastPeriod),
		EMASlow: indicator.EMA(prices, EMASlowPeriod),
		RSI:     indicator.RSI(prices, RSIPeriod),
		PrevRSI: prevRSI,
		BB:      indicator.BollingerBands(prices, BBPeriod, BBMult),
	}
}

// Evaluate возвращает торговый сигнал.
// lastSellTime — время последней продажи (нулевое если ещё не было).
func Evaluate(ind Indicators, entryPrice float64, inPosition bool, lastSellTime time.Time) Signal {
	if inPosition {
		if ind.Price <= entryPrice*(1-StopLossPct) {
			return SignalStopLoss
		}
		if ind.Price >= entryPrice*(1+TakeProfitPct) {
			return SignalTakeProfit
		}
		// Продавать только когда RSI падает (подтверждение разворота вниз)
		if ind.RSI > RSIOverbought && ind.RSI < ind.PrevRSI {
			return SignalSell
		}
		return SignalNone
	}
	// Cooldown: не входить в рынок сразу после продажи
	if !lastSellTime.IsZero() && time.Since(lastSellTime) < CooldownAfterSell {
		return SignalNone
	}
	// Покупать когда RSI в зоне перепроданности (избегаем нулевого значения)
	if ind.RSI < RSIOversold && ind.RSI > 0 {
		return SignalBuy
	}
	return SignalNone
}
