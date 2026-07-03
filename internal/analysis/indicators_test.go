package analysis

import (
	"testing"
	"time"

	"telegram-bist-bot/internal/market"
)

func TestTRPBuySignalRequiresLastNineClosesBelowFourBarsAgo(t *testing.T) {
	closes := []float64{20, 21, 22, 23, 19, 18, 17, 16, 15, 14, 13, 12, 11, 10}
	if !trpBuySignal(closes) {
		t.Fatal("expected TRP signal")
	}

	closes[len(closes)-1] = 30
	if trpBuySignal(closes) {
		t.Fatal("expected TRP signal to fail when the last close is not below four bars ago")
	}
}

func TestOBVBreakoutDetectsCrossAboveSMA(t *testing.T) {
	candles := make([]market.Candle, 22)
	price := 100.0
	for i := range candles {
		volume := 100.0
		if i == len(candles)-1 {
			price += 5
			volume = 5000
		}
		candles[i] = market.Candle{
			Time:   time.Unix(int64(i*3600), 0),
			Open:   price,
			High:   price + 1,
			Low:    price - 1,
			Close:  price,
			Volume: volume,
		}
	}

	if !obvBreakout(candles) {
		t.Fatal("expected OBV breakout")
	}
}

func TestVolumeProfileReturnsPOCAndVAH(t *testing.T) {
	candles := []market.Candle{
		{Low: 9, High: 11, Close: 10, Volume: 100},
		{Low: 9, High: 11, Close: 10, Volume: 100},
		{Low: 19, High: 21, Close: 20, Volume: 10},
	}

	poc, vah := volumeProfile(candles, 3, 2, 0.70)
	if poc < 9 || poc > 15 {
		t.Fatalf("expected POC in lower volume area, got %.2f", poc)
	}
	if vah <= poc {
		t.Fatalf("expected VAH above POC, got poc=%.2f vah=%.2f", poc, vah)
	}
}
