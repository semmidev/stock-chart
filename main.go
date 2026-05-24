package main

import (
	"encoding/json"
	"html/template"
	"log"
	"math"
	"math/rand"
	"net/http"
	"sync"
	"time"
)

// ─── Data Types ────────────────────────────────────────────────────────────────

type Candle struct {
	Time   int64   `json:"time"`
	Open   float64 `json:"open"`
	High   float64 `json:"high"`
	Low    float64 `json:"low"`
	Close  float64 `json:"close"`
	Volume float64 `json:"volume"`
}

type Stock struct {
	Symbol     string  `json:"symbol"`
	Name       string  `json:"name"`
	LastPrice  float64 `json:"lastPrice"`
	Change     float64 `json:"change"`
	ChangePct  float64 `json:"changePct"`
	Open       float64 `json:"open"`
	High       float64 `json:"high"`
	Low        float64 `json:"low"`
	Volume     int64   `json:"volume"`
	MarketCap  string  `json:"marketCap"`
	PERatio    float64 `json:"peRatio"`
	WeekHigh52 float64 `json:"weekHigh52"`
	WeekLow52  float64 `json:"weekLow52"`
}

type MarketStore struct {
	mu      sync.RWMutex
	candles map[string][]Candle
	stocks  map[string]*Stock
	latest  map[string]Candle
}

// ─── Global State ──────────────────────────────────────────────────────────────

var store = &MarketStore{
	candles: make(map[string][]Candle),
	stocks:  make(map[string]*Stock),
	latest:  make(map[string]Candle),
}

var symbols = []struct {
	symbol    string
	name      string
	basePrice float64
	peRatio   float64
	marketCap string
}{
	{"BBCA", "Bank Central Asia", 9800, 24.5, "Rp 1.200 T"},
	{"TLKM", "Telkom Indonesia", 3720, 18.2, "Rp 370 T"},
	{"ASII", "Astra International", 5025, 12.8, "Rp 203 T"},
	{"BMRI", "Bank Mandiri", 6325, 14.1, "Rp 295 T"},
	{"GOTO", "GoTo Group", 88, 0, "Rp 95 T"},
}

// ─── Data Generation ───────────────────────────────────────────────────────────

func generateInitialData(symbol string, basePrice float64) []Candle {
	var candles []Candle
	price := basePrice
	now := time.Now()
	// start 200 days ago, 1 candle per day
	start := now.AddDate(0, 0, -200)
	// align to start of day
	start = time.Date(start.Year(), start.Month(), start.Day(), 9, 0, 0, 0, start.Location())

	for i := 0; i < 200; i++ {
		t := start.AddDate(0, 0, i)
		// skip weekends
		if t.Weekday() == time.Saturday || t.Weekday() == time.Sunday {
			continue
		}

		change := (rand.Float64()*2 - 1) * 0.025
		open := price
		closeP := price * (1 + change)
		high := math.Max(open, closeP) * (1 + rand.Float64()*0.01)
		low := math.Min(open, closeP) * (1 - rand.Float64()*0.01)
		vol := 1000000 + rand.Float64()*9000000

		candles = append(candles, Candle{
			Time:   t.Unix(),
			Open:   round2(open),
			High:   round2(high),
			Low:    round2(low),
			Close:  round2(closeP),
			Volume: round2(vol),
		})
		price = closeP
	}
	return candles
}

func round2(f float64) float64 {
	return math.Round(f*100) / 100
}

func initStocks() {
	for _, s := range symbols {
		candles := generateInitialData(s.symbol, s.basePrice)
		store.candles[s.symbol] = candles

		last := candles[len(candles)-1]
		first := candles[0]
		change := last.Close - first.Open
		changePct := (change / first.Open) * 100

		// 52 week stats
		high52, low52 := last.High, last.Low
		for _, c := range candles {
			if c.High > high52 {
				high52 = c.High
			}
			if c.Low < low52 {
				low52 = c.Low
			}
		}

		totalVol := int64(0)
		for _, c := range candles {
			totalVol += int64(c.Volume)
		}

		store.stocks[s.symbol] = &Stock{
			Symbol:     s.symbol,
			Name:       s.name,
			LastPrice:  last.Close,
			Change:     round2(last.Close - candles[len(candles)-2].Close),
			ChangePct:  round2((last.Close - candles[len(candles)-2].Close) / candles[len(candles)-2].Close * 100),
			Open:       last.Open,
			High:       last.High,
			Low:        last.Low,
			Volume:     int64(last.Volume),
			MarketCap:  s.marketCap,
			PERatio:    s.peRatio,
			WeekHigh52: round2(high52),
			WeekLow52:  round2(low52),
		}
		store.latest[s.symbol] = last
		_ = change
		_ = changePct
	}
}

// ─── Background Ticker ─────────────────────────────────────────────────────────

func startMarketSimulator() {
	go func() {
		ticker := time.NewTicker(1 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			store.mu.Lock()
			for _, s := range symbols {
				sym := s.symbol
				candles := store.candles[sym]
				if len(candles) == 0 {
					continue
				}
				last := candles[len(candles)-1]
				stock := store.stocks[sym]

				// simulate tick: sometimes extend current candle, sometimes new candle
				change := (rand.Float64()*2 - 1) * 0.008
				newClose := round2(last.Close * (1 + change))
				newHigh := math.Max(last.High, newClose)
				newLow := math.Min(last.Low, newClose)

				// every ~30s add a new candle (10 ticks * 3s)
				if rand.Intn(10) == 0 {
					newCandle := Candle{
						Time:   last.Time + 86400, // next day
						Open:   last.Close,
						High:   round2(newHigh),
						Low:    round2(newLow),
						Close:  newClose,
						Volume: round2(500000 + rand.Float64()*4500000),
					}
					store.candles[sym] = append(store.candles[sym], newCandle)
					store.latest[sym] = newCandle
				} else {
					// update last candle
					candles[len(candles)-1].Close = newClose
					candles[len(candles)-1].High = round2(newHigh)
					candles[len(candles)-1].Low = round2(newLow)
					store.latest[sym] = candles[len(candles)-1]
				}

				// update stock meta
				prevClose := candles[len(candles)-2].Close
				if len(candles) >= 2 {
					prevClose = candles[len(candles)-2].Close
				}
				stock.LastPrice = newClose
				stock.Change = round2(newClose - prevClose)
				stock.ChangePct = round2((newClose - prevClose) / prevClose * 100)
				stock.High = round2(math.Max(stock.High, newClose))
				stock.Low = round2(math.Min(stock.Low, newClose))
			}
			store.mu.Unlock()
		}
	}()
}

// ─── HTTP Handlers ─────────────────────────────────────────────────────────────

var tmpl = template.Must(template.ParseFiles("index.html"))

func handleIndex(w http.ResponseWriter, r *http.Request) {
	store.mu.RLock()
	stockList := make([]*Stock, 0, len(store.stocks))
	for _, sym := range symbols {
		stockList = append(stockList, store.stocks[sym.symbol])
	}
	defaultSymbol := symbols[0].symbol
	store.mu.RUnlock()

	data := map[string]interface{}{
		"Stocks":        stockList,
		"DefaultSymbol": defaultSymbol,
	}
	if err := tmpl.Execute(w, data); err != nil {
		http.Error(w, err.Error(), 500)
	}
}

func handleCandles(w http.ResponseWriter, r *http.Request) {
	sym := r.URL.Query().Get("symbol")
	if sym == "" {
		sym = symbols[0].symbol
	}
	store.mu.RLock()
	candles := store.candles[sym]
	store.mu.RUnlock()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(candles)
}

func handleLatest(w http.ResponseWriter, r *http.Request) {
	store.mu.RLock()
	stockList := make([]*Stock, 0, len(store.stocks))
	for _, sym := range symbols {
		stockList = append(stockList, store.stocks[sym.symbol])
	}
	latestCandles := make(map[string]Candle)
	for k, v := range store.latest {
		latestCandles[k] = v
	}
	store.mu.RUnlock()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"stocks":  stockList,
		"candles": latestCandles,
	})
}

// ─── Main ──────────────────────────────────────────────────────────────────────

func main() {
	rand.Seed(time.Now().UnixNano())
	initStocks()
	startMarketSimulator()

	http.HandleFunc("/", handleIndex)
	http.HandleFunc("/api/candles", handleCandles)
	http.HandleFunc("/api/latest", handleLatest)

	log.Println("📈 StockChart server running at http://localhost:8080")
	log.Fatal(http.ListenAndServe(":8080", nil))
}
