package main

import (
	"crypto/tls"
	"errors"
	"flag"
	"fmt"
	irc "github.com/fluffle/goirc/client"
	"github.com/gorilla/websocket"
	"log"
	"math"
	"math/rand"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

var (
	sessionCookie = flag.String("key", "", "`key` cookie")
	ircHost       = flag.String("server", "chat.freenode.net", "IRC server hostname")
	ircPort       = flag.Uint("port", 6697, "IRC server port")
	ircNick       = flag.String("nick", "lexiflaire", "IRC nick")
	ircChan       = flag.String("chan", "#lexiflaire", "IRC channel")
	botRoots      = flag.String("roots", "zopieux", "Bot roots")
	botAdmins     = flag.String("admins", "", "Bot admins")

	chars = regexp.MustCompile("^[a-zA-Zàâäéèêëïîöôûüùÿŷœæ]+\\s*$")

	client *http.Client
	ws     *websocket.Dialer
)

func getKey() (string, error) {
	resp, err := client.Get("http://lexiflaire.com/core")
	if err != nil {
		return "", err
	}
	for _, cookie := range resp.Cookies() {
		if cookie.Name == "key" {
			return cookie.Value, nil
		}
	}
	return "", errors.New("no key cookie")
}

type game struct {
	c           *websocket.Conn
	ins         chan interface{}
	outs        chan *outbound
	done        chan struct{}
	stop        chan struct{}
	consumeNext map[int]bool
	mut         *sync.Mutex
}

func (g *game) addConsumeNext(messType int) {
	g.mut.Lock()
	g.consumeNext[messType] = true
	g.mut.Unlock()
}

func (g *game) shouldConsumeNext(messType int) (ret bool) {
	g.mut.Lock()
	ret = g.consumeNext[messType]
	delete(g.consumeNext, messType)
	g.mut.Unlock()
	return
}

func newGame() (*game, error) {
	key, err := getKey()
	if err != nil {
		return nil, err
	}
	h := http.Header{}
	h.Add("User-Agent", "User-Agent: Mozilla/5.0 (X11; Linux x86_64; rv:87.0) Gecko/20100101 Firefox/87.0")
	h.Add("Pragma", "no-cache")
	conn, _, err := ws.Dial("ws://lexiflaire.com/wsupdate/"+key, h)
	if err != nil {
		return nil, err
	}

	g := &game{
		c:           conn,
		ins:         make(chan interface{}),
		outs:        make(chan *outbound),
		done:        make(chan struct{}),
		stop:        make(chan struct{}),
		consumeNext: map[int]bool{},
		mut:         &sync.Mutex{},
	}

	wsChan := make(chan interface{})

	go func() {
		defer close(wsChan)
		var inRaw inbound
		//currentRound := 0
		for {
			if err := g.c.ReadJSON(&inRaw); err != nil {
				return
			}
			inFace, err := parseInbound(inRaw)
			if err != nil {
				log.Println("parseInbound:", err)
				return
			}
			if g.shouldConsumeNext(inRaw.Type) {
				log.Printf("Filtered event of type %d", inRaw.Type)
			} else {
				wsChan <- inFace
			}
		}
	}()

	go func() {
		defer func() {
			close(g.done)
			_ = g.c.Close()
			//close(g.ins)
			//close(g.outs)
			//close(g.stop)
		}()
		currentRound := 0
		for {
			select {
			case <-g.stop:
				return
			case out, ok := <-g.outs:
				if !ok {
					return
				}
				if err := g.c.WriteJSON(out); err != nil {
					log.Println("Error writing JSON:", err)
				} else {
					log.Printf("Wrote json: %+v", out)
				}
			case ev, ok := <-wsChan:
				if !ok {
					log.Println("Stop after game event chan closed")
					return
				}
				log.Printf("inbound: %+v", ev)
				switch in := ev.(type) {
				case *gameCloseEvent:
					_ = g.c.WriteJSON(makeKeepPlaying(false))
					log.Println("Stop after game closed")
					return
				case *newGameEvent:
					currentRound = in.Round
					g.ins <- ev
				case *gameEndEvent:
					g.ins <- ev
					if currentRound >= 4 {
						_ = g.c.WriteJSON(makeKeepPlaying(false))
						log.Println("Stop after 4 rounds")
						return
					}
				case *giveUpEvent:
					g.ins <- ev
					log.Println("Stop after receiving give up")
					return
				case *keepPlayingEvent:
					g.ins <- ev
					if !in.KeepPlaying {
						log.Println("Stop after receiving negative keep-playing")
						return
					}
				default:
					g.ins <- ev
				}
			}
		}
	}()

	return g, nil
}

func gameOnce(g *game, ins chan<- interface{}, outs <-chan *outbound, stop <-chan bool, abort <-chan struct{}) (keepGoing bool) {
	sentKeepPlaying := false
	keepPlaying := true
	for {
		select {
		// Forward game 'ins', aka events.
		case inFace, ok := <-g.ins:
			if ok {
				switch in := inFace.(type) {
				case *newGameEvent:
					sentKeepPlaying = false
				case *keepPlayingEvent:
					if !in.KeepPlaying {
						return true
					} else if !sentKeepPlaying {
						sentKeepPlaying = true
						if keepPlaying {
							g.addConsumeNext(5)
						}
						g.outs <- makeKeepPlaying(keepPlaying)
						if !keepPlaying {
							g.stop <- struct{}{}
							return false
						}
					}
				case *gameUpdateEvent:
					if in.CanGiveUp && in.TimeLeft < 20 {
						g.outs <- makeGiveUp()
					}
				}
				ins <- inFace
			}

		// Forward application 'outs', aka actions.
		case out, ok := <-outs:
			if ok {
				// Sentiment and keepPlaying messages are immediately echoed back to us.
				// Filter them out.
				if out.Type == 1 || out.Type == 9 {
					g.addConsumeNext(out.Type)
				}
				g.outs <- out
			}

		case <-g.done:
			log.Println("game is done")
			return true

		case <-abort:
			g.stop <- struct{}{}
			log.Println("game is aborted")
			return true

		case hardStop, ok := <-stop:
			if ok && hardStop {
				g.stop <- struct{}{}
				return false
			} else if ok && !hardStop {
				keepPlaying = false
			}
		}
	}
}

func gameForever(ins chan<- interface{}, outs <-chan *outbound, stop <-chan bool, abort <-chan struct{}, errs chan<- error) {
	defer log.Println("end of gameForever")
	for {
		g, err := newGame()
		if err != nil {
			errs <- err
			return
		}
		ins <- &newGameSearchEvent{}
		if gameOnce(g, ins, outs, stop, abort) {
			time.Sleep(time.Second * 3)
		} else {
			errs <- errors.New("stop request")
			return
		}
	}
}

func main() {
	flag.Parse()
	roots := map[string]bool{}
	admins := map[string]bool{}
	for _, x := range strings.Split(*botRoots, ",") {
		roots[x] = true
	}
	for _, x := range strings.Split(*botAdmins, ",") {
		admins[x] = true
	}
	jar, err := cookiejar.New(nil)
	if err != nil {
		log.Fatal("cookiejar:", err)
	}
	if *sessionCookie != "" {
		u, err := url.Parse("http://lexiflaire.com")
		if err != nil {
			log.Fatal("url.Parse:", err)
		}
		jar.SetCookies(u, []*http.Cookie{{Name: "key", Value: *sessionCookie}})
	}
	client = &http.Client{Jar: jar}
	ws = &websocket.Dialer{Jar: client.Jar}

	quit := make(chan struct{})

	ins := make(chan interface{})
	outs := make(chan *outbound)
	errs := make(chan error)
	prop := make(chan string)
	stop := make(chan bool)
	abort := make(chan struct{})

	playing := false
	var currentRound = &newGameEvent{}
	lastTypeAnnounce := time.Now().Add(-time.Hour)
	lastQueueAnnounce := time.Now().Add(-time.Hour)
	lastSentimentAnnounce := time.Now().Add(-time.Hour)
	var lastTyping *time.Time = nil
	canProposeNow := false
	isMyTurn := false
	var announceTimeUnder float64
	var typeTimings []int64
	var totalScore int

	cfg := irc.NewConfig(*ircNick)
	cfg.SSL = true
	cfg.SSLConfig = &tls.Config{ServerName: *ircHost}
	cfg.Server = fmt.Sprintf("%s:%d", *ircHost, *ircPort)
	cfg.NewNick = func(s string) string { return s + "_" }
	bot := irc.Client(cfg)
	say := func(s string) {
		bot.Privmsg(*ircChan, s)
	}
	bot.HandleFunc(irc.CONNECTED, func(conn *irc.Conn, line *irc.Line) {
		log.Println("Connected to IRC, joining")
		conn.Join(*ircChan)
	})
	bot.HandleFunc(irc.JOIN, func(conn *irc.Conn, line *irc.Line) {
		if line.Target() == *ircChan && line.Nick == *ircNick {
			say("Coucou c'est moi.")
		}
	})
	bot.HandleFunc(irc.DISCONNECTED, func(conn *irc.Conn, line *irc.Line) {
		log.Println("Disconnected from IRC, reconnecting")
		time.Sleep(time.Second * 2)
		if err := bot.Connect(); err != nil {
			log.Printf("error connecting: %q", err)
			quit <- struct{}{}
		}
	})
	bot.HandleFunc(irc.PRIVMSG, func(conn *irc.Conn, line *irc.Line) {
		if !line.Public() || line.Target() != *ircChan {
			return
		}
		isSuperAdmin := roots[line.Nick] == true
		isAdmin := isSuperAdmin || admins[line.Nick] == true
		if line.Text() == "!start" && !playing && isAdmin {
			playing = true
			go gameForever(ins, outs, stop, abort, errs)
		} else if line.Text() == "!stop" && playing && isAdmin {
			say(grey("OK on arrête après cette manche."))
			stop <- false
		} else if line.Text() == "!hardstop" && playing && isSuperAdmin {
			stop <- true
		} else if line.Text() == "!quit" && isSuperAdmin {
			quit <- struct{}{}
		} else if line.Text() == "!nice" && isAdmin {
			outs <- makeSentiment(1)
		} else if line.Text() == "!main" && isAdmin {
			outs <- makeSentiment(2)
		} else if line.Text() == "!ffs" && isAdmin {
			outs <- makeSentiment(3)
		} else if line.Text() == "!score" && isAdmin {
			go func() {
				if stat, err := GetStats(client); err == nil {
					var rounds = stat.Rounds
					if rounds == 0 {
						rounds = 1
					}
					say(fmt.Sprintf("%d parties jouées, dont %s victoires (%.1f%%) et %d abandons. Score : %s, position %d dans le classement.",
						stat.Rounds,
						green(strconv.FormatInt(stat.RoundsWon, 10)),
						100.*float64(stat.RoundsWon)/float64(rounds),
						stat.GivesUp,
						green(emph(strconv.FormatInt(stat.Score, 10))),
						stat.Rank))
				} else {
					log.Printf("GetStats error: %q", err)
				}
			}()
		} else if canProposeNow && chars.MatchString(line.Text()) {
			canProposeNow = false
			prop <- line.Text()
		}
	})
	defer bot.Close()
	defer close(stop)

	if err := bot.Connect(); err != nil {
		log.Printf("error connecting: %q", err)
		quit <- struct{}{}
	}

	checkBot := func() (isBot bool) {
		mean, sd := medianTiming(typeTimings)
		log.Printf("Timings (%d): %1.f ± %2.f", len(typeTimings), mean, sd)
		isBot = 198 < mean && mean < 202 && sd <= 2
		typeTimings = nil
		lastTyping = nil
		if isBot {
			say(fmt.Sprintf("Oh non, c'est le %s :( Abandon.",
				red("bot HAL9000")))
			abort <- struct{}{}
		}
		return
	}

	for {
		select {
		case err := <-errs:
			say("Avorté ! " + err.Error())
			playing = false
		case inFace := <-ins:
			switch ev := inFace.(type) {
			case *newGameSearchEvent:
				lastQueueAnnounce = time.Now()
				say("Recherche d'une partie, !stop pour arrêter.")

			case *matchMakingEvent:
				now := time.Now()
				if now.Sub(lastQueueAnnounce).Seconds() > 6 {
					lastQueueAnnounce = now
					qs := ev.QueueSize - 1
					if qs < 0 {
						qs = 0
					}
					say(grey(fmt.Sprintf("Toujours dans la file. Il y a %d joueurs et %d parties avant la notre.", ev.Players, qs)))
				}

			case *newGameEvent:
				currentRound = &newGameEvent{}
				*currentRound = *ev
				lastTypeAnnounce = time.Now().Add(-time.Hour)
				lastSentimentAnnounce = time.Now().Add(-time.Hour)
				announceTimeUnder = 20.
				lastTyping = nil
				typeTimings = nil
				if currentRound.IsGuesser {
					isMyTurn = false
					canProposeNow = false
					say("Vous allez deviner…")
				} else {
					isMyTurn = true
					canProposeNow = true
					say(fmt.Sprintf("Faites deviner le mot %s", yellow(emph(currentRound.Word))))
				}

			case *gameEndEvent:
				currentRound = nil
				var outcome string
				if ev.IsWin {
					outcome = green("gagné")
				} else {
					outcome = red("perdu")
				}
				partner := ""
				if ev.Partner != "" && ev.PartnerRank != 0 {
					partner = fmt.Sprintf(" Vous avez joué avec %s, rang %d.", ev.Partner, ev.PartnerRank)
				}
				totalScore += ev.PlayerScore
				say(fmt.Sprintf("C'est %s, le mot était %s, %d (cumulé : %d)%s", emph(outcome), emph(ev.Word), ev.PlayerScore, totalScore, partner))

			case *gameUpdateEvent:
				if ev.TimeLeft > 0 && ev.TimeLeft <= announceTimeUnder {
					announceTimeUnder /= 2
					say(grey(fmt.Sprintf("Il reste %.0f secondes", ev.TimeLeft)))
				}

			case *giveUpEvent:
				say("Abandon de la partie.")

			case *guessEvent:
				if checkBot() {
					continue
				}
				if currentRound != nil && !currentRound.IsGuesser {
					isMyTurn = true
					canProposeNow = true
					say(fmt.Sprintf("Le partenaire suppose : %s", blue(emph(ev.Guess))))
				} else if currentRound != nil && currentRound.IsGuesser {
					say(grey(fmt.Sprintf("Supposition envoyée : %s", emph(ev.Guess))))
				}

			case *hintEvent:
				if checkBot() {
					continue
				}
				if currentRound != nil && currentRound.IsGuesser {
					isMyTurn = true
					canProposeNow = true
					say(fmt.Sprintf("Le partenaire indique : %s", yellow(emph(ev.Hint))))
				} else if currentRound != nil && !currentRound.IsGuesser {
					say(grey(fmt.Sprintf("Indice envoyé : %s", emph(ev.Hint))))
				}

			case *refusedPropositionEvent:
				if currentRound != nil {
					typeTimings = nil
					lastTyping = nil
					if isMyTurn {
						canProposeNow = true
						say(fmt.Sprintf("Prosposition invalide : %s. Réessayez !", red(ev.name())))
					} else {
						say(grey(fmt.Sprintf("Proposition invalide du partenaire : %s", ev.name())))
					}
				}

			case *sentimentEvent:
				m := ""
				switch ev.Sentiment {
				case 1:
					m = green("est tout content")
				case 2:
					m = blue("veut la main")
				case 3:
					m = red("fait la gueule")
				}
				if m != "" {
					now := time.Now()
					if now.Sub(lastSentimentAnnounce).Seconds() > 2 {
						lastSentimentAnnounce = now
						say(fmt.Sprintf("Le partenaire %s.", emph(m)))
					}
				}

			case *typingEvent:
				now := time.Now()
				if lastTyping != nil {
					typeTimings = append(typeTimings, now.Sub(*lastTyping).Milliseconds())
				}
				lastTyping = &time.Time{}
				*lastTyping = now
				if now.Sub(lastTypeAnnounce).Seconds() > 4.5 {
					lastTypeAnnounce = now
					say(grey("[…]"))
				}
			}

		case p := <-prop:
			go func() {
				lastTypeAnnounce = time.Now()
				for i := range p {
					outs <- makeTyping(p[:i])
					time.Sleep(time.Millisecond * time.Duration(33+rand.Intn(55)))
				}
				outs <- makeProposition(p)
			}()

		case <-quit:
			return
		}
	}
}

func medianTiming(deltas []int64) (mean float64, sd float64) {
	if len(deltas) < 2 {
		return 0, 0
	}
	var total float64
	for _, v := range deltas {
		total += float64(v)
	}
	mean = total / float64(len(deltas))
	for _, v := range deltas {
		sd += math.Pow(float64(v)-mean, 2)
	}
	sd = math.Sqrt(sd / float64(len(deltas)))
	return
}
