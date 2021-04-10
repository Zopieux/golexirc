package main

import (
	"encoding/json"
	"errors"
	"fmt"
)

var emptyMap = map[string]string{}

type inbound struct {
	Type int              `json:"MessageType"`
	Data *json.RawMessage `json:"Data"`
}

type outbound struct {
	Type int         `json:"MessageType"`
	Data interface{} `json:"Value"`
}

type matchMakingEvent struct {
	QueueSize   int `json:"QueueSize"`
	Players     int `json:"Players"`
	ActiveGames int `json:"Turns"`
}

type gameCloseEvent struct{}

type giveUpEvent struct{}

type typingEvent struct {
	Length int `json:"CurrentTyping"`
}

type hintEvent struct {
	Hint string `json:"Prop"`
}

type guessEvent struct {
	Guess string `json:"Guess"`
}

type sentimentEvent struct {
	Sentiment int `json:"Sentiment"`
}

func (s *sentimentEvent) name() string {
	switch s.Sentiment {
	case 1:
		return "positive"
	case 2:
		return "skip"
	case 3:
		return "negative"
	default:
		return ""
	}
}

type keepPlayingEvent struct {
	KeepPlaying bool `json:"Replay"`
}

type gameEndEvent struct {
	Partner        string   `json:"Partner"`
	PartnerRank    int      `json:"PartnerRank"`
	IsWin          bool     `json:"IsWin"`
	Word           string   `json:"Word"`
	Link           string   `json:"Link"`
	BrikBonus      int      `json:"BrikBonus"`
	TimeBonus      int      `json:"TimeBonus"`
	PlayerScore    int      `json:"PlayerScore"`
	RoundDuration  float64  `json:"RoundDuration"`
	WordBricks     float64  `json:"WordBricks"`
	WordTimes      float64  `json:"WordTimes"`
	WordDifficulty int      `json:"WordDiff"`
	WordCluster    int      `json:"WordCluster"`
	Stars          []string `json:"Stars"`
}

type refusedPropositionEvent struct {
	Reason int `json:"Frb_cause"`
}

func (r *refusedPropositionEvent) name() string {
	switch r.Reason {
	case 1:
		return "le début est trop similaire"
	case 2:
		return "l'orthographe est invalide"
	case 3:
		return "l'orthographe est trop similaire"
	case 4:
		return "les caractères spéciaux sont interdits"
	case 5:
		return "le mot est trop long"
	case 6:
		return "la fin est trop similaire"
	default:
		return ""
	}
}

type gameUpdateEvent struct {
	Word        string  `json:"Word"`
	Round       int     `json:"RoundNumber"`
	TimeLeft    float64 `json:"TimeLeft"`
	WordCluster int     `json:"WordCluster"`
	CanGiveUp   bool    `json:"CanGiveUp"`
}

type newGameEvent struct {
	gameUpdateEvent
	IsGuesser bool `json:"IsGuesser"`
}

// Internal.
type newGameSearchEvent struct{}

func makeProposition(word string) *outbound {
	return &outbound{Type: 100, Data: word}
}

func makeKeepPlaying(keepPlaying bool) *outbound {
	return &outbound{Type: 5, Data: keepPlaying}
}

func makeTyping(word string) *outbound {
	return &outbound{Type: 1, Data: word}
}

func makeGiveUp() *outbound {
	return &outbound{Type: 12, Data: emptyMap}
}

func makeSentiment(s int) *outbound {
	return &outbound{Type: 9, Data: s}
}

func makeCancel() *outbound {
	return &outbound{Type: 13, Data: emptyMap}
}

func parseInbound(in inbound) (interface{}, error) {
	var i interface{}
	switch in.Type {
	case 0:
		i = &gameCloseEvent{}
	case 1:
		i = &typingEvent{}
	case 2:
		i = &hintEvent{}
	case 3:
		i = &guessEvent{}
	case 4:
		i = &gameEndEvent{}
	case 5:
		i = &keepPlayingEvent{}
	case 6:
		i = &refusedPropositionEvent{}
	case 7:
		i = &newGameEvent{}
	case 9:
		i = &sentimentEvent{}
	case 11:
		i = &gameUpdateEvent{}
	case 12:
		i = &giveUpEvent{}
	case 17:
		i = &matchMakingEvent{}
	default:
		return nil, errors.New(fmt.Sprintf("unknown type %d", in.Type))
	}
	if err := json.Unmarshal(*in.Data, i); err != nil {
		return nil, err
	}
	//log.Printf("inbound %d: %+v", in.Type, i)
	return i, nil
}

