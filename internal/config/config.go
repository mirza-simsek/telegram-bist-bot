package config

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	TelegramToken      string
	DefaultChatID      int64
	AllowedChatIDs     map[int64]struct{}
	MarketTimezone     *time.Location
	MarketTimezoneRaw  string
	SymbolsFile        string
	AllSymbolsFile     string
	BIST100SymbolsFile string
	DefaultUniverse    string
	ScheduledUniverse  string

	TradingViewChartConcurrency int
	PythonScannerEnabled        bool
	PythonExecutable            string
	PythonScannerScript         string
	PythonScannerBatchSize      int
	PythonScannerWorkers        int
	PythonScannerYFThreads      bool
	HTTPTimeout                 time.Duration
	ScanTimeout                 time.Duration
	MaxResults                  int
	DailyMinScore               int
	DailyAlertMinScore          int
	IntradayMinScore            int
	IntradayAlertScore          int
	AlertDedupWindow            time.Duration
	RunScanOnStartup            bool
	IntradayAutoEnabled         bool
	DailyAutoEnabled            bool
	IntradayMinute              int
	IntradayStartHour           int
	IntradayEndHour             int
	DailyHour                   int
	DailyMinute                 int
}

func Load() (Config, error) {
	_ = loadDotEnv(getenv("ENV_FILE", ".env"))

	locName := getenv("MARKET_TIMEZONE", "Europe/Istanbul")
	loc, err := time.LoadLocation(locName)
	if err != nil {
		return Config{}, fmt.Errorf("load timezone %q: %w", locName, err)
	}

	token := strings.TrimSpace(os.Getenv("TELEGRAM_BOT_TOKEN"))
	if token == "" {
		return Config{}, errors.New("TELEGRAM_BOT_TOKEN is required")
	}

	defaultChatID, err := int64Env("TELEGRAM_CHAT_ID", 0)
	if err != nil {
		return Config{}, err
	}

	allowed, err := chatIDSet(os.Getenv("TELEGRAM_ALLOWED_CHAT_IDS"))
	if err != nil {
		return Config{}, err
	}
	if len(allowed) == 0 && defaultChatID != 0 {
		allowed[defaultChatID] = struct{}{}
	}

	allSymbolsFile := getenv("ALL_SYMBOLS_FILE", getenv("SYMBOLS_FILE", filepath.FromSlash("data/bist_tum_hisseler.txt")))
	cfg := Config{
		TelegramToken:      token,
		DefaultChatID:      defaultChatID,
		AllowedChatIDs:     allowed,
		MarketTimezone:     loc,
		MarketTimezoneRaw:  locName,
		SymbolsFile:        allSymbolsFile,
		AllSymbolsFile:     allSymbolsFile,
		BIST100SymbolsFile: getenv("BIST100_SYMBOLS_FILE", filepath.FromSlash("data/bist_100_hisseler.txt")),
		DefaultUniverse:    strings.ToLower(getenv("DEFAULT_UNIVERSE", "tum")),
		ScheduledUniverse:  strings.ToLower(getenv("SCHEDULED_UNIVERSE", "tum")),

		TradingViewChartConcurrency: mustIntEnv("TRADINGVIEW_CHART_CONCURRENCY", 4),
		PythonScannerEnabled:        boolEnv("PYTHON_SCANNER_ENABLED", true),
		PythonExecutable:            getenv("PYTHON_EXECUTABLE", "python3"),
		PythonScannerScript:         getenv("PYTHON_SCANNER_SCRIPT", filepath.FromSlash("scripts/bist_data_scrap_bridge.py")),
		PythonScannerBatchSize:      mustIntEnv("PYTHON_SCANNER_BATCH_SIZE", 50),
		PythonScannerWorkers:        mustIntEnv("PYTHON_SCANNER_WORKERS", 4),
		PythonScannerYFThreads:      boolEnv("PYTHON_SCANNER_YF_THREADS", false),
		HTTPTimeout:                 time.Duration(mustIntEnv("HTTP_TIMEOUT_SECONDS", 8)) * time.Second,
		ScanTimeout:                 time.Duration(mustIntEnv("SCAN_TIMEOUT_MINUTES", 5)) * time.Minute,
		MaxResults:                  mustIntEnv("MAX_RESULTS", 10),
		DailyMinScore:               mustIntEnv("GUNLUK_MIN_SCORE", 3),
		DailyAlertMinScore:          mustIntEnv("GUNLUK_ALERT_MIN_SCORE", 3),
		IntradayMinScore:            mustIntEnv("GUNICI_MIN_SCORE", 5),
		IntradayAlertScore:          mustIntEnv("GUNICI_ALERT_MIN_SCORE", 7),
		AlertDedupWindow:            time.Duration(mustIntEnv("ALERT_DEDUP_HOURS", 6)) * time.Hour,
		RunScanOnStartup:            boolEnv("RUN_SCAN_ON_STARTUP", false),
		IntradayAutoEnabled:         boolEnv("INTRADAY_AUTO_ENABLED", true),
		DailyAutoEnabled:            boolEnv("DAILY_AUTO_ENABLED", true),
		IntradayMinute:              mustIntEnv("INTRADAY_SCAN_MINUTE", 15),
		IntradayStartHour:           mustIntEnv("INTRADAY_START_HOUR", 10),
		IntradayEndHour:             mustIntEnv("INTRADAY_END_HOUR", 17),
		DailyHour:                   mustIntEnv("DAILY_SCAN_HOUR", 18),
		DailyMinute:                 mustIntEnv("DAILY_SCAN_MINUTE", 20),
	}

	if err := cfg.validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func (c Config) IsChatAllowed(chatID int64) bool {
	if len(c.AllowedChatIDs) == 0 {
		return true
	}
	_, ok := c.AllowedChatIDs[chatID]
	return ok
}

func (c Config) validate() error {
	if c.TradingViewChartConcurrency < 1 {
		return errors.New("TRADINGVIEW_CHART_CONCURRENCY must be greater than 0")
	}
	if c.PythonScannerEnabled {
		if strings.TrimSpace(c.PythonExecutable) == "" {
			return errors.New("PYTHON_EXECUTABLE is required when PYTHON_SCANNER_ENABLED=true")
		}
		if strings.TrimSpace(c.PythonScannerScript) == "" {
			return errors.New("PYTHON_SCANNER_SCRIPT is required when PYTHON_SCANNER_ENABLED=true")
		}
		if c.PythonScannerBatchSize < 1 {
			return errors.New("PYTHON_SCANNER_BATCH_SIZE must be greater than 0")
		}
		if c.PythonScannerWorkers < 1 {
			return errors.New("PYTHON_SCANNER_WORKERS must be greater than 0")
		}
	}
	if c.MaxResults < 1 {
		return errors.New("MAX_RESULTS must be greater than 0")
	}
	if c.IntradayMinute < 0 || c.IntradayMinute > 59 || c.DailyMinute < 0 || c.DailyMinute > 59 {
		return errors.New("schedule minutes must be between 0 and 59")
	}
	if c.IntradayStartHour < 0 || c.IntradayStartHour > 23 || c.IntradayEndHour < 0 || c.IntradayEndHour > 23 || c.DailyHour < 0 || c.DailyHour > 23 {
		return errors.New("schedule hours must be between 0 and 23")
	}
	if c.IntradayEndHour < c.IntradayStartHour {
		return errors.New("INTRADAY_END_HOUR must be greater than or equal to INTRADAY_START_HOUR")
	}
	return nil
}

func loadDotEnv(path string) error {
	if strings.TrimSpace(path) == "" {
		return nil
	}
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		value = strings.Trim(value, `"'`)
		if key == "" {
			continue
		}
		if _, exists := os.LookupEnv(key); !exists {
			_ = os.Setenv(key, value)
		}
	}
	return scanner.Err()
}

func getenv(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func mustIntEnv(key string, fallback int) int {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	n, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return n
}

func int64Env(key string, fallback int64) (int64, error) {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback, nil
	}
	n, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("%s must be an integer: %w", key, err)
	}
	return n, nil
}

func boolEnv(key string, fallback bool) bool {
	value := strings.ToLower(strings.TrimSpace(os.Getenv(key)))
	if value == "" {
		return fallback
	}
	switch value {
	case "1", "true", "yes", "y", "on":
		return true
	case "0", "false", "no", "n", "off":
		return false
	default:
		return fallback
	}
}

func chatIDSet(raw string) (map[int64]struct{}, error) {
	result := make(map[int64]struct{})
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		id, err := strconv.ParseInt(part, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("TELEGRAM_ALLOWED_CHAT_IDS has invalid chat id %q: %w", part, err)
		}
		result[id] = struct{}{}
	}
	return result, nil
}
