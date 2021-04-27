package main

import (
	"encoding/json"
	"io/ioutil"
	"net/http"
)

type Stats struct {
	Login        string `json:"Login"`
	Rounds       int64  `json:"Rounds"`
	RoundsWon    int64  `json:"RoundsWon"`
	Score        int64  `json:"EloM"`
	GuessScore   int64  `json:"EloG"`
	ProposeScore int64  `json:"EloP"`
	Rank         int64  `json:"Rank"`
	GivesUp      int64  `json:"GivesUp"`
	Stars        struct {
		Blue       int64 `json:"BlueStar"`
		Copper     int64 `json:"CooperStar"` // yes, cooper.
		Gold       int64 `json:"GoldStar"`
		Purple     int64 `json:"PurpleStar"`
		SilverStar int64 `json:"SilverStar"`
	}
}

func GetStats(client *http.Client) (*Stats, error) {
	page, err := client.Get("http://lexiflaire.com/stats")
	if err != nil {
		return nil, err
	}
	var stats Stats
	data, err := ioutil.ReadAll(page.Body)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(data, &stats); err != nil {
		return nil, err
	}
	return &stats, nil
}
