package report

import (
	"fmt"
	"io"
	"time"
)

type Trade struct {
	Num        int
	EntryTime  time.Time
	ExitTime   time.Time
	EntryPrice float64
	ExitPrice  float64
	Qty        float64
	Spent      float64
	Gross      float64
	Fee        float64
	Net        float64
	ExitReason string
}

func PrintSummary(out io.Writer, startUSDT, endUSDT, totalPnL float64, tradeCount int, history []Trade) {
	sep := "──────────────────────────────────────────────────────────────────────────────────────────────"

	fmt.Fprintf(out, "\n%s\n", sep)
	fmt.Fprintf(out, "  TRADE HISTORY\n")
	fmt.Fprintf(out, "%s\n", sep)
	fmt.Fprintf(out, "  %-3s  %-10s  %-10s  %-9s  %-10s  %-9s  %-12s  %-9s  %-12s  %s\n",
		"#", "Entry", "Exit", "Δ price", "Qty", "Gross", "Fee (USDT%)", "Net", "Reason", "Held")
	fmt.Fprintf(out, "%s\n", sep)

	wins := 0
	totalFee := 0.0
	bestNet := 0.0
	worstNet := 0.0

	for i, t := range history {
		dur := t.ExitTime.Sub(t.EntryTime).Round(time.Second)
		pct := (t.ExitPrice - t.EntryPrice) / t.EntryPrice * 100
		feePct := t.Fee / t.Spent * 100
		totalFee += t.Fee

		if t.Net > 0 {
			wins++
		}
		if i == 0 || t.Net > bestNet {
			bestNet = t.Net
		}
		if i == 0 || t.Net < worstNet {
			worstNet = t.Net
		}

		fmt.Fprintf(out, "  %-3d  %-10.4f  %-10.4f  %+8.2f%%  %-10.2f  %+8.2f  %6.2f(%.1f%%)  %+8.2f  %-12s  %s\n",
			t.Num,
			t.EntryPrice, t.ExitPrice,
			pct,
			t.Qty,
			t.Gross,
			t.Fee, feePct,
			t.Net,
			t.ExitReason,
			dur,
		)
	}

	if len(history) == 0 {
		fmt.Fprintf(out, "  (no closed trades)\n")
	}

	winRate := 0.0
	if len(history) > 0 {
		winRate = float64(wins) / float64(len(history)) * 100
	}

	fmt.Fprintf(out, "%s\n", sep)
	fmt.Fprintf(out, "  RESULTS\n")
	fmt.Fprintf(out, "%s\n", sep)
	fmt.Fprintf(out, "  Portfolio start : %10.2f USDT\n", startUSDT)
	fmt.Fprintf(out, "  Portfolio end   : %10.2f USDT\n", endUSDT)
	fmt.Fprintf(out, "  Net P&L         : %+10.2f USDT\n", endUSDT-startUSDT)
	fmt.Fprintf(out, "  Realized P&L    : %+10.2f USDT\n", totalPnL)
	fmt.Fprintf(out, "  Total fees paid : %10.2f USDT\n", totalFee)
	fmt.Fprintf(out, "  Trades opened   : %d\n", tradeCount)
	fmt.Fprintf(out, "  Trades closed   : %d  wins=%d  win rate=%.0f%%\n", len(history), wins, winRate)
	if len(history) > 0 {
		fmt.Fprintf(out, "  Best trade      : %+10.2f USDT\n", bestNet)
		fmt.Fprintf(out, "  Worst trade     : %+10.2f USDT\n", worstNet)
	}
	fmt.Fprintf(out, "%s\n\n", sep)
}
