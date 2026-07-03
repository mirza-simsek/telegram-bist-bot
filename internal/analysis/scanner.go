package analysis

import (
	"context"
	"fmt"
	"sort"
	"time"

	"telegram-bist-bot/internal/market"
)

type Scanner struct {
	client         MarketClient
	snapshotClient SnapshotClient
	location       *time.Location
	maxResults     int
	pythonScanner  *PythonScanner
}

func NewScanner(client MarketClient, snapshotClient SnapshotClient, location *time.Location, maxResults int) *Scanner {
	return &Scanner{
		client:         client,
		snapshotClient: snapshotClient,
		location:       location,
		maxResults:     maxResults,
	}
}

func (s *Scanner) SetPythonScanner(pythonScanner *PythonScanner) {
	s.pythonScanner = pythonScanner
}

func (s *Scanner) ScanDaily(ctx context.Context, universe Universe, minScore int) (*Report, error) {
	if s.pythonScanner != nil {
		return s.pythonScanner.ScanDaily(ctx, universe, minScore)
	}
	if len(universe.Symbols) == 0 {
		return nil, fmt.Errorf("%s universe has no symbols", universe.Label)
	}
	started := time.Now().In(s.location)
	report := &Report{
		Mode:            ModeDaily,
		UniverseKey:     universe.Key,
		UniverseName:    universe.Label,
		StartedAt:       started,
		TotalSymbols:    len(universe.Symbols),
		MinScore:        minScore,
		MaxResults:      s.maxResults,
		Source:          "TradingView chart OHLCV",
		IntervalSummary: "1y / 1d",
		FilterSummary:   fmt.Sprintf("Skor >= %d, RSI < 80", minScore),
	}

	batch := s.client.FetchMany(ctx, universe.Symbols, "1y", "1d")
	report.DataSymbols = len(batch.Data)
	report.FailedSymbols = len(batch.Errors)
	report.ErrorSamples = sampleErrors(batch.Errors, 5)

	now := time.Now().In(s.location)
	var signals []Signal
	for _, symbol := range universe.Symbols {
		candles, ok := batch.Data[symbol]
		if !ok {
			continue
		}
		signal, ok := analyzeDaily(symbol, candles, now)
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
		if s.snapshotClient != nil {
			return s.scanDailySnapshot(ctx, universe, minScore, report.ErrorSamples)
		}
		return report, fmt.Errorf("no daily market data could be downloaded")
	}
	return report, nil
}

func (s *Scanner) ScanIntraday(ctx context.Context, universe Universe, minScore int) (*Report, error) {
	if s.pythonScanner != nil {
		return s.pythonScanner.ScanIntraday(ctx, universe, minScore)
	}
	if len(universe.Symbols) == 0 {
		return nil, fmt.Errorf("%s universe has no symbols", universe.Label)
	}
	started := time.Now().In(s.location)
	report := &Report{
		Mode:            ModeIntraday,
		UniverseKey:     universe.Key,
		UniverseName:    universe.Label,
		StartedAt:       started,
		TotalSymbols:    len(universe.Symbols),
		MinScore:        minScore,
		MaxResults:      s.maxResults,
		Source:          "TradingView chart OHLCV",
		IntervalSummary: "3mo / 1h + 1mo / 15m",
		FilterSummary:   fmt.Sprintf("Skor >= %d, 1h RSI < 80, 15m RSI < 80", minScore),
	}

	type batchOut struct {
		name   string
		result market.BatchResult
	}
	out := make(chan batchOut, 2)
	go func() {
		out <- batchOut{name: "1h", result: s.client.FetchMany(ctx, universe.Symbols, "3mo", "1h")}
	}()
	go func() {
		out <- batchOut{name: "15m", result: s.client.FetchMany(ctx, universe.Symbols, "1mo", "15m")}
	}()

	var oneHour market.BatchResult
	var fifteenMin market.BatchResult
	for i := 0; i < 2; i++ {
		item := <-out
		if item.name == "1h" {
			oneHour = item.result
		} else {
			fifteenMin = item.result
		}
	}

	report.DataSymbols = minInt(len(oneHour.Data), len(fifteenMin.Data))
	report.FailedSymbols = maxInt(len(oneHour.Errors), len(fifteenMin.Errors))
	report.ErrorSamples = append(sampleErrors(oneHour.Errors, 3), sampleErrors(fifteenMin.Errors, 3)...)

	var signals []Signal
	for _, symbol := range universe.Symbols {
		data1H, ok1 := oneHour.Data[symbol]
		data15M, ok15 := fifteenMin.Data[symbol]
		if !ok1 || !ok15 {
			continue
		}
		signal, ok := analyzeIntraday(symbol, data1H, data15M, s.location)
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

	if len(oneHour.Data) == 0 || len(fifteenMin.Data) == 0 {
		if s.snapshotClient != nil {
			return s.scanIntradaySnapshot(ctx, universe, minScore, report.ErrorSamples)
		}
		return report, fmt.Errorf("no intraday market data could be downloaded")
	}
	return report, nil
}

func analyzeDaily(symbol string, candles []market.Candle, now time.Time) (Signal, bool) {
	if len(candles) < 200 {
		return Signal{}, false
	}
	closes := closeSeries(candles)
	price := candles[len(candles)-1].Close
	s20 := smaLast(closes, 20)
	s50 := smaLast(closes, 50)
	s200 := smaLast(closes, 200)
	rsi := rsiLast(closes, 14)
	vwap := rollingVWAPLast(candles, 20)
	poc := pocLast(candles, 22, 50)
	atr := atrLast(candles, 14)
	if !finite(price, s20, s50, s200, rsi, vwap, poc, atr) {
		return Signal{}, false
	}

	_, nearS3, aboveS3 := pivotS3Status(candles)
	volRatio := volumeRatio(candles, true, now)
	score := 0
	details := make([]string, 0, 8)

	if price < s200 {
		score -= 2
		details = append(details, "Ayi Trend (SMA200 alti)")
	} else {
		score++
		details = append(details, "Boga")
	}
	if trpBuySignal(closes) {
		score += 3
		details = append(details, "TRP(9) AL")
	}
	if obvBreakout(candles) {
		score += 2
		details = append(details, "OBV Kirilim")
	}
	if nearS3 && aboveS3 {
		score++
		details = append(details, "S3 Destek")
	}
	if price > s20 && s20 > s50 {
		score++
		details = append(details, "Kusursuz Trend (F>S20>S50)")
	}
	if rsi > 50 && rsi < 70 {
		score++
		details = append(details, "Ideal RSI")
	}
	if volRatio > 1.5 {
		score++
		details = append(details, fmt.Sprintf("HacimX%.1f", volRatio))
	}

	approval := "RISKLI"
	if price > poc && price > vwap {
		approval = "GUVENLI"
	}

	return Signal{
		Symbol:   symbol,
		Score:    score,
		Price:    round(price, 2),
		StopLoss: round(price-(1.5*atr), 2),
		RSI:      round(rsi, 1),
		VolumeX:  round(volRatio, 2),
		Approval: approval,
		Details:  details,
	}, true
}

func analyzeIntraday(symbol string, data1H []market.Candle, data15M []market.Candle, loc *time.Location) (Signal, bool) {
	if len(data1H) < 200 || len(data15M) < 200 {
		return Signal{}, false
	}
	m1H, ok1 := intradayMetrics(data1H, 80, loc)
	m15M, ok15 := intradayMetrics(data15M, 40, loc)
	if !ok1 || !ok15 {
		return Signal{}, false
	}

	price := m1H.price
	score := 0
	details := make([]string, 0, 8)

	if price > m1H.vwap && price > m1H.poc {
		score += 2
	}
	if price > m15M.vwap && price > m15M.poc {
		score += 2
	}
	if price > m1H.s20 && m1H.s20 > m1H.s50 {
		score++
		details = append(details, "1h Boga Trendi")
	}
	if price < m1H.s200 {
		score -= 2
		details = append(details, "1h SMA200 Alti")
	}
	if m1H.trp || m15M.trp {
		score++
		details = append(details, "TRP(9) AL")
	}
	if m1H.obv {
		score++
		details = append(details, "1h OBV")
	}
	if m15M.obv {
		score++
		details = append(details, "15m OBV")
	}
	if price > m1H.vah {
		score++
		details = append(details, "1h VAH Kirilimi")
	}
	if m1H.volumeX > 1.5 || m15M.volumeX > 1.5 {
		score++
		details = append(details, "Hacim Sicramasi")
	}

	return Signal{
		Symbol:     symbol,
		Score:      score,
		Price:      round(price, 2),
		StopLoss:   round(price-(1.5*m1H.atr), 2),
		RSI1H:      round(m1H.rsi, 1),
		RSI15M:     round(m15M.rsi, 1),
		VolumeX1H:  round(m1H.volumeX, 1),
		VolumeX15M: round(m15M.volumeX, 1),
		VWAP1H:     upperLower(price, m1H.vwap),
		POC1H:      upperLower(price, m1H.poc),
		VWAP15M:    upperLower(price, m15M.vwap),
		POC15M:     upperLower(price, m15M.poc),
		Details:    details,
	}, true
}

type metrics struct {
	price   float64
	s20     float64
	s50     float64
	s200    float64
	rsi     float64
	vwap    float64
	poc     float64
	vah     float64
	atr     float64
	trp     bool
	obv     bool
	volumeX float64
}

func intradayMetrics(candles []market.Candle, pocLookback int, loc *time.Location) (metrics, bool) {
	closes := closeSeries(candles)
	poc, vah := volumeProfile(candles, pocLookback, 50, 0.70)
	m := metrics{
		price:   candles[len(candles)-1].Close,
		s20:     smaLast(closes, 20),
		s50:     smaLast(closes, 50),
		s200:    smaLast(closes, 200),
		rsi:     rsiLast(closes, 14),
		vwap:    intradayVWAPLast(candles, loc),
		poc:     poc,
		vah:     vah,
		atr:     atrLast(candles, 14),
		trp:     trpBuySignal(closes),
		obv:     obvBreakout(candles),
		volumeX: volumeRatio(candles, false, time.Time{}),
	}
	return m, finite(m.price, m.s20, m.s50, m.s200, m.rsi, m.vwap, m.poc, m.vah, m.atr)
}

func upperLower(price float64, level float64) string {
	if price > level {
		return "Ust"
	}
	return "Alt"
}

func limitSignals(signals []Signal, maxResults int) []Signal {
	if maxResults <= 0 || len(signals) <= maxResults {
		return signals
	}
	return signals[:maxResults]
}

func sampleErrors(errorsBySymbol map[string]error, limit int) []string {
	if limit <= 0 || len(errorsBySymbol) == 0 {
		return nil
	}
	symbols := make([]string, 0, len(errorsBySymbol))
	for symbol := range errorsBySymbol {
		symbols = append(symbols, symbol)
	}
	sort.Strings(symbols)
	samples := make([]string, 0, limit)
	for _, symbol := range symbols {
		if len(samples) >= limit {
			break
		}
		samples = append(samples, fmt.Sprintf("%s: %v", symbol, errorsBySymbol[symbol]))
	}
	return samples
}

func minInt(a int, b int) int {
	if a < b {
		return a
	}
	return b
}

func maxInt(a int, b int) int {
	if a > b {
		return a
	}
	return b
}
