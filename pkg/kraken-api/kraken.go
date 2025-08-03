package kraken_api

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"test-task1/models"
)

var (
	KrakenPairs   = make(map[string]string)
	initPairsOnce sync.Once
)

func InitKrakenPairs() {
	resp, err := http.Get("https://api.kraken.com/0/public/AssetPairs")
	if err != nil {
		fmt.Printf("kraken_api: failed to fetch asset pairs: %v\n", err)
		return
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		fmt.Printf("kraken_api: failed to read response: %v\n", err)
		return
	}

	var result struct {
		Error  []string                          `json:"error"`
		Result map[string]map[string]interface{} `json:"result"`
	}

	if err := json.Unmarshal(body, &result); err != nil {
		fmt.Printf("kraken_api: failed to parse JSON: %v\n", err)
		return
	}

	for pairID, data := range result.Result {
		if status, ok := data["status"].(string); !ok || status != "online" {
			continue
		}
		wsname, _ := data["wsname"].(string)

		if !strings.HasSuffix(wsname, "/USD") {
			continue
		}

		parts := strings.Split(wsname, "/")
		if len(parts) != 2 {
			continue
		}

		baseSymbol := parts[0]
		mappedSymbol := mapSpecialSymbols(baseSymbol)
		KrakenPairs[mappedSymbol] = pairID
	}
}

func mapSpecialSymbols(symbol string) string {
	specialCases := map[string]string{
		"XBT": "BTC",
		"XDG": "DOGE",
		"XXM": "MONERO",
	}

	if mapped, ok := specialCases[symbol]; ok {
		return mapped
	}
	return symbol
}

func GetPrice(coin string) (float64, error) {
	const op = "kraken.GetPrice"

	initPairsOnce.Do(InitKrakenPairs)

	pairID, ok := KrakenPairs[coin]
	if !ok {
		return 0, fmt.Errorf("%s: token doesn't exist: %s", op, coin)
	}

	url := fmt.Sprintf("https://api.kraken.com/0/public/Ticker?pair=%s", pairID)

	resp, err := http.Get(url)
	if err != nil {
		return 0, fmt.Errorf("%s: request error: %v", op, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, fmt.Errorf("%s: read error: %v", op, err)
	}

	var ticker models.KrakenTickerResponse
	if err := json.Unmarshal(body, &ticker); err != nil {
		return 0, fmt.Errorf("%s: json parse error: %v", op, err)
	}

	if len(ticker.Error) > 0 {
		return 0, fmt.Errorf("%s: API returned error: %v", op, ticker.Error)
	}

	pairData, ok := ticker.Result[pairID]
	if !ok {
		return 0, fmt.Errorf("%s: no data for pair %s", op, pairID)
	}

	if len(pairData.C) < 1 {
		return 0, fmt.Errorf("%s: no price data in response", op)
	}

	price, err := strconv.ParseFloat(pairData.C[0], 64)
	if err != nil {
		return 0, fmt.Errorf("%s: invalid price format: %v", op, err)
	}

	return price, nil
}
