package app

import (
	"fmt"
	"html"
	"strings"

	"telegram-bist-bot/internal/analysis"
)

func (a *App) defaultUniverse() analysis.Universe {
	return a.universeOrFallback(a.cfg.DefaultUniverse)
}

func (a *App) scheduledUniverse() analysis.Universe {
	return a.universeOrFallback(a.cfg.ScheduledUniverse)
}

func (a *App) universeOrFallback(raw string) analysis.Universe {
	key := normalizeUniverseKey(raw)
	return a.universeByKey(key)
}

func (a *App) universeByKey(key string) analysis.Universe {
	key = normalizeUniverseKey(key)
	if universe, ok := a.universes[key]; ok {
		return universe
	}
	if universe, ok := a.universes["bist30"]; ok {
		return universe
	}
	if len(a.universeOrder) > 0 {
		return a.universes[a.universeOrder[0]]
	}
	return analysis.Universe{Key: "bist30", Label: "BIST 30"}
}

func normalizeUniverseKey(raw string) string {
	value := strings.ToLower(strings.TrimSpace(raw))
	value = strings.TrimPrefix(value, "/")
	value = strings.ReplaceAll(value, "_", "")
	value = strings.ReplaceAll(value, "-", "")
	value = strings.ReplaceAll(value, " ", "")
	switch value {
	case "", "30", "bist30", "xu030", "tum", "bisttum", "100", "bist100", "xu100":
		return "bist30"
	default:
		return value
	}
}

func normalizeCommandName(raw string) string {
	value := strings.TrimSpace(raw)
	if at := strings.Index(value, "@"); at >= 0 {
		value = value[:at]
	}
	value = strings.TrimPrefix(value, "/")
	return strings.ToLower(strings.TrimSpace(value))
}

func (a *App) symbolFromCommand(command string) (string, bool) {
	symbol := normalizeCommandSymbol(normalizeCommandName(command))
	if symbol == "" {
		return "", false
	}
	if _, ok := a.knownSymbols[symbol]; !ok {
		return "", false
	}
	return symbol, true
}

func normalizeCommandSymbol(raw string) string {
	symbol := strings.ToUpper(strings.TrimSpace(raw))
	if dot := strings.Index(symbol, "."); dot >= 0 {
		symbol = symbol[:dot]
	}
	if colon := strings.Index(symbol, ":"); colon >= 0 {
		symbol = symbol[colon+1:]
	}
	if len(symbol) < 3 || len(symbol) > 8 {
		return ""
	}
	for _, r := range symbol {
		if (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			continue
		}
		return ""
	}
	return symbol
}

func (a *App) formatHelp(chatID int64) string {
	return fmt.Sprintf(`<b>BIST 30 Teknik Tarama Botu</b>
Chat ID: <code>%d</code>

<b>Tarama komutlari</b>
<code>gunici</code> - BIST 30 gun ici TL taramasi (5dk + 15dk)
<code>gunluk</code> - BIST 30 gunluk TL ve USD radar (puanlar ayri)

<b>Hisse karti</b>
<code>THYAO</code> - Gun ici TL ve gunluk TL/USD hisse karti

<b>Yonetim</b>
<code>durum</code> - Son tarama durumu
<code>reset</code> - Aktif islemi durdur
<code>ayarlar</code> - Aktif esikler ve zamanlama
<code>help</code> - Bu yardim

Kapsam: %s

Bu bot teknik sinyal taramasi yapar; yatirim tavsiyesi degildir.`,
		chatID,
		a.formatUniverseList(),
	)
}

func (a *App) formatUsageError(err error) string {
	return fmt.Sprintf(`<b>Komut anlasilamadi</b>
Sebep: <code>%s</code>

Kullanim:
<code>gunici</code>
<code>gunluk</code>`, html.EscapeString(err.Error()))
}

func (a *App) formatUniverseList() string {
	if len(a.universeOrder) == 0 {
		return "<code>yok</code>"
	}
	parts := make([]string, 0, len(a.universeOrder))
	for _, key := range a.universeOrder {
		universe := a.universes[key]
		parts = append(parts, fmt.Sprintf("<code>%s: %d</code>", html.EscapeString(universe.Label), len(universe.Symbols)))
	}
	return strings.Join(parts, ", ")
}
