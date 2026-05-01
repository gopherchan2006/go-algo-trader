package indicator

import "math"

type Bands struct {
	Upper  float64
	Middle float64
	Lower  float64
}

func BollingerBands(prices []float64, period int, mult float64) Bands {
	if len(prices) < period {
		return Bands{}
	}
	window := prices[len(prices)-period:]
	sum := 0.0
	for _, p := range window {
		sum += p
	}
	mean := sum / float64(period)
	variance := 0.0
	for _, p := range window {
		d := p - mean
		variance += d * d
	}
	std := math.Sqrt(variance / float64(period))
	return Bands{
		Upper:  mean + mult*std,
		Middle: mean,
		Lower:  mean - mult*std,
	}
}
