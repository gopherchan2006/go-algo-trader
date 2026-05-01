package indicator

func EMA(data []float64, period int) float64 {
	if len(data) < period {
		return 0
	}
	k := 2.0 / float64(period+1)
	ema := 0.0
	for i := 0; i < period; i++ {
		ema += data[i]
	}
	ema /= float64(period)
	for i := period; i < len(data); i++ {
		ema = data[i]*k + ema*(1-k)
	}
	return ema
}
