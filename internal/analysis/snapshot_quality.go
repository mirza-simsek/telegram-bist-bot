package analysis

import (
	"fmt"

	"telegram-bist-bot/internal/market"
)

const (
	maxReasonableBISTPrice   = 1_000_000.0
	maxReasonableVolumeRatio = 100.0
	minLevelPriceRatio       = 0.05
	maxLevelPriceRatio       = 20.0
)

func snapshotPriceValue(snapshot market.Snapshot, key string) (float64, bool, string) {
	value, exists, ok := snapshotNumber(snapshot, key)
	if !exists {
		return 0, false, ""
	}
	if !ok || value <= 0 || value > maxReasonableBISTPrice {
		return 0, false, fmt.Sprintf("%s fiyat verisi gecersiz filtrelendi", key)
	}
	return value, true, ""
}

func snapshotRSIValue(snapshot market.Snapshot, key string) (float64, bool, string) {
	value, exists, ok := snapshotNumber(snapshot, key)
	if !exists {
		return 0, false, ""
	}
	if !ok || value < 0 || value > 100 {
		return 0, false, fmt.Sprintf("%s RSI verisi gecersiz filtrelendi", key)
	}
	return value, true, ""
}

func snapshotRecommendValue(snapshot market.Snapshot, key string) (float64, bool, string) {
	value, exists, ok := snapshotNumber(snapshot, key)
	if !exists {
		return 0, false, ""
	}
	if !ok || value < -1 || value > 1 {
		return 0, false, fmt.Sprintf("%s teknik skor verisi gecersiz filtrelendi", key)
	}
	return value, true, ""
}

func snapshotVolumeRatioValue(snapshot market.Snapshot, key string) (float64, bool, string) {
	value, exists, ok := snapshotNumber(snapshot, key)
	if !exists {
		return 0, false, ""
	}
	if !ok || value < 0 || value > maxReasonableVolumeRatio {
		return 0, false, fmt.Sprintf("%s hacim verisi gecersiz filtrelendi", key)
	}
	return value, true, ""
}

func snapshotLevelValue(snapshot market.Snapshot, key string, price float64) (float64, bool, string) {
	value, exists, ok := snapshotNumber(snapshot, key)
	if !exists {
		return 0, false, ""
	}
	if !ok || value <= 0 {
		return 0, false, fmt.Sprintf("%s seviye verisi gecersiz filtrelendi", key)
	}
	if price > 0 {
		ratio := value / price
		if ratio < minLevelPriceRatio || ratio > maxLevelPriceRatio {
			return 0, false, fmt.Sprintf("%s seviye verisi fiyatla uyumsuz filtrelendi", key)
		}
	}
	return value, true, ""
}

func snapshotNumber(snapshot market.Snapshot, key string) (float64, bool, bool) {
	value, exists := snapshot.Values[key]
	if !exists {
		return 0, false, false
	}
	if !finite(value) {
		return 0, true, false
	}
	return value, true, true
}

func appendWarning(warnings []string, warning string) []string {
	if warning == "" {
		return warnings
	}
	for _, existing := range warnings {
		if existing == warning {
			return warnings
		}
	}
	return append(warnings, warning)
}

func compactWarnings(warnings []string, limit int) []string {
	if len(warnings) == 0 {
		return nil
	}
	unique := make([]string, 0, len(warnings))
	for _, warning := range warnings {
		unique = appendWarning(unique, warning)
	}
	if limit > 0 && len(unique) > limit {
		return unique[:limit]
	}
	return unique
}
