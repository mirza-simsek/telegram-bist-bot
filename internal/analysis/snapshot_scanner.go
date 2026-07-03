package analysis

import (
	"context"
	"fmt"
	"sort"
	"time"

	"telegram-bist-bot/internal/market"
)

var dailySnapshotColumns = []string{
	"name",
	"close",
	"volume",
	"RSI",
	"SMA20",
	"SMA50",
	"SMA200",
	"VWAP",
	"relative_volume_10d_calc",
	"Recommend.All",
}

var intradaySnapshotColumns = []string{
	"name",
	"close",
	"RSI|60",
	"SMA20|60",
	"SMA50|60",
	"SMA200|60",
	"VWAP|60",
	"relative_volume_10d_calc|60",
	"Recommend.All|60",
	"RSI|15",
	"SMA20|15",
	"SMA50|15",
	"SMA200|15",
	"VWAP|15",
	"relative_volume_10d_calc|15",
	"Recommend.All|15",
}

func (s *Scanner) scanDailySnapshot(ctx context.Context, universe Universe, minScore int, previousErrors []string) (*Report, error) {
	started := time.Now().In(s.location)
	report := &Report{
		Mode:            ModeDaily,
		UniverseKey:     universe.Key,
		UniverseName:    universe.Label,
		StartedAt:       started,
		TotalSymbols:    len(universe.Symbols),
		MinScore:        minScore,
		MaxResults:      s.maxResults,
		Source:          "TradingView scanner fallback",
		IntervalSummary: "Daily technical snapshot",
		FilterSummary:   fmt.Sprintf("Skor >= %d, RSI < 80", minScore),
		ErrorSamples:    previousErrors,
	}

	batch := s.snapshotClient.FetchSnapshots(ctx, universe.Symbols, dailySnapshotColumns)
	report.DataSymbols = len(batch.Data)
	report.FailedSymbols = len(batch.Errors)
	report.ErrorSamples = append(report.ErrorSamples, sampleErrors(batch.Errors, 5)...)

	var signals []Signal
	for _, symbol := range universe.Symbols {
		snapshot, ok := batch.Data[symbol]
		if !ok {
			continue
		}
		signal, ok := analyzeDailySnapshot(symbol, snapshot)
		if !ok {
			continue
		}
		report.AnalyzedSymbols++
		if signal.Score >= minScore && signal.RSI < 80 {
			signals = append(signals, signal)
		}
	}

	sort.Slice(signals, func(i, j int) bool {
		if signals[i].Score != signals[j].Score {
			return signals[i].Score > signals[j].Score
		}
		if signals[i].VolumeX != signals[j].VolumeX {
			return signals[i].VolumeX > signals[j].VolumeX
		}
		return signals[i].RSI < signals[j].RSI
	})
	report.Results = limitSignals(signals, s.maxResults)
	report.FinishedAt = time.Now().In(s.location)

	if report.DataSymbols == 0 {
		return report, fmt.Errorf("no daily market data could be downloaded")
	}
	return report, nil
}

func (s *Scanner) scanIntradaySnapshot(ctx context.Context, universe Universe, minScore int, previousErrors []string) (*Report, error) {
	started := time.Now().In(s.location)
	report := &Report{
		Mode:            ModeIntraday,
		UniverseKey:     universe.Key,
		UniverseName:    universe.Label,
		StartedAt:       started,
		TotalSymbols:    len(universe.Symbols),
		MinScore:        minScore,
		MaxResults:      s.maxResults,
		Source:          "TradingView scanner fallback",
		IntervalSummary: "1h + 15m technical snapshot",
		FilterSummary:   fmt.Sprintf("Skor >= %d, 1h RSI < 80, 15m RSI < 80", minScore),
		ErrorSamples:    previousErrors,
	}

	batch := s.snapshotClient.FetchSnapshots(ctx, universe.Symbols, intradaySnapshotColumns)
	report.DataSymbols = len(batch.Data)
	report.FailedSymbols = len(batch.Errors)
	report.ErrorSamples = append(report.ErrorSamples, sampleErrors(batch.Errors, 5)...)

	var signals []Signal
	for _, symbol := range universe.Symbols {
		snapshot, ok := batch.Data[symbol]
		if !ok {
			continue
		}
		signal, ok := analyzeIntradaySnapshot(symbol, snapshot)
		if !ok {
			continue
		}
		report.AnalyzedSymbols++
		if signal.Score >= minScore && signal.RSI1H < 80 && signal.RSI15M < 80 {
			signals = append(signals, signal)
		}
	}

	sort.Slice(signals, func(i, j int) bool {
		if signals[i].Score != signals[j].Score {
			return signals[i].Score > signals[j].Score
		}
		return signals[i].RSI1H < signals[j].RSI1H
	})
	report.Results = limitSignals(signals, s.maxResults)
	report.FinishedAt = time.Now().In(s.location)

	if report.DataSymbols == 0 {
		return report, fmt.Errorf("no intraday market data could be downloaded")
	}
	return report, nil
}

func analyzeDailySnapshot(symbol string, snapshot market.Snapshot) (Signal, bool) {
	price, ok, _ := snapshotPriceValue(snapshot, "close")
	if !ok {
		return Signal{}, false
	}
	rsi, ok, _ := snapshotRSIValue(snapshot, "RSI")
	if !ok {
		return Signal{}, false
	}
	s20, _, _ := snapshotLevelValue(snapshot, "SMA20", price)
	s50, _, _ := snapshotLevelValue(snapshot, "SMA50", price)
	s200, _, _ := snapshotLevelValue(snapshot, "SMA200", price)
	vwap, _, _ := snapshotLevelValue(snapshot, "VWAP", price)
	volumeX, _, _ := snapshotVolumeRatioValue(snapshot, "relative_volume_10d_calc")
	recommend, _, _ := snapshotRecommendValue(snapshot, "Recommend.All")

	score := 0
	details := make([]string, 0, 8)
	if finite(s200) {
		if price >= s200 {
			score++
			details = append(details, "Boga")
		} else {
			score -= 2
			details = append(details, "Ayi Trend (SMA200 alti)")
		}
	}
	if finite(s20, s50) && price > s20 && s20 > s50 {
		score++
		details = append(details, "Kusursuz Trend (F>S20>S50)")
	}
	if finite(vwap) && price > vwap {
		score++
		details = append(details, "VWAP Ustu")
	}
	if rsi > 50 && rsi < 70 {
		score++
		details = append(details, "Ideal RSI")
	}
	if volumeX > 1.5 {
		score++
		details = append(details, fmt.Sprintf("HacimX%.1f", volumeX))
	}
	if recommend > 0.5 {
		score += 2
		details = append(details, "TV Guclu Al")
	} else if recommend > 0.1 {
		score++
		details = append(details, "TV Al")
	} else if recommend < -0.1 {
		score--
		details = append(details, "TV Zayif")
	}

	approval := "RISKLI"
	if finite(vwap) && price > vwap && recommend > 0 {
		approval = "GUVENLI"
	}
	return Signal{
		Symbol:   symbol,
		Score:    score,
		Price:    round(price, 2),
		RSI:      round(rsi, 1),
		VolumeX:  round(volumeX, 2),
		Approval: approval,
		Details:  details,
	}, true
}

func analyzeIntradaySnapshot(symbol string, snapshot market.Snapshot) (Signal, bool) {
	price, ok, _ := snapshotPriceValue(snapshot, "close")
	if !ok {
		return Signal{}, false
	}
	rsi1H, ok1, _ := snapshotRSIValue(snapshot, "RSI|60")
	rsi15M, ok15, _ := snapshotRSIValue(snapshot, "RSI|15")
	if !ok1 || !ok15 {
		return Signal{}, false
	}

	s20H, _, _ := snapshotLevelValue(snapshot, "SMA20|60", price)
	s50H, _, _ := snapshotLevelValue(snapshot, "SMA50|60", price)
	s200H, _, _ := snapshotLevelValue(snapshot, "SMA200|60", price)
	vwapH, _, _ := snapshotLevelValue(snapshot, "VWAP|60", price)
	volumeH, _, _ := snapshotVolumeRatioValue(snapshot, "relative_volume_10d_calc|60")
	recommendH, _, _ := snapshotRecommendValue(snapshot, "Recommend.All|60")
	vwap15, _, _ := snapshotLevelValue(snapshot, "VWAP|15", price)
	volume15, _, _ := snapshotVolumeRatioValue(snapshot, "relative_volume_10d_calc|15")
	recommend15, _, _ := snapshotRecommendValue(snapshot, "Recommend.All|15")

	score := 0
	details := make([]string, 0, 8)
	if finite(vwapH) && price > vwapH {
		score += 2
	}
	if finite(vwap15) && price > vwap15 {
		score += 2
	}
	if finite(s20H, s50H) && price > s20H && s20H > s50H {
		score++
		details = append(details, "1h Boga Trendi")
	}
	if finite(s200H) && price < s200H {
		score -= 2
		details = append(details, "1h SMA200 Alti")
	}
	if recommendH > 0.2 {
		score++
		details = append(details, "1h TV Al")
	}
	if recommend15 > 0.2 {
		score++
		details = append(details, "15m TV Al")
	}
	if volumeH > 1.5 || volume15 > 1.5 {
		score++
		details = append(details, "Hacim Sicramasi")
	}

	return Signal{
		Symbol:     symbol,
		Score:      score,
		Price:      round(price, 2),
		RSI1H:      round(rsi1H, 1),
		RSI15M:     round(rsi15M, 1),
		VolumeX1H:  round(volumeH, 1),
		VolumeX15M: round(volume15, 1),
		VWAP1H:     upperLower(price, vwapH),
		VWAP15M:    upperLower(price, vwap15),
		Details:    details,
	}, true
}

func snapshotValue(snapshot market.Snapshot, key string) (float64, bool) {
	value, ok := snapshot.Values[key]
	if !ok || !finite(value) {
		return 0, false
	}
	return value, true
}
