package analysis

import (
	"context"
	"fmt"
	"strings"
	"time"

	"telegram-bist-bot/internal/market"
)

var symbolAnalysisColumns = []string{
	"close",
	"EMA9|15",
	"EMA20|15",
	"VWAP|15",
	"RSI|15",
	"Recommend.All|15",
	"relative_volume_10d_calc|15",
	"SMA200|15",
	"EMA9|60",
	"EMA20|60",
	"VWAP|60",
	"RSI|60",
	"Recommend.All|60",
	"relative_volume_10d_calc|60",
	"SMA200|60",
	"EMA9",
	"EMA20",
	"VWAP",
	"RSI",
	"Recommend.All",
	"relative_volume_10d_calc",
	"SMA200",
}

var symbolTimeframes = []struct {
	key    string
	label  string
	suffix string
}{
	{key: "15m", label: "15 Dakika", suffix: "|15"},
	{key: "1h", label: "1 Saat", suffix: "|60"},
	{key: "1d", label: "Gunluk", suffix: ""},
}

func (s *Scanner) AnalyzeSymbol(ctx context.Context, rawSymbol string) (*SymbolAnalysis, error) {
	if s.snapshotClient == nil {
		return nil, fmt.Errorf("snapshot data source is not configured")
	}
	symbol := normalizeSymbol(rawSymbol)
	if symbol == "" {
		return nil, fmt.Errorf("symbol is empty")
	}

	batch := s.snapshotClient.FetchSnapshots(ctx, []string{symbol}, symbolAnalysisColumns)
	snapshot, ok := batch.Data[symbol]
	if !ok {
		if err := batch.Errors[symbol]; err != nil {
			return nil, err
		}
		for _, err := range batch.Errors {
			if err != nil {
				return nil, err
			}
		}
		return nil, fmt.Errorf("no snapshot data for %s", symbol)
	}

	analysis, ok := analyzeSymbolSnapshot(symbol, snapshot, time.Now().In(s.location))
	if !ok {
		return nil, fmt.Errorf("not enough technical data for %s", symbol)
	}
	return analysis, nil
}

func analyzeSymbolSnapshot(symbol string, snapshot market.Snapshot, finishedAt time.Time) (*SymbolAnalysis, bool) {
	price, ok, priceWarning := snapshotPriceValue(snapshot, "close")
	if !ok {
		return nil, false
	}

	timeframes := make([]TimeframeAnalysis, 0, len(symbolTimeframes))
	warnings := make([]string, 0)
	warnings = appendWarning(warnings, priceWarning)
	totalScore := 0
	totalMax := 0
	for _, spec := range symbolTimeframes {
		frame := buildTimeframeAnalysis(spec.key, spec.label, spec.suffix, price, snapshot)
		timeframes = append(timeframes, frame)
		warnings = append(warnings, frame.DataWarnings...)
		totalScore += frame.Score
		totalMax += frame.MaxScore
	}
	if totalMax == 0 {
		return nil, false
	}

	verdict, note := symbolVerdict(totalScore, totalMax, timeframes)
	return &SymbolAnalysis{
		Symbol:       symbol,
		Price:        round(price, 2),
		Score:        totalScore,
		MaxScore:     totalMax,
		Verdict:      verdict,
		VerdictNote:  note,
		Source:       "TradingView technical snapshot",
		FinishedAt:   finishedAt,
		Timeframes:   timeframes,
		DataWarnings: compactWarnings(warnings, 4),
	}, true
}

func buildTimeframeAnalysis(key string, label string, suffix string, price float64, snapshot market.Snapshot) TimeframeAnalysis {
	frame := TimeframeAnalysis{
		Key:      key,
		Label:    label,
		Price:    round(price, 2),
		MaxScore: 0,
		Notes:    make([]string, 0, 8),
	}

	rawScore := 0
	ema9, ok9, warning := snapshotLevelValue(snapshot, "EMA9"+suffix, price)
	frame.DataWarnings = appendWarning(frame.DataWarnings, warning)
	if ok9 {
		ema20, ok20, warning := snapshotLevelValue(snapshot, "EMA20"+suffix, price)
		frame.DataWarnings = appendWarning(frame.DataWarnings, warning)
		if ok20 {
			frame.EMA9 = round(ema9, 2)
			frame.EMA20 = round(ema20, 2)
			frame.MaxScore += 2
			switch {
			case price > ema9 && ema9 > ema20:
				rawScore += 2
				frame.Notes = append(frame.Notes, "Fiyat EMA9/EMA20 uzerinde; kisa ortalama dizilimi pozitif.")
			case price > ema20:
				rawScore++
				frame.Notes = append(frame.Notes, "Fiyat EMA20 uzerinde, fakat kisa ortalama dizilimi tam guclu degil.")
			case price < ema9 && price < ema20:
				rawScore--
				frame.Notes = append(frame.Notes, "Fiyat EMA9 ve EMA20 altinda; momentum zayif.")
			default:
				frame.Notes = append(frame.Notes, "Fiyat kisa ortalamalar arasinda; net yon teyidi yok.")
			}
		}
	}

	if vwap, ok, warning := snapshotLevelValue(snapshot, "VWAP"+suffix, price); ok {
		frame.VWAP = round(vwap, 2)
		frame.MaxScore++
		if price > vwap {
			rawScore++
			frame.Notes = append(frame.Notes, "Fiyat VWAP uzerinde.")
		} else {
			rawScore--
			frame.Notes = append(frame.Notes, "Fiyat VWAP altinda.")
		}
	} else {
		frame.DataWarnings = appendWarning(frame.DataWarnings, warning)
	}

	if rsi, ok, warning := snapshotRSIValue(snapshot, "RSI"+suffix); ok {
		frame.RSI = round(rsi, 1)
		frame.MaxScore++
		switch {
		case rsi >= 50 && rsi <= 68:
			rawScore++
			frame.Notes = append(frame.Notes, "RSI guclu bolgede ve asiri isinmis degil.")
		case rsi > 75:
			rawScore--
			frame.Notes = append(frame.Notes, "RSI cok yuksek; yukari hareket yorulmus olabilir.")
		case rsi > 68:
			frame.Notes = append(frame.Notes, "RSI guclu ama isinan bolgede.")
		case rsi < 45:
			rawScore--
			frame.Notes = append(frame.Notes, "RSI 45 altinda; momentum zayif.")
		default:
			frame.Notes = append(frame.Notes, "RSI karar bolgesinde.")
		}
	} else {
		frame.DataWarnings = appendWarning(frame.DataWarnings, warning)
	}

	if recommend, ok, warning := snapshotRecommendValue(snapshot, "Recommend.All"+suffix); ok {
		frame.Recommend = round(recommend, 2)
		frame.MaxScore++
		switch {
		case recommend > 0.2:
			rawScore++
			frame.Notes = append(frame.Notes, "Teknik skor pozitif.")
		case recommend < -0.2:
			rawScore--
			frame.Notes = append(frame.Notes, "Teknik skor negatif.")
		default:
			frame.Notes = append(frame.Notes, "Teknik skor notr.")
		}
	} else {
		frame.DataWarnings = appendWarning(frame.DataWarnings, warning)
	}

	if sma200, ok, warning := snapshotLevelValue(snapshot, "SMA200"+suffix, price); ok {
		frame.SMA200 = round(sma200, 2)
		frame.MaxScore++
		if price > sma200 {
			rawScore++
			frame.Notes = append(frame.Notes, "Fiyat SMA200 uzerinde; ana trend destekli.")
		} else {
			rawScore--
			frame.Notes = append(frame.Notes, "Fiyat SMA200 altinda; ana trend temkinli.")
		}
	} else {
		frame.DataWarnings = appendWarning(frame.DataWarnings, warning)
	}

	if volumeX, ok, warning := snapshotVolumeRatioValue(snapshot, "relative_volume_10d_calc"+suffix); ok && volumeX > 0 {
		frame.VolumeX = round(volumeX, 1)
		frame.MaxScore++
		switch {
		case volumeX >= 1.5:
			rawScore++
			frame.Notes = append(frame.Notes, "Hacim ortalamanin uzerinde.")
		case volumeX < 0.7:
			frame.Notes = append(frame.Notes, "Hacim zayif; hareketin teyidi sinirli.")
		default:
			frame.Notes = append(frame.Notes, "Hacim normal bolgede.")
		}
	} else {
		frame.DataWarnings = appendWarning(frame.DataWarnings, warning)
	}

	frame.Score = rawScore
	if frame.Score < 0 {
		frame.Score = 0
	}
	frame.Bias = timeframeBias(rawScore, frame.MaxScore)
	if len(frame.Notes) == 0 {
		frame.Notes = append(frame.Notes, "Bu zaman dilimi icin yeterli teknik teyit yok.")
	}
	return frame
}

func symbolVerdict(score int, maxScore int, timeframes []TimeframeAnalysis) (string, string) {
	ratio := float64(score) / float64(maxScore)
	positiveFrames := 0
	for _, frame := range timeframes {
		if frame.Bias == "Pozitif" {
			positiveFrames++
		}
	}

	switch {
	case ratio >= 0.65 && positiveFrames >= 2:
		return "Alim radarinda", "Birden fazla zaman diliminde teknik gorunum pozitif. Yine de son karar icin kendi grafikte teyit gerekir."
	case ratio >= 0.48:
		return "Takip edilebilir", "Teknik tablo karisik ama izlemeye deger sinyaller var. Net giris icin ek teyit aranir."
	default:
		return "Teyit bekliyor", "Mevcut teknik gorunum guclu alim radari uretmiyor."
	}
}

func timeframeBias(score int, maxScore int) string {
	if maxScore <= 0 {
		return "Veri yetersiz"
	}
	ratio := float64(score) / float64(maxScore)
	switch {
	case ratio >= 0.55:
		return "Pozitif"
	case ratio >= 0.25:
		return "Notr"
	default:
		return "Zayif"
	}
}

func normalizeSymbol(raw string) string {
	symbol := strings.ToUpper(strings.TrimSpace(raw))
	symbol = strings.TrimPrefix(symbol, "/")
	if colon := strings.Index(symbol, ":"); colon >= 0 {
		symbol = symbol[colon+1:]
	}
	if dot := strings.Index(symbol, "."); dot >= 0 {
		symbol = symbol[:dot]
	}
	return symbol
}
