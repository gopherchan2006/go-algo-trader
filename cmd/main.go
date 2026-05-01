package main

import (
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gopherchan2006/go-algo-trader/bybit"
)

const (
	symbol      = "BTCUSDT"
	riskPct     = 0.20
	pollEvery   = 15 * time.Second
	runDuration = 15 * time.Minute
	emaFast     = 3
	emaSlow     = 7
)

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
	fmt.Printf("║  symbol=%-8s  risk=%.0f%%  run=15m   ║\n", symbol, riskPct*100)
	fmt.Printf("╚══════════════════════════════════════╝\n")
	fmt.Printf("  Starting balance: %.2f USDT\n\n", startUSDT)

	var (
		prices     []float64
		inPosition bool
		btcHeld    float64
		entryPrice float64
		trades     int
		totalPnL   float64
	)

	deadline := time.Now().Add(runDuration)
	ticker := time.NewTicker(pollEvery)
	defer ticker.Stop()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	for {
		select {
		case <-quit:
			fmt.Println("\n[!] Interrupted — closing position if open...")
			goto exit

		case <-ticker.C:
			now := time.Now()
			remaining := time.Until(deadline).Round(time.Second)

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
			if inPosition && btcHeld > 0 {
				unrealized = (price - entryPrice) * btcHeld
			}

			fmt.Printf("[%s] price=%.2f  EMA%d=%.2f  EMA%d=%.2f  pos=%v  unreal=%.2f  remain=%s\n",
				now.Format("15:04:05"), price, emaFast, fast, emaSlow, slow,
				inPosition, unrealized, remaining)

			if crossUp && !inPosition {
				usdtBalance, _ := client.GetCoinBalance("USDT")
				spend := usdtBalance * riskPct
				fmt.Printf("  → BUY SIGNAL  spending=%.2f USDT\n", spend)

				orderID, err := client.MarketBuy(symbol, spend)
				if err != nil {
					fmt.Printf("  [ERR] buy failed: %v\n", err)
				} else {
					time.Sleep(2 * time.Second)
					btcHeld, _ = client.GetCoinBalance("BTC")
					entryPrice = price
					inPosition = true
					trades++
					fmt.Printf("  ✓ BUY  orderID=%s  btc=%.5f  entry=%.2f\n", orderID, btcHeld, entryPrice)
				}

			} else if crossDown && inPosition {
				fmt.Printf("  → SELL SIGNAL  btc=%.5f\n", btcHeld)
				sellQty, _ := client.GetCoinBalance("BTC")
				orderID, err := client.MarketSell(symbol, sellQty)
				if err != nil {
					fmt.Printf("  [ERR] sell failed: %v\n", err)
				} else {
					pnl := (price - entryPrice) * btcHeld
					totalPnL += pnl
					inPosition = false
					fmt.Printf("  ✓ SELL  orderID=%s  pnl=%.2f USDT  totalPnL=%.2f\n", orderID, pnl, totalPnL)
					btcHeld = 0
				}
			}

			if now.After(deadline) {
				fmt.Println("\n[!] 15 minutes elapsed — closing position if open...")
				goto exit
			}

			if len(prices) > 100 {
				prices = prices[len(prices)-100:]
			}
		}
	}

exit:
	if inPosition && btcHeld > 0 {
		btcNow, _ := client.GetCoinBalance("BTC")
		if btcNow > 0 {
			orderID, err := client.MarketSell(symbol, btcNow)
			if err != nil {
				fmt.Printf("[ERR] final sell: %v\n", err)
			} else {
				price, _ := client.GetLastPrice(symbol)
				pnl := (price - entryPrice) * btcNow
				totalPnL += pnl
				fmt.Printf("✓ Final SELL  orderID=%s  pnl=%.2f\n", orderID, pnl)
			}
		}
	}

	time.Sleep(2 * time.Second)
	endUSDT, _ := client.GetCoinBalance("USDT")

	fmt.Printf("\n╔══════════════════════════════════════╗\n")
	fmt.Printf("║              RESULTS                 ║\n")
	fmt.Printf("╠══════════════════════════════════════╣\n")
	fmt.Printf("║  Start:   %10.2f USDT             ║\n", startUSDT)
	fmt.Printf("║  End:     %10.2f USDT             ║\n", endUSDT)
	fmt.Printf("║  P&L:     %+10.2f USDT             ║\n", endUSDT-startUSDT)
	fmt.Printf("║  Trades:  %d                          ║\n", trades)
	fmt.Printf("╚══════════════════════════════════════╝\n")
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
