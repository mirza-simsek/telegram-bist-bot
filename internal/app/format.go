package app

import (
	"fmt"
	"html"
	"sort"
	"strings"
	"time"

	"telegram-bist-bot/internal/analysis"
)

func formatReport(report *analysis.Report, title string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "<b>%s</b>\n", html.EscapeString(title))
	fmt.Fprintf(&b, "<code>%s</code> | <code>%s</code>\n\n", html.EscapeString(report.UniverseName), html.EscapeString(report.FinishedAt.Format("02.01.2006 15:04")))

	if len(report.Results) == 0 {
		b.WriteString("<b>Sonuc</b>\n")
		b.WriteString("Esigi gecen aday yok. Bu, filtrelerin su an yeterince guclu bir alim radari uretmedigi anlamina gelir.\n\n")
		b.WriteString("Teknik tarama notudur; yatirim tavsiyesi degildir.")
		return b.String()
	}

	fmt.Fprintf(&b, "<b>Radar Listesi</b> <code>%d aday</code>\n", len(report.Results))
	b.WriteString("Detayli teknik inceleme icin on eleme listesidir.\n\n")
	switch report.Mode {
	case analysis.ModeDaily:
		for idx, signal := range report.Results {
			formatDailyCandidate(&b, idx+1, signal)
		}
	case analysis.ModeIntraday:
		for idx, signal := range report.Results {
			formatIntradayCandidate(&b, idx+1, signal)
		}
	}

	b.WriteString("\n")
	b.WriteString("Teknik tarama notudur; yatirim tavsiyesi degildir.")
	return b.String()
}

func formatScanStarted(title string, universe analysis.Universe, minScore int) string {
	return fmt.Sprintf(`<b>%s basladi</b>
Kapsam: <code>%s (%d sembol)</code>
Esik: <code>Skor >= %d</code>

Veri indiriliyor ve indikatorler hesaplaniyor. Sonuc hazir olunca raporu buraya gonderecegim.`,
		html.EscapeString(title),
		html.EscapeString(universe.Label),
		len(universe.Symbols),
		minScore,
	)
}

func formatScanError(title string, universe analysis.Universe, err error) string {
	message := "Tarama tamamlanamadi."
	errText := err.Error()
	if strings.Contains(errText, "context canceled") {
		message = "Tarama kullanici istegiyle durduruldu."
	} else if strings.Contains(errText, "market data could be downloaded") || strings.Contains(errText, "too little market data") || strings.Contains(errText, "context deadline exceeded") || strings.Contains(errText, "Client.Timeout") {
		message = "Veri kaynagi su an yanit vermiyor veya cok yavas. Tarama erken durduruldu."
	}
	return fmt.Sprintf(`<b>%s tamamlanamadi</b>
Kapsam: <code>%s (%d sembol)</code>

%s
Detay: <code>%s</code>

Biraz sonra tekrar deneyebilirsin. Sorun devam ederse veri kaynagini veya timeout ayarlarini degistiririz.`,
		html.EscapeString(title),
		html.EscapeString(universe.Label),
		len(universe.Symbols),
		html.EscapeString(message),
		html.EscapeString(errText),
	)
}

func shortDuration(d time.Duration) string {
	if d < time.Second {
		return d.String()
	}
	if d < time.Minute {
		return fmt.Sprintf("%.0fs", d.Seconds())
	}
	return fmt.Sprintf("%dm%02ds", int(d.Minutes()), int(d.Seconds())%60)
}

func formatDailyCandidate(b *strings.Builder, rank int, signal analysis.Signal) {
	fmt.Fprintf(b, "%d. <b>%s</b> | <code>%.2f TL</code> | Skor <code>%d/10</code>\n",
		rank,
		html.EscapeString(signal.Symbol),
		signal.Price,
		signal.Score,
	)
	parts := make([]string, 0, 3)
	if signal.RSI > 0 {
		parts = append(parts, fmt.Sprintf("RSI <code>%.1f</code>", signal.RSI))
	}
	if signal.VolumeX > 0 {
		parts = append(parts, fmt.Sprintf("Hacim <code>x%.1f</code>", signal.VolumeX))
	}
	if signal.Approval != "" {
		parts = append(parts, fmt.Sprintf("Onay <code>%s</code>", html.EscapeString(prettyApproval(signal.Approval))))
	}
	if len(parts) > 0 {
		fmt.Fprintf(b, "%s\n", strings.Join(parts, " | "))
	}
	if reason := compactDetails(signal.Details, 6); reason != "" {
		fmt.Fprintf(b, "Sinyal: %s\n", html.EscapeString(reason))
	}
	b.WriteString("\n")
}

func formatIntradayCandidate(b *strings.Builder, rank int, signal analysis.Signal) {
	fmt.Fprintf(b, "%d. <b>%s</b> | <code>%.2f TL</code> | Skor <code>%d/10</code>\n",
		rank,
		html.EscapeString(signal.Symbol),
		signal.Price,
		signal.Score,
	)
	fmt.Fprintf(b, "Konum: <code>1s VWAP/POC %s/%s</code> | <code>15d VWAP/POC %s/%s</code>\n",
		html.EscapeString(positionText(signal.VWAP1H)),
		html.EscapeString(positionText(signal.POC1H)),
		html.EscapeString(positionText(signal.VWAP15M)),
		html.EscapeString(positionText(signal.POC15M)),
	)
	momentum := make([]string, 0, 2)
	if signal.RSI1H > 0 || signal.RSI15M > 0 {
		momentum = append(momentum, fmt.Sprintf("RSI <code>1s %.1f</code> / <code>15d %.1f</code>", signal.RSI1H, signal.RSI15M))
	}
	if signal.VolumeX1H > 0 || signal.VolumeX15M > 0 {
		momentum = append(momentum, fmt.Sprintf("Hacim <code>1s x%.1f</code> / <code>15d x%.1f</code>", signal.VolumeX1H, signal.VolumeX15M))
	}
	if len(momentum) > 0 {
		fmt.Fprintf(b, "%s\n", strings.Join(momentum, " | "))
	}
	if reason := compactDetails(signal.Details, 6); reason != "" {
		fmt.Fprintf(b, "Sinyal: %s\n", html.EscapeString(reason))
	}
	b.WriteString("\n")
}

func formatSymbolStarted(symbol string) string {
	return fmt.Sprintf(`<b>%s teknik karti hazirlaniyor</b>
15dk, 1s ve gunluk teknik gorunum kontrol ediliyor.`,
		html.EscapeString(symbol),
	)
}

func formatSymbolError(symbol string, err error) string {
	message := "Teknik kart hazirlanamadi."
	errText := err.Error()
	if strings.Contains(errText, "context canceled") {
		message = "Hisse analizi kullanici istegiyle durduruldu."
	} else if strings.Contains(errText, "deadline exceeded") || strings.Contains(errText, "Client.Timeout") {
		message = "Veri kaynagi su an yanit vermiyor veya cok yavas."
	}
	return fmt.Sprintf(`<b>%s teknik karti tamamlanamadi</b>

%s
Detay: <code>%s</code>`,
		html.EscapeString(symbol),
		html.EscapeString(message),
		html.EscapeString(errText),
	)
}

func formatSymbolAnalysis(card *analysis.SymbolAnalysis) string {
	var b strings.Builder
	fmt.Fprintf(&b, "<b>%s Teknik Karti</b>\n", html.EscapeString(card.Symbol))
	fmt.Fprintf(&b, "<code>%s</code> | Kaynak: <code>%s</code>\n", html.EscapeString(card.FinishedAt.Format("02.01.2006 15:04")), html.EscapeString(card.Source))
	fmt.Fprintf(&b, "Fiyat: <code>%.2f TL</code>\n", card.Price)
	fmt.Fprintf(&b, "Karar: <b>%s</b>\n", html.EscapeString(card.Verdict))
	fmt.Fprintf(&b, "Skor: <code>%d/%d</code>\n", card.Score, card.MaxScore)
	if card.VerdictNote != "" {
		fmt.Fprintf(&b, "%s\n", html.EscapeString(card.VerdictNote))
	}
	if len(card.DataWarnings) > 0 {
		fmt.Fprintf(&b, "Veri kontrolu: <code>%s</code>\n", html.EscapeString(strings.Join(card.DataWarnings, " | ")))
	}

	for _, frame := range card.Timeframes {
		b.WriteString("\n")
		formatTimeframeAnalysis(&b, frame)
	}

	b.WriteString("\nTeknik tarama notudur; yatirim tavsiyesi degildir.")
	return b.String()
}

func formatTimeframeAnalysis(b *strings.Builder, frame analysis.TimeframeAnalysis) {
	fmt.Fprintf(b, "<b>%s</b> - <code>%s</code> (<code>%d/%d</code>)\n",
		html.EscapeString(frame.Label),
		html.EscapeString(frame.Bias),
		frame.Score,
		frame.MaxScore,
	)
	if position := formatPositionLine(frame); position != "" {
		fmt.Fprintf(b, "Konum: %s\n", position)
	}
	if levels := formatLevelsLine(frame); levels != "" {
		fmt.Fprintf(b, "Seviyeler: %s\n", levels)
	}
	if momentum := formatMomentumLine(frame); momentum != "" {
		fmt.Fprintf(b, "Momentum: %s\n", momentum)
	}
	fmt.Fprintf(b, "Teknik yorum: %s\n", html.EscapeString(timeframeNarrative(frame)))
}

func formatPositionLine(frame analysis.TimeframeAnalysis) string {
	parts := make([]string, 0, 4)
	parts = appendPosition(parts, frame.Price, "EMA9", frame.EMA9)
	parts = appendPosition(parts, frame.Price, "EMA20", frame.EMA20)
	parts = appendPosition(parts, frame.Price, "VWAP", frame.VWAP)
	parts = appendPosition(parts, frame.Price, "SMA200", frame.SMA200)
	return strings.Join(parts, " / ")
}

func appendPosition(parts []string, price float64, label string, level float64) []string {
	if level <= 0 {
		return parts
	}
	position := "alti"
	if price > level {
		position = "ustu"
	}
	return append(parts, fmt.Sprintf("<code>%s %s</code>", html.EscapeString(label), position))
}

func formatLevelsLine(frame analysis.TimeframeAnalysis) string {
	parts := make([]string, 0, 4)
	parts = appendLevel(parts, "EMA9", frame.EMA9)
	parts = appendLevel(parts, "EMA20", frame.EMA20)
	parts = appendLevel(parts, "VWAP", frame.VWAP)
	parts = appendLevel(parts, "SMA200", frame.SMA200)
	return strings.Join(parts, " | ")
}

func appendLevel(parts []string, label string, value float64) []string {
	if value <= 0 {
		return parts
	}
	return append(parts, fmt.Sprintf("%s <code>%.2f</code>", html.EscapeString(label), value))
}

func formatMomentumLine(frame analysis.TimeframeAnalysis) string {
	parts := make([]string, 0, 2)
	if frame.RSI > 0 {
		parts = append(parts, fmt.Sprintf("RSI <code>%.1f</code>", frame.RSI))
	}
	if frame.VolumeX > 0 {
		parts = append(parts, fmt.Sprintf("Hacim <code>x%.1f</code>", frame.VolumeX))
	}
	return strings.Join(parts, " | ")
}

func timeframeNarrative(frame analysis.TimeframeAnalysis) string {
	sentences := make([]string, 0, 4)
	if trend := shortTrendSentence(frame); trend != "" {
		sentences = append(sentences, trend)
	}
	if vwap := vwapSentence(frame); vwap != "" {
		sentences = append(sentences, vwap)
	}
	if momentum := momentumSentence(frame); momentum != "" {
		sentences = append(sentences, momentum)
	}
	if trigger := triggerSentence(frame); trigger != "" {
		sentences = append(sentences, trigger)
	}
	if len(sentences) == 0 {
		return strings.Join(limitStrings(frame.Notes, 2), " ")
	}
	return strings.Join(sentences, " ")
}

func shortTrendSentence(frame analysis.TimeframeAnalysis) string {
	hasEMA := frame.EMA9 > 0 && frame.EMA20 > 0
	hasSMA200 := frame.SMA200 > 0
	switch {
	case hasEMA && frame.Price > frame.EMA9 && frame.EMA9 > frame.EMA20:
		if hasSMA200 && frame.Price > frame.SMA200 {
			return "Fiyat kisa ortalamalarin ve SMA200'un uzerinde; trend yapisi destekli."
		}
		return "Fiyat kisa ortalamalarin uzerinde; EMA9'un EMA20 uzerinde kalmasi kisa trendi destekliyor."
	case hasEMA && frame.Price > frame.EMA20:
		return "Fiyat EMA20 uzerinde, ancak kisa ortalama dizilimi henuz tam guclenmis degil."
	case hasEMA && frame.Price < frame.EMA9 && frame.Price < frame.EMA20:
		if hasSMA200 && frame.Price > frame.SMA200 {
			return "Fiyat kisa ortalamalarin altinda, fakat SMA200 uzeri ana gorunum tamamen bozulmamis."
		}
		return "Fiyat kisa ortalamalarin altinda; satici baskisi bu zaman diliminde belirgin."
	case hasEMA:
		return "Fiyat EMA9/EMA20 arasinda; yon karari henuz netlesmemis."
	case hasSMA200 && frame.Price > frame.SMA200:
		return "Fiyat SMA200 uzerinde; ana trend tarafinda olumlu zemin korunuyor."
	case hasSMA200:
		return "Fiyat SMA200 altinda; ana trend tarafinda temkinli gorunum var."
	default:
		return ""
	}
}

func vwapSentence(frame analysis.TimeframeAnalysis) string {
	if frame.VWAP <= 0 {
		return ""
	}
	if frame.Price > frame.VWAP {
		return "VWAP uzeri fiyatlama alici tarafinin ortalama maliyette avantajli oldugunu gosteriyor."
	}
	return fmt.Sprintf("VWAP %.2f altinda kalindigi icin tepki hareketinin once bu seviyeyi geri almasi gerekir.", frame.VWAP)
}

func momentumSentence(frame analysis.TimeframeAnalysis) string {
	hasRSI := frame.RSI > 0
	hasVolume := frame.VolumeX > 0
	switch {
	case hasRSI && frame.RSI < 35 && hasVolume && frame.VolumeX < 0.7:
		return "RSI zayif ve hacim dusuk; alici ilgisi henuz net bir tepkiye donusmemis."
	case hasRSI && frame.RSI < 45:
		return "RSI zayif bolgede; momentum toparlanmadan agresif sinyal gucu dusuk kalir."
	case hasRSI && frame.RSI > 75:
		return "RSI cok isinmis; pozitif gorunum olsa bile yeni giriste geri cekilme riski artar."
	case hasRSI && frame.RSI > 68:
		return "RSI guclu ama isinan bolgede; hareketin saglikli kalmasi icin yatay dinlenme iyi olur."
	case hasRSI && frame.RSI >= 50 && hasVolume && frame.VolumeX >= 1.5:
		return "RSI guclu bolgede ve hacim ortalama uzerinde; hareket teyit aliyor."
	case hasRSI && frame.RSI >= 50 && hasVolume && frame.VolumeX < 0.7:
		return "RSI dengeli, ancak hacim dusuk oldugu icin hareket teyidi sinirli."
	case hasRSI && frame.RSI >= 50:
		return "RSI dengeli guc bolgesinde; momentum olumsuz degil."
	case hasVolume && frame.VolumeX >= 1.5:
		return "Hacim ortalama uzerinde; fiyat hareketi piyasa ilgisiyle destekleniyor."
	case hasVolume && frame.VolumeX < 0.7:
		return "Hacim dusuk; fiyat hareketinin teyidi sinirli."
	default:
		return ""
	}
}

func triggerSentence(frame analysis.TimeframeAnalysis) string {
	switch frame.Bias {
	case "Pozitif":
		if supports := nearestLevels(frame, false, 2); len(supports) > 0 {
			return "Pozitif gorunumun korunmasi icin " + formatLevelList(supports) + " altina sarkma izlenmeli."
		}
	case "Notr":
		if resistances := nearestLevels(frame, true, 2); len(resistances) > 0 {
			return "Netlesme icin " + formatLevelList(resistances) + " uzeri kalicilik takip edilir."
		}
	case "Zayif":
		if resistances := nearestLevels(frame, true, 2); len(resistances) > 0 {
			return "Ilk toparlanma teyidi icin " + formatLevelList(resistances) + " uzeri kapanis aranir."
		}
	}
	if frame.Recommend > 0.2 {
		return "Genel teknik skor pozitif tarafta; teyit fiyatin seviyeler uzerinde kalmasiyla guclenir."
	}
	if frame.Recommend < -0.2 {
		return "Genel teknik skor negatif tarafta; acele sinyal yerine toparlanma teyidi beklenir."
	}
	return ""
}

type levelInfo struct {
	name  string
	value float64
}

func nearestLevels(frame analysis.TimeframeAnalysis, above bool, limit int) []levelInfo {
	levels := []levelInfo{
		{name: "EMA9", value: frame.EMA9},
		{name: "EMA20", value: frame.EMA20},
		{name: "VWAP", value: frame.VWAP},
		{name: "SMA200", value: frame.SMA200},
	}
	result := make([]levelInfo, 0, len(levels))
	for _, level := range levels {
		if level.value <= 0 {
			continue
		}
		if above && level.value > frame.Price {
			result = append(result, level)
		}
		if !above && level.value < frame.Price {
			result = append(result, level)
		}
	}
	sort.Slice(result, func(i, j int) bool {
		left := result[i].value - frame.Price
		if left < 0 {
			left = -left
		}
		right := result[j].value - frame.Price
		if right < 0 {
			right = -right
		}
		return left < right
	})
	if limit > 0 && len(result) > limit {
		return result[:limit]
	}
	return result
}

func formatLevelList(levels []levelInfo) string {
	parts := make([]string, 0, len(levels))
	for _, level := range levels {
		parts = append(parts, fmt.Sprintf("%s %.2f", level.name, level.value))
	}
	return strings.Join(parts, " ve ")
}

func limitStrings(values []string, limit int) []string {
	if limit <= 0 || len(values) <= limit {
		return values
	}
	return values[:limit]
}

func radarReason(details []string, intraday bool) string {
	reason := compactDetails(details, 3)
	if reason == "" || reason == "Teknik kosullar uyumlu" || reason == "Cift zaman/onay filtresi" {
		if intraday {
			return "Cift zaman diliminde teknik kosullar uyumlu"
		}
		return "Gunluk teknik kosullar uyumlu"
	}
	return reason
}

func compactDetails(details []string, limit int) string {
	if len(details) == 0 {
		return "Cift zaman/onay filtresi"
	}
	cleaned := make([]string, 0, len(details))
	seen := make(map[string]struct{})
	for _, detail := range details {
		label := prettyDetail(detail)
		if label == "" {
			continue
		}
		if _, ok := seen[label]; ok {
			continue
		}
		seen[label] = struct{}{}
		cleaned = append(cleaned, label)
		if len(cleaned) >= limit {
			break
		}
	}
	if len(cleaned) == 0 {
		return "Teknik kosullar uyumlu"
	}
	return strings.Join(cleaned, ", ")
}

func prettyDetail(detail string) string {
	switch {
	case strings.HasPrefix(detail, "HacimX"):
		return detail
	case detail == "Boga":
		return "SMA200 ustu"
	case detail == "boga":
		return "SMA200 ustu"
	case detail == "Ayi Trend (SMA200 alti)":
		return "SMA200 alti"
	case detail == "ayi trendi":
		return "SMA200 alti"
	case detail == "Kusursuz Trend (F>S20>S50)":
		return "Trend uyumu"
	case detail == "kusursuz trend":
		return "Trend uyumu"
	case detail == "Ideal RSI":
		return "RSI ideal"
	case detail == "ideal RSI":
		return "RSI ideal"
	case detail == "OBV Kirilim":
		return "OBV kirilimi"
	case detail == "OBV kirilim":
		return "OBV kirilimi"
	case detail == "S3 Destek":
		return "S3 destek"
	case detail == "S3 destek":
		return "S3 destek"
	case detail == "TRP(9) AL":
		return "TRP(9)"
	case detail == "1h Boga Trendi":
		return "1s trend"
	case detail == "1h boga trendi":
		return "1s trend"
	case detail == "1h SMA200 Alti":
		return "1s SMA200 alti"
	case detail == "1h SMA200 alti":
		return "1s SMA200 alti"
	case detail == "1h OBV":
		return "1s OBV"
	case detail == "15m OBV":
		return "15d OBV"
	case detail == "1h VAH Kirilimi":
		return "1s VAH kirilimi"
	case detail == "1h VAH kirilimi":
		return "1s VAH kirilimi"
	case detail == "Hacim Sicramasi":
		return "Hacim sicramasi"
	case detail == "hacim sicramasi":
		return "Hacim sicramasi"
	case detail == "VWAP Ustu":
		return "VWAP ustu"
	case detail == "TV Guclu Al":
		return "Teknik skor guclu"
	case detail == "TV Al":
		return "Teknik skor pozitif"
	case detail == "TV Zayif":
		return "Teknik skor zayif"
	case detail == "1h TV Al":
		return "1s teknik skor pozitif"
	case detail == "15m TV Al":
		return "15d teknik skor pozitif"
	default:
		return detail
	}
}

func positionText(value string) string {
	switch value {
	case "Ust", "ust", "Ustu", "ustu":
		return "Ust"
	case "Alt", "alt", "Alti", "alti":
		return "Alt"
	default:
		if value == "" {
			return "-"
		}
		return value
	}
}

func prettyApproval(value string) string {
	switch value {
	case "GUVENLI":
		return "Guvenli"
	case "RISKLI":
		return "Riskli"
	default:
		return value
	}
}

func writeVolumeLine(b *strings.Builder, value float64) {
	if value <= 0 {
		return
	}
	fmt.Fprintf(b, "Hacim: <code>x%.1f</code>\n", value)
}

func writeDualVolumeLine(b *strings.Builder, first float64, second float64) {
	if first <= 0 && second <= 0 {
		return
	}
	firstText := "-"
	secondText := "-"
	if first > 0 {
		firstText = fmt.Sprintf("x%.1f", first)
	}
	if second > 0 {
		secondText = fmt.Sprintf("x%.1f", second)
	}
	fmt.Fprintf(b, "Hacim: <code>1s %s</code> / <code>15d %s</code>\n", firstText, secondText)
}

func truncate(value string, max int) string {
	if len(value) <= max {
		return value
	}
	if max <= 3 {
		return value[:max]
	}
	return value[:max-3] + "..."
}
