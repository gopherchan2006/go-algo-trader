package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gopherchan2006/go-algo-trader/bybit"
)

const (
	symbol    = "BTCUSDT"
	riskPct   = 0.20
	pollEvery = 15 * time.Second
	emaFast   = 3
	emaSlow   = 7
	feeRate   = 0.001
)

type Trade struct {
	num        int
	entryTime  time.Time
	exitTime   time.Time
	entryPrice float64
	exitPrice  float64
	qty        float64
	spent      float64
	gross      float64
	fee        float64
	net        float64
}

type position struct {
	active     bool
	btcHeld    float64
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

	client := bybit.New(apiKey, secretKey)

	startUSDT, err := client.GetCoinBalance("USDT")
	if err != nil {
		log.Fatalf("get balance: %v", err)
	}
	if startUSDT < 10 {
		log.Fatalf("USDT balance too low: %.2f", startUSDT)
	}

	fmt.Printf("\n╔══════════════════════════════════════╗\n")
	fmt.Printf("║  go-algo-trader  |  EMA %d/%d momentum  ║\n", emaFast, emaSlow)
	fmt.Printf("║  symbol=%-8s  risk=%.0f%%  run=24/7  ║\n", symbol, riskPct*100)
	fmt.Printf("╚══════════════════════════════════════╝\n")
	fmt.Printf("  Starting balance: %.2f USDT\n\n", startUSDT)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-quit
		fmt.Println("\n[!] Interrupted — closing position if open...")
		cancel()
	}()

	pos, tradeCount, totalPnL, history := runLoop(ctx, client)

	if pos.active && pos.btcHeld > 0 {
		orderID, err := client.MarketSell(symbol, pos.btcHeld)
		if err != nil {
			fmt.Printf("[ERR] final sell: %v\n", err)
		} else {
			price, _ := client.GetLastPrice(symbol)
			gross := (price - pos.entryPrice) * pos.btcHeld
			fee := pos.spentUSDT*feeRate + price*pos.btcHeld*feeRate
			net := gross - fee
			pricePct := (price - pos.entryPrice) / pos.entryPrice * 100
			dur := time.Since(pos.entryTime).Round(time.Second)
			totalPnL += net
			history = append(history, Trade{
				num:        tradeCount,
				entryTime:  pos.entryTime,
				exitTime:   time.Now(),
				entryPrice: pos.entryPrice,
				exitPrice:  price,
				qty:        pos.btcHeld,
				spent:      pos.spentUSDT,
				gross:      gross,
				fee:        fee,
				net:        net,
			})
			fmt.Printf("✓ Final SELL  Δprice=%+.2f%%  held=%s  net=%+.2f USDT  orderID=%s\n",
				pricePct, dur, net, orderID)
		}
	}

	time.Sleep(2 * time.Second)
	endUSDT, _ := client.GetCoinBalance("USDT")
	printSummary(startUSDT, endUSDT, totalPnL, tradeCount, history)
}

func runLoop(ctx context.Context, client *bybit.Client) (pos position, tradeCount int, totalPnL float64, history []Trade) {
	var prices []float64

	ticker := time.NewTicker(pollEvery)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return

		case <-ticker.C:
			now := time.Now()

			price, err := client.GetLastPrice(symbol)
			if err != nil {
				fmt.Printf("[WARN] price fetch failed: %v\n", err)
				continue
			}
			prices = append(prices, price)

			if len(prices) < emaSlow {
				fmt.Printf("[%s] Warming up (%d/%d samples)  price=%.2f\n",
					now.Format("15:04:05"), len(prices), emaSlow, price)
				continue
			}

			fast := calcEMA(prices, emaFast)
			slow := calcEMA(prices, emaSlow)
			prevFast := calcEMA(prices[:len(prices)-1], emaFast)
			prevSlow := calcEMA(prices[:len(prices)-1], emaSlow)

			crossUp := prevFast <= prevSlow && fast > slow
			crossDown := prevFast >= prevSlow && fast < slow

			unrealized := 0.0
			if pos.active && pos.btcHeld > 0 {
				gross := (price - pos.entryPrice) * pos.btcHeld
				fee := pos.spentUSDT*feeRate + price*pos.btcHeld*feeRate
				unrealized = gross - fee
			}

			fmt.Printf("[%s] price=%.2f  EMA%d=%.2f  EMA%d=%.2f  pos=%v  unreal=%+.2f\n",
				now.Format("15:04:05"), price, emaFast, fast, emaSlow, slow,
				pos.active, unrealized)

			if crossUp && !pos.active {
				usdtBalance, _ := client.GetCoinBalance("USDT")
				spend := usdtBalance * riskPct
				fmt.Printf("  ┌─ BUY SIGNAL ─────────────────────────\n")
				fmt.Printf("  │  balance=%.2f USDT  spending=%.2f USDT\n", usdtBalance, spend)

				btcBefore, _ := client.GetCoinBalance("BTC")
				orderID, err := client.MarketBuy(symbol, spend)
				if err != nil {
					fmt.Printf("  └─ [ERR] buy failed: %v\n", err)
				} else {
					time.Sleep(2 * time.Second)
					btcAfter, _ := client.GetCoinBalance("BTC")
					btcHeld := btcAfter - btcBefore
					if btcHeld <= 0 {
						btcHeld = spend / price
					}
					tradeCount++
					pos = position{
						active:     true,
						btcHeld:    btcHeld,
						entryPrice: price,
						entryTime:  now,
						spentUSDT:  spend,
					}
					fmt.Printf("  └─ ✓ BUY #%d  entry=%.2f  qty=%.5f BTC  cost=%.2f USDT\n",
						tradeCount, pos.entryPrice, pos.btcHeld, spend)
					fmt.Printf("       orderID=%s\n", orderID)
				}

			} else if crossDown && pos.active {
				fmt.Printf("  ┌─ SELL SIGNAL ─────────────────────────\n")
				fmt.Printf("  │  btc=%.5f  entry=%.2f  now=%.2f\n", pos.btcHeld, pos.entryPrice, price)

				orderID, err := client.MarketSell(symbol, pos.btcHeld)
				if err != nil {
					fmt.Printf("  └─ [ERR] sell failed: %v\n", err)
				} else {
					gross := (price - pos.entryPrice) * pos.btcHeld
					fee := pos.spentUSDT*feeRate + price*pos.btcHeld*feeRate
					net := gross - fee
					pricePct := (price - pos.entryPrice) / pos.entryPrice * 100
					feePct := fee / pos.spentUSDT * 100
					dur := now.Sub(pos.entryTime).Round(time.Second)
					totalPnL += net

					history = append(history, Trade{
						num:        tradeCount,
						entryTime:  pos.entryTime,
						exitTime:   now,
						entryPrice: pos.entryPrice,
						exitPrice:  price,
						qty:        pos.btcHeld,
						spent:      pos.spentUSDT,
						gross:      gross,
						fee:        fee,
						net:        net,
					})

					fmt.Printf("  └─ ✓ SELL #%d  exit=%.2f  Δprice=%+.2f%%  held=%s\n",
						tradeCount, price, pricePct, dur)
					fmt.Printf("       gross=%+.2f USDT  fee=%.2f(%.2f%%)  net=%+.2f USDT\n",
						gross, fee, feePct, net)
					fmt.Printf("       cumulative P&L: %+.2f USDT\n", totalPnL)
					fmt.Printf("       orderID=%s\n", orderID)

					pos = position{}
				}
			}

			if len(prices) > 100 {
				prices = prices[len(prices)-100:]
			}
		}
	}
}

func printSummary(startUSDT, endUSDT, totalPnL float64, tradeCount int, history []Trade) {
	sep := "─────────────────────────────────────────────────────────────────────────────"

	fmt.Printf("\n%s\n", sep)
	fmt.Printf("  TRADE HISTORY\n")
	fmt.Printf("%s\n", sep)
	fmt.Printf("  %-3s  %-10s  %-10s  %-9s  %-9s  %-9s  %-12s  %-9s  %s\n",
		"#", "Entry", "Exit", "Δ price", "Qty BTC", "Gross", "Fee (USDT%)", "Net", "Held")
	fmt.Printf("%s\n", sep)

	wins := 0
	totalFee := 0.0
	bestNet := 0.0
	worstNet := 0.0

	for i, t := range history {
		dur := t.exitTime.Sub(t.entryTime).Round(time.Second)
		pct := (t.exitPrice - t.entryPrice) / t.entryPrice * 100
		feePct := t.fee / t.spent * 100
		totalFee += t.fee

		if t.net > 0 {
			wins++
		}
		if i == 0 || t.net > bestNet {
			bestNet = t.net
		}
		if i == 0 || t.net < worstNet {
			worstNet = t.net
		}

		fmt.Printf("  %-3d  %-10.2f  %-10.2f  %+8.2f%%  %-9.5f  %+8.2f  %6.2f(%.2f%%)  %+8.2f  %s\n",
			t.num,
			t.entryPrice, t.exitPrice,
			pct,
			t.qty,
			t.gross,
			t.fee, feePct,
			t.net,
			dur,
		)
	}

	if len(history) == 0 {
		fmt.Printf("  (no closed trades)\n")
	}

	winRate := 0.0
	if len(history) > 0 {
		winRate = float64(wins) / float64(len(history)) * 100
	}

	fmt.Printf("%s\n", sep)
	fmt.Printf("  RESULTS\n")
	fmt.Printf("%s\n", sep)
	fmt.Printf("  Balance start   : %10.2f USDT\n", startUSDT)
	fmt.Printf("  Balance end     : %10.2f USDT\n", endUSDT)
	fmt.Printf("  Net P&L         : %+10.2f USDT\n", endUSDT-startUSDT)
	fmt.Printf("  Realized P&L    : %+10.2f USDT\n", totalPnL)
	fmt.Printf("  Total fees paid : %10.2f USDT\n", totalFee)
	fmt.Printf("  Trades opened   : %d\n", tradeCount)
	fmt.Printf("  Trades closed   : %d  wins=%d  win rate=%.0f%%\n", len(history), wins, winRate)
	if len(history) > 0 {
		fmt.Printf("  Best trade      : %+10.2f USDT\n", bestNet)
		fmt.Printf("  Worst trade     : %+10.2f USDT\n", worstNet)
	}
	fmt.Printf("%s\n\n", sep)
}

func calcEMA(data []float64, period int) float64 {
	if len(data) < period {
		return 0
	}
	k := 2.0 / float64(period+1)
	ema := 0.0
	for _, v := range data[:period] {
		ema += v
	}
	ema /= float64(period)
	for _, v := range data[period:] {
		ema = v*k + ema*(1-k)
	}
	return ema
}
