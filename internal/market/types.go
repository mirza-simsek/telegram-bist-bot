package market

import (
	"math"
	"strings"
	"time"
)

type Candle struct {
	Time   time.Time
	Open   float64
	High   float64
	Low    float64
	Close  float64
	Volume float64
}

type BatchResult struct {
	Data   map[string][]Candle
	Errors map[string]error
}

func validCandle(c Candle) bool {
	values := []float64{c.Open, c.High, c.Low, c.Close, c.Volume}
	for _, value := range values {
		if math.IsNaN(value) || math.IsInf(value, 0) {
			return false
		}
	}
	return c.High > 0 && c.Low > 0 && c.Close > 0 && c.Volume >= 0
}

func normalizeRawSymbol(symbol string) string {
	symbol = strings.ToUpper(strings.TrimSpace(symbol))
	if colon := strings.Index(symbol, ":"); colon >= 0 {
		symbol = symbol[colon+1:]
	}
	if dot := strings.Index(symbol, "."); dot >= 0 {
		symbol = symbol[:dot]
	}
	return symbol
}
