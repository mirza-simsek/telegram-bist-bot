package analysis

import (
	"bufio"
	"fmt"
	"os"
	"regexp"
	"strings"
)

var symbolPattern = regexp.MustCompile(`^[A-Z0-9]{2,12}(\.[A-Z]{2,4})?$`)

func LoadSymbols(path string) ([]string, []string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, nil, fmt.Errorf("open symbols file %q: %w", path, err)
	}
	defer file.Close()

	seen := make(map[string]struct{})
	var symbols []string
	var invalid []string

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		symbol := strings.ToUpper(strings.TrimSpace(scanner.Text()))
		if symbol == "" || strings.HasPrefix(symbol, "#") {
			continue
		}
		if !symbolPattern.MatchString(symbol) {
			invalid = append(invalid, symbol)
			continue
		}
		if _, ok := seen[symbol]; ok {
			continue
		}
		seen[symbol] = struct{}{}
		symbols = append(symbols, symbol)
	}
	if err := scanner.Err(); err != nil {
		return nil, nil, err
	}
	if len(symbols) == 0 {
		return nil, invalid, fmt.Errorf("no valid symbols in %q", path)
	}
	return symbols, invalid, nil
}
