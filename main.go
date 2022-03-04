package main

import (
	"encoding/json"
	"fmt"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"golang.org/x/text/language"
	"golang.org/x/text/message"
	"io/ioutil"
	"log"
	"math"
	"net/http"
	"os"
	"strconv"
	"strings"
)

type Basic struct {
	ItemCode string `json:"itemCode"`
	StockName string `json:"stockName"`
	ClosePrice string `json:"closePrice"`
	CompareToPreviousClosePrice string `json:"CompareToPreviousClosePrice"`
}

type Majors struct {
	HomeMajors []HomeMajor `json:"homeMajors"`
}

type HomeMajor struct {
	ItemCode string `json:"itemCode"`
	Name string `json:"name"`
	ClosePrice string `json:"closePrice"`
	CompareToPreviousClosePrice string `json:"CompareToPreviousClosePrice"`
	FluctuationRatio string `json:"fluctuationRatio"`
}

type StockItem interface {
	StockId() string
	Price() float64
	Name() string
	CompareToPreviousPrice() float64
}

// ---------------------------------------

func (b Basic) StockId() string {
	return b.ItemCode
}

func (b Basic) Price() float64 {
	price, err := strconv.ParseFloat(strings.ReplaceAll(b.ClosePrice, ",", ""), 64)
	if err != nil {
		return 0
	}
	return price
}

func (b Basic) Name() string {
	return b.StockName
}

func (b Basic) CompareToPreviousPrice() float64 {
	compareToPreviousClosePrice, err := strconv.ParseFloat(strings.ReplaceAll(b.CompareToPreviousClosePrice, ",", ""), 64)
	if err != nil {
		return 0
	}
	return compareToPreviousClosePrice
}

// ---------------------------------------

func (b Majors) StockId() string {
	return b.HomeMajors[0].ItemCode
}

func (b Majors) Price() float64 {
	price, err := strconv.ParseFloat(strings.ReplaceAll(b.HomeMajors[0].ClosePrice, ",", ""), 64)
	if err != nil {
		return 0
	}
	return float64(price)
}

func (b Majors) Name() string {
	return b.HomeMajors[0].Name
}

func (b Majors) CompareToPreviousPrice() float64 {
	compareToPreviousClosePrice, err := strconv.ParseFloat(strings.ReplaceAll(b.HomeMajors[0].CompareToPreviousClosePrice, ",", ""), 64)
	if err != nil {
		return 0
	}
	return float64(compareToPreviousClosePrice)
}

// ---------------------------------------

func getStockItemText(s StockItem, frac bool) string {
	closePrice := s.Price()
	compareToPreviousClosePrice := s.CompareToPreviousPrice()

	prevClosePrice := closePrice - compareToPreviousClosePrice
	percent := compareToPreviousClosePrice / prevClosePrice * 100

	var percentIcon string
	if percent > 0 {
		percentIcon = "🔺"
	} else if percent < 0 {
		percentIcon = "🦋"
	} else {
		percentIcon = ""
	}

	percent = math.Abs(percent)
	if compareToPreviousClosePrice < 0 {
		compareToPreviousClosePrice = -compareToPreviousClosePrice
	}

	p := message.NewPrinter(language.English)
	if frac == false {
		return p.Sprintf("%s\n현재가: %.0f\n전일비: %s%.0f (%.2f%%)", s.Name(), closePrice, percentIcon, compareToPreviousClosePrice, percent)
	} else {
		return p.Sprintf("%s\n현재가: %.2f\n전일비: %s%.2f (%.2f%%)", s.Name(), closePrice, percentIcon, compareToPreviousClosePrice, percent)
	}
}

func main() {
	bot, err := tgbotapi.NewBotAPI(os.Getenv("CHEAP_ALTARI_BOT_TOKEN"))
	if err != nil {
		log.Panic(err)
	}

	bot.Debug = true

	log.Printf("Authorized on account %s", bot.Self.UserName)

	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60

	updates := bot.GetUpdatesChan(u)

	for update := range updates {
		if update.Message != nil { // If we got a message
			log.Printf("[%s] %s", update.Message.From.UserName, update.Message.Text)

			words := strings.Fields(update.Message.Text)
			
			if len(words) <= 0 {
				continue
			}
			
			stockId := ""
			
			if words[0] == "/k" {
				stockId = "259960"
			} else if words[0] == "/n" {
				stockId = "036570"
			} else if words[0] == "/a" {
				stockId = "027360"
			} else if words[0] == "/s" && len(words) > 1 {
				stockId = words[1]
			} else if words[0] == "/kospi" {
				stockId = "kospi"
			} else if words[0] == "/spy" {
				stockId = "SPY"
			}

			text, err := getStockPriceText(stockId)

			if err != nil || text == "" {
				text = "오류"
			}

			msg := tgbotapi.NewMessage(update.Message.Chat.ID, text)
			//msg.ReplyToMessageID = update.Message.MessageID

			_, _ = bot.Send(msg)
		}
	}
}

func getStockPriceText(stockId string) (string, error) {
	var getUrl string
	var frac bool
	if stockId == "kospi" {
		getUrl = "https://m.stock.naver.com/api/home/majors"
		frac = false
	} else if stockId == "SPY" {
		getUrl = "https://api.stock.naver.com/etf/SPY/basic"
		frac = true
	} else {
		getUrl = fmt.Sprintf("https://m.stock.naver.com/api/stock/%s/basic", stockId)
		frac = false
	}

	// GET 호출
	resp, err := http.Get(getUrl)
	if err != nil {
		panic(err)
	}
	defer resp.Body.Close()

	// 결과 출력
	data, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		panic(err)
	}
	fmt.Printf("%s\n", string(data))

	var stockItem StockItem
	var basic Basic
	var majors Majors
	if err := json.Unmarshal(data, &basic); err == nil && basic.ClosePrice != "" || basic.CompareToPreviousClosePrice != "" {
		stockItem = basic
	} else if err := json.Unmarshal(data, &majors); err == nil && len(majors.HomeMajors) > 0 {
		stockItem = majors
	} else {
		return "", err
	}

	return getStockItemText(stockItem, frac), nil
}