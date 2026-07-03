package analysis

import (
	"math"
	"time"

	"telegram-bist-bot/internal/market"
)

func smaLast(values []float64, period int) float64 {
	if period <= 0 || len(values) < period {
		return math.NaN()
	}
	sum := 0.0
	for _, value := range values[len(values)-period:] {
		sum += value
	}
	return sum / float64(period)
}

func rsiLast(closes []float64, period int) float64 {
	if period <= 0 || len(closes) < period+1 {
		return math.NaN()
	}
	alpha := 1.0 / float64(period)
	avgGain := 0.0
	avgLoss := 0.0
	for i := 1; i < len(closes); i++ {
		delta := closes[i] - closes[i-1]
		gain := 0.0
		loss := 0.0
		if delta > 0 {
			gain = delta
		} else if delta < 0 {
			loss = -delta
		}
		avgGain = alpha*gain + (1-alpha)*avgGain
		avgLoss = alpha*loss + (1-alpha)*avgLoss
	}
	if avgLoss == 0 {
		return 100
	}
	rs := avgGain / avgLoss
	return 100 - (100 / (1 + rs))
}

func atrLast(candles []market.Candle, period int) float64 {
	if period <= 0 || len(candles) < period {
		return math.NaN()
	}
	trs := make([]float64, len(candles))
	for i, candle := range candles {
		tr := candle.High - candle.Low
		if i > 0 {
			prevClose := candles[i-1].Close
			tr = math.Max(tr, math.Abs(candle.High-prevClose))
			tr = math.Max(tr, math.Abs(candle.Low-prevClose))
		}
		trs[i] = tr
	}
	return smaLast(trs, period)
}

func rollingVWAPLast(candles []market.Candle, period int) float64 {
	if period <= 0 || len(candles) < period {
		return math.NaN()
	}
	start := len(candles) - period
	tpvSum := 0.0
	volumeSum := 0.0
	for _, candle := range candles[start:] {
		tp := (candle.High + candle.Low + candle.Close) / 3
		tpvSum += tp * candle.Volume
		volumeSum += candle.Volume
	}
	if volumeSum == 0 {
		return math.NaN()
	}
	return tpvSum / volumeSum
}

func intradayVWAPLast(candles []market.Candle, loc *time.Location) float64 {
	if len(candles) == 0 {
		return math.NaN()
	}
	lastDay := candles[len(candles)-1].Time.In(loc).Format("2006-01-02")
	tpvSum := 0.0
	volumeSum := 0.0
	for _, candle := range candles {
		if candle.Time.In(loc).Format("2006-01-02") != lastDay {
			continue
		}
		tp := (candle.High + candle.Low + candle.Close) / 3
		tpvSum += tp * candle.Volume
		volumeSum += candle.Volume
	}
	if volumeSum == 0 {
		return candles[len(candles)-1].Close
	}
	return tpvSum / volumeSum
}

func pocLast(candles []market.Candle, lookback int, bins int) float64 {
	poc, _ := volumeProfile(candles, lookback, bins, 0.70)
	return poc
}

func volumeProfile(candles []market.Candle, lookback int, bins int, valueAreaPct float64) (float64, float64) {
	if lookback <= 0 || bins <= 0 || len(candles) == 0 {
		return math.NaN(), math.NaN()
	}
	start := len(candles) - lookback
	if start < 0 {
		start = 0
	}
	window := candles[start:]
	minLow := window[0].Low
	maxHigh := window[0].High
	for _, candle := range window[1:] {
		if candle.Low < minLow {
			minLow = candle.Low
		}
		if candle.High > maxHigh {
			maxHigh = candle.High
		}
	}
	if maxHigh <= minLow {
		last := window[len(window)-1].Close
		return last, last
	}

	profile := make([]float64, bins)
	width := (maxHigh - minLow) / float64(bins)
	for _, candle := range window {
		idx := int(math.Floor((candle.Close - minLow) / width))
		if idx < 0 {
			idx = 0
		}
		if idx >= bins {
			idx = bins - 1
		}
		profile[idx] += candle.Volume
	}

	pocIdx := 0
	totalVolume := 0.0
	for i, volume := range profile {
		totalVolume += volume
		if volume > profile[pocIdx] {
			pocIdx = i
		}
	}
	poc := minLow + (float64(pocIdx)+0.5)*width
	target := totalVolume * valueAreaPct
	volumeSum := profile[pocIdx]
	lowIdx := pocIdx
	highIdx := pocIdx

	for volumeSum < target && (lowIdx > 0 || highIdx < bins-1) {
		lowVolume := 0.0
		highVolume := 0.0
		if lowIdx > 0 {
			lowVolume = profile[lowIdx-1]
		}
		if highIdx < bins-1 {
			highVolume = profile[highIdx+1]
		}
		if lowVolume >= highVolume && lowIdx > 0 {
			lowIdx--
			volumeSum += lowVolume
			continue
		}
		if highIdx < bins-1 {
			highIdx++
			volumeSum += highVolume
			continue
		}
		break
	}

	vah := minLow + float64(highIdx+1)*width
	return poc, vah
}

func trpBuySignal(closes []float64) bool {
	if len(closes) < 14 {
		return false
	}
	start := len(closes) - 9
	for i := start; i < len(closes); i++ {
		if i-4 < 0 || !(closes[i] < closes[i-4]) {
			return false
		}
	}
	return true
}

func obvBreakout(candles []market.Candle) bool {
	if len(candles) < 22 {
		return false
	}
	obv := make([]float64, len(candles))
	for i := 1; i < len(candles); i++ {
		switch {
		case candles[i].Close > candles[i-1].Close:
			obv[i] = obv[i-1] + candles[i].Volume
		case candles[i].Close < candles[i-1].Close:
			obv[i] = obv[i-1] - candles[i].Volume
		default:
			obv[i] = obv[i-1]
		}
	}
	lastSMA := smaLast(obv, 21)
	prevSMA := mean(obv[len(obv)-22 : len(obv)-1])
	last := obv[len(obv)-1]
	prev := obv[len(obv)-2]
	return last > lastSMA && prev <= prevSMA
}

func pivotS3Status(candles []market.Candle) (float64, bool, bool) {
	if len(candles) < 2 {
		return math.NaN(), false, false
	}
	prev := candles[len(candles)-2]
	pivot := (prev.High + prev.Low + prev.Close) / 3
	s3 := pivot - (2 * (prev.High - prev.Low))
	price := candles[len(candles)-1].Close
	near := price >= s3*0.98 && price <= s3*1.02
	above := price > s3
	return s3, near, above
}

func volumeRatio(candles []market.Candle, projected bool, now time.Time) float64 {
	if len(candles) < 22 {
		return 0
	}
	lastVolume := candles[len(candles)-1].Volume
	if projected && now.Hour() >= 10 && now.Hour() < 18 {
		elapsed := (now.Hour()-10)*60 + now.Minute()
		if elapsed < 1 {
			elapsed = 1
		}
		lastVolume = lastVolume * (480 / float64(elapsed))
	}
	avg := 0.0
	for _, candle := range candles[len(candles)-21 : len(candles)-1] {
		avg += candle.Volume
	}
	avg /= 20
	if avg <= 0 {
		return 0
	}
	return lastVolume / avg
}

func closeSeries(candles []market.Candle) []float64 {
	values := make([]float64, len(candles))
	for i, candle := range candles {
		values[i] = candle.Close
	}
	return values
}

func mean(values []float64) float64 {
	if len(values) == 0 {
		return math.NaN()
	}
	sum := 0.0
	for _, value := range values {
		sum += value
	}
	return sum / float64(len(values))
}

func finite(values ...float64) bool {
	for _, value := range values {
		if math.IsNaN(value) || math.IsInf(value, 0) {
			return false
		}
	}
	return true
}

func round(value float64, digits int) float64 {
	pow := math.Pow10(digits)
	return math.Round(value*pow) / pow
}
