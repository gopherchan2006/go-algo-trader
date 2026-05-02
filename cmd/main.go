package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gopherchan2006/go-algo-trader/bybit"
	"github.com/gopherchan2006/go-algo-trader/report"
	"github.com/gopherchan2006/go-algo-trader/strategy"
	"github.com/gopherchan2006/go-triangle-detector/pkg/triangle"
)

var out io.Writer = os.Stdout

type position struct {
	active     bool
	qtyHeld    float64
	entryPrice float64
	entryTime  time.Time
	spentUSDT  float64
}

func main() {
	apiKey := os.Getenv("BYBIT_API_KEY")
	secretKey := os.Getenv("BYBIT_SECRET_KEY")
	if apiKey == "" || secretKey == "" {
		log.Fatal("Set BYBIT_API_KEY and BYBIT_SECRET_KEY")
	}

	logFile, err := os.OpenFile(strategy.LogPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		log.Fatalf("open %s: %v", strategy.LogPath, err)
	}
	defer logFile.Close()

	sessionSep := "════════════════════════════════════════════════════════════════════════════════"
	fmt.Fprintf(logFile, "\n%s\n  SESSION  %s\n%s\n\n",
		sessionSep, time.Now().Format("2006-01-02 15:04:05"), sessionSep)

	out = io.MultiWriter(os.Stdout, logFile)

	client := bybit.New(apiKey, secretKey)

	startUSDT, err := client.GetCoinBalance("USDT")
	if err != nil {
		log.Fatalf("get balance: %v", err)
	}

	startPrice, _ := client.GetLastPrice(strategy.Symbol)
	btcAtStart, _ := client.GetCoinBalance(strategy.BaseCoin)

	if btcAtStart > 0.0001 {
		fmt.Fprintf(out, "[!] Found %.6f %s leftover from previous session — selling...\n", btcAtStart, strategy.BaseCoin)
		_, sellErr := client.MarketSell(strategy.Symbol, btcAtStart, strategy.QtyDecimals)
		if sellErr != nil {
			fmt.Fprintf(out, "[WARN] Could not sell leftover %s: %v\n", strategy.BaseCoin, sellErr)
		} else {
			time.Sleep(2 * time.Second)
			startUSDT, _ = client.GetCoinBalance("USDT")
			btcAtStart = 0
			startPrice, _ = client.GetLastPrice(strategy.Symbol)
			fmt.Fprintf(out, "[!] Sold leftover %s. USDT balance now: %.2f\n", strategy.BaseCoin, startUSDT)
		}
	}

	startTotal := startUSDT + btcAtStart*startPrice
	if startTotal < 10 {
		log.Fatalf("portfolio balance too low: %.2f USDT", startTotal)
	}

	fmt.Fprintf(out, "\n╔════════════════════════════════════════════════╗\n")
	fmt.Fprintf(out, "║  go-algo-trader  |  Ascending Triangle          ║\n")
	fmt.Fprintf(out, "║  symbol=%-10s  risk=%.0f%%  run=24/7        ║\n", strategy.Symbol, strategy.RiskPct*100)
	fmt.Fprintf(out, "║  interval=%s  limit=%d                        ║\n", strategy.KlineInterval, strategy.KlineLimit)
	fmt.Fprintf(out, "║  SL=%.0f%%  TP=%.0f%%                               ║\n",
		strategy.StopLossPct*100, strategy.TakeProfitPct*100)
	fmt.Fprintf(out, "╚════════════════════════════════════════════════╝\n")
	fmt.Fprintf(out, "  Starting portfolio: %.2f USDT\n\n", startTotal)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-quit
		fmt.Fprintln(out, "\n[!] Interrupted — closing position if open...")
		cancel()
	}()

	pos, tradeCount, totalPnL, history := runLoop(ctx, client)

	if pos.active && pos.qtyHeld > 0 {
		orderID, err := client.MarketSell(strategy.Symbol, pos.qtyHeld, strategy.QtyDecimals)
		if err != nil {
			fmt.Fprintf(out, "[ERR] final sell: %v\n", err)
		} else {
			price, _ := client.GetLastPrice(strategy.Symbol)
			gross := (price - pos.entryPrice) * pos.qtyHeld
			fee := pos.spentUSDT*strategy.FeeRate + price*pos.qtyHeld*strategy.FeeRate
			net := gross - fee
			pricePct := (price - pos.entryPrice) / pos.entryPrice * 100
			dur := time.Since(pos.entryTime).Round(time.Second)
			totalPnL += net
			history = append(history, report.Trade{
				Num:        tradeCount,
				EntryTime:  pos.entryTime,
				ExitTime:   time.Now(),
				EntryPrice: pos.entryPrice,
				ExitPrice:  price,
				Qty:        pos.qtyHeld,
				Spent:      pos.spentUSDT,
				Gross:      gross,
				Fee:        fee,
				Net:        net,
				ExitReason: "INTERRUPT",
			})
			fmt.Fprintf(out, "✓ Final SELL  Δprice=%+.2f%%  held=%s  net=%+.2f USDT  orderID=%s\n",
				pricePct, dur, net, orderID)
		}
	}

	time.Sleep(2 * time.Second)
	endUSDT, _ := client.GetCoinBalance("USDT")
	endPrice, _ := client.GetLastPrice(strategy.Symbol)
	btcAtEnd, _ := client.GetCoinBalance(strategy.BaseCoin)
	endTotal := endUSDT + btcAtEnd*endPrice
	report.PrintSummary(out, startTotal, endTotal, totalPnL, tradeCount, history)
}

func runLoop(ctx context.Context, client *bybit.Client) (pos position, tradeCount int, totalPnL float64, history []report.Trade) {
	var lastSellTime time.Time

	ticker := time.NewTicker(strategy.PollEvery)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return

		case <-ticker.C:
			now := time.Now()

			candles, err := client.GetKlines(strategy.Symbol, strategy.KlineInterval, strategy.KlineLimit)
			if err != nil {
				fmt.Fprintf(out, "[WARN] klines fetch failed: %v\n", err)
				continue
			}

			price := candles[len(candles)-1].Close
			result := triangle.Detect(candles)

			unrealized := 0.0
			if pos.active && pos.qtyHeld > 0 {
				gross := (price - pos.entryPrice) * pos.qtyHeld
				fee := pos.spentUSDT*strategy.FeeRate + price*pos.qtyHeld*strategy.FeeRate
				unrealized = gross - fee
			}

			fmt.Fprintf(out, "[%s] price=%.4f  found=%v  resistance=%.4f  touches=%d  reason=%s  pos=%v  unreal=%+.2f\n",
				now.Format("15:04:05"), price,
				result.Found,
				result.ResistanceLevel,
				result.ResistanceTouches,
				result.RejectReason,
				pos.active, unrealized)

			sig := strategy.Evaluate(result, price, pos.entryPrice, pos.active, lastSellTime)

			switch sig {
			case strategy.SignalBuy:
				usdtBalance, _ := client.GetCoinBalance("USDT")
				spend := usdtBalance * strategy.RiskPct
				fmt.Fprintf(out, "  ┌─ BUY SIGNAL ─────────────────────────────────────────────\n")
				fmt.Fprintf(out, "  │  resistance=%.4f  touches=%d  price=%.4f\n",
					result.ResistanceLevel, result.ResistanceTouches, price)
				fmt.Fprintf(out, "  │  balance=%.2f USDT  spending=%.2f USDT\n", usdtBalance, spend)

				coinBefore, _ := client.GetCoinBalance(strategy.BaseCoin)
				orderID, err := client.MarketBuy(strategy.Symbol, spend)
				if err != nil {
					fmt.Fprintf(out, "  └─ [ERR] buy failed: %v\n", err)
				} else {
					time.Sleep(2 * time.Second)
					coinAfter, _ := client.GetCoinBalance(strategy.BaseCoin)
					qtyHeld := coinAfter - coinBefore
					if qtyHeld <= 0 {
						qtyHeld = spend / price
					}
					tradeCount++
					pos = position{
						active:     true,
						qtyHeld:    qtyHeld,
						entryPrice: price,
						entryTime:  now,
						spentUSDT:  spend,
					}
					fmt.Fprintf(out, "  └─ ✓ BUY #%d  entry=%.4f  qty=%.6f %s  cost=%.2f USDT\n",
						tradeCount, pos.entryPrice, pos.qtyHeld, strategy.BaseCoin, spend)
					fmt.Fprintf(out, "       orderID=%s\n", orderID)
				}

			case strategy.SignalSell, strategy.SignalStopLoss, strategy.SignalTakeProfit:
				if !pos.active {
					break
				}
				fmt.Fprintf(out, "  ┌─ %s ────────────────────────────────────────────────────\n", sig)
				fmt.Fprintf(out, "  │  qty=%.6f %s  entry=%.4f  now=%.4f\n",
					pos.qtyHeld, strategy.BaseCoin, pos.entryPrice, price)

				orderID, err := client.MarketSell(strategy.Symbol, pos.qtyHeld, strategy.QtyDecimals)
				if err != nil {
					fmt.Fprintf(out, "  └─ [ERR] sell failed: %v\n", err)
				} else {
					gross := (price - pos.entryPrice) * pos.qtyHeld
					fee := pos.spentUSDT*strategy.FeeRate + price*pos.qtyHeld*strategy.FeeRate
					net := gross - fee
					pricePct := (price - pos.entryPrice) / pos.entryPrice * 100
					feePct := fee / pos.spentUSDT * 100
					dur := now.Sub(pos.entryTime).Round(time.Second)
					totalPnL += net

					history = append(history, report.Trade{
						Num:        tradeCount,
						EntryTime:  pos.entryTime,
						ExitTime:   now,
						EntryPrice: pos.entryPrice,
						ExitPrice:  price,
						Qty:        pos.qtyHeld,
						Spent:      pos.spentUSDT,
						Gross:      gross,
						Fee:        fee,
						Net:        net,
						ExitReason: sig.String(),
					})

					fmt.Fprintf(out, "  └─ ✓ SELL #%d  exit=%.4f  Δprice=%+.2f%%  held=%s\n",
						tradeCount, price, pricePct, dur)
					fmt.Fprintf(out, "       gross=%+.2f USDT  fee=%.2f(%.2f%%)  net=%+.2f USDT\n",
						gross, fee, feePct, net)
					fmt.Fprintf(out, "       cumulative P&L: %+.2f USDT\n", totalPnL)
					fmt.Fprintf(out, "       orderID=%s\n", orderID)
					lastSellTime = now
					pos = position{}
				}
			}
		}
	}
}
