package kraken_api

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"strconv"
)

func GetPrice(coin string) (float64, error) {
	const op = "kraken.GetPrice"
	pair := coin + "USD"
	url := fmt.Sprintf("https://api.kraken.com/0/public/Ticker?pair=%s", pair)
	resp, err := http.Get(url)
	if err != nil {
		return 0, fmt.Errorf("%s: %v", op, err)
	}
	defer resp.Body.Close()

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return 0, fmt.Errorf("%s: %v", op, err)
	}

	var data map[string]interface{}
	if err := json.Unmarshal(body, &data); err != nil {
		return 0, fmt.Errorf("%s: %v", op, err)
	}

	result := data["result"].(map[string]interface{})
	ticker := result[pair].(map[string]interface{})
	priceStr := ticker["c"].([]interface{})[0].(string)
	return strconv.ParseFloat(priceStr, 64)
}
