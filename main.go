package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/google/go-github/v38/github"
	"golang.org/x/text/language"
	"golang.org/x/text/message"
)

type Basic struct {
	ItemCode                    string `json:"itemCode"`
	StockName                   string `json:"stockName"`
	ClosePrice                  string `json:"closePrice"`
	CompareToPreviousClosePrice string `json:"CompareToPreviousClosePrice"`
}

type Majors struct {
	HomeMajors []HomeMajor `json:"homeMajors"`
}

type HomeMajor struct {
	ItemCode                    string `json:"itemCode"`
	Name                        string `json:"name"`
	ClosePrice                  string `json:"closePrice"`
	CompareToPreviousClosePrice string `json:"CompareToPreviousClosePrice"`
	FluctuationRatio            string `json:"fluctuationRatio"`
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

func (b Majors) GetHomeMajor() HomeMajor {
	for i := 0; i < len(b.HomeMajors); i++ {
		if b.HomeMajors[i].ItemCode == "KOSPI" {
			return b.HomeMajors[i]
		}
	}
	return HomeMajor{}
}

func (b Majors) StockId() string {
	return b.GetHomeMajor().ItemCode
}

func (b Majors) Price() float64 {
	price, err := strconv.ParseFloat(strings.ReplaceAll(b.GetHomeMajor().ClosePrice, ",", ""), 64)
	if err != nil {
		return 0
	}
	return price
}

func (b Majors) Name() string {
	return b.GetHomeMajor().Name
}

func (b Majors) CompareToPreviousPrice() float64 {
	compareToPreviousClosePrice, err := strconv.ParseFloat(strings.ReplaceAll(b.GetHomeMajor().CompareToPreviousClosePrice, ",", ""), 64)
	if err != nil {
		return 0
	}
	return compareToPreviousClosePrice
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

var shutdownCh chan string

func startGitHubPushListener() {
	shutdownCh = make(chan string)

	m := http.NewServeMux()
	m.HandleFunc("/onGitHubPush", handleOnGitHubPush)

	var server *http.Server

	if os.Getenv("CHEAP_ALTARI_BOT_SERVER_DEV") == "1" {
		addr := ":21092"
		log.Println(fmt.Sprintf("개발 서버네요!!! HTTP addr=%s", addr))
		server = &http.Server{Addr: addr, Handler: m}

		go func() {
			if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				panic(err)
			}
		}()
	} else {
		addr := ":21093"
		log.Println(fmt.Sprintf("실서비스 서버네요!!! HTTPS addr=%s", addr))
		server = &http.Server{Addr: addr, Handler: m}

		go func() {
			certFilePath := os.Args[1]
			keyFilePath := os.Args[2]
			if err := server.ListenAndServeTLS(certFilePath, keyFilePath); err != nil && err != http.ErrServerClosed {
				panic(err)
			}
		}()
	}

	select {
	case shutdownMsg := <-shutdownCh:
		_ = server.Shutdown(context.Background())
		log.Printf("Shutdown message: %s", shutdownMsg)
	}

	log.Println("Gracefully shutdown")
	os.Exit(0)
}

func main() {
	go startGitHubPushListener()

	bot, err := tgbotapi.NewBotAPI(os.Getenv("CHEAP_ALTARI_BOT_TOKEN"))
	if err != nil {
		log.Panic(err)
	}

	bot.Debug = true

	log.Printf("Authorized on account %s", bot.Self.UserName)

	bot.Request(tgbotapi.DeleteWebhookConfig{})

	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60

	updates := bot.GetUpdatesChan(u)

	for update := range updates {
		if update.Message != nil && update.Message.ReplyToMessage == nil { // If we got a message
			log.Printf("[%s] %s", update.Message.From.UserName, update.Message.Text)

			words := strings.Fields(update.Message.Text)

			if len(words) <= 0 {
				continue
			}

			if words[0] == "/kdream" {
				dreamStockItem := Basic{
					ItemCode:                    "259960",
					StockName:                   "크래프톤",
					ClosePrice:                  "1000000",
					CompareToPreviousClosePrice: "770000",
				}

				text := getStockItemText(dreamStockItem, false)
				msg := tgbotapi.NewMessage(update.Message.Chat.ID, text)
				_, _ = bot.Send(msg)

				continue
			}

			stockId := ""

			if words[0] == "/k" {
				// 크래프톤
				stockId = "259960"
			} else if words[0] == "/n" {
				// 엔씨소프트
				stockId = "036570"
			} else if words[0] == "/a" {
				// 아주IB투자
				stockId = "027360"
			} else if words[0] == "/skh" {
				// SK하이닉스
				stockId = "000660"
			} else if words[0] == "/energy" {
				// KODEX K-신재생에너지액티브
				stockId = "385510"
			} else if words[0] == "/s" && len(words) > 1 {
				// 종목 직접 지정 ('/s 259960')
				stockId = words[1]
			} else if words[0] == "/kospi" {
				// 코스피
				stockId = "kospi"
			} else if words[0] == "/spy" {
				// SPDR S&P 500 ETF Trust
				stockId = "SPY"
			}

			if stockId != "" {
				text, err := getStockPriceText(stockId)
				//msg.ReplyToMessageID = update.Message.MessageID
				if err != nil {
					text = "오류"
				}
				msg := tgbotapi.NewMessage(update.Message.Chat.ID, text)
				_, _ = bot.Send(msg)

				continue
			}

			imageId := ""

			if words[0] == "/ggul" {
				imageId = "ggul_bird.png"
			} else if words[0] == "/palggul" {
				imageId = "parggul.jpg"
			} else if words[0] == "/salggul" {
				imageId = "salggul.png"
			} else if words[0] == "/racoon" {
				imageId = "racoon.jpg"
			} else if words[0] == "/racoon" {
				imageId = "racoon.jpg"
			}

			if imageId != "" {
				ibytes, err := ioutil.ReadFile(filepath.Join("./images", imageId))

				if err == nil {

					file := tgbotapi.FileBytes{
						Name:  imageId,
						Bytes: ibytes,
					}

					photo := tgbotapi.NewPhoto(update.Message.Chat.ID, file)
					bot.Send(photo)

					continue
				}
			}

			msg := tgbotapi.NewMessage(update.Message.Chat.ID, "오류")
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

func handleOnGitHubPush(writer http.ResponseWriter, request *http.Request) {
	log.Println("handleOnGitHubPush")

	payload, err := github.ValidatePayload(request, []byte(os.Getenv("CHEAP_ALTARI_BOT_GITHUB_WEBHOOK_SECRET")))
	if err != nil {
		log.Println(err)
		if os.Getenv("CHEAP_ALTARI_BOT_SERVER_DEV") != "1" {
			return
		}
	}

	_, _ = writer.Write([]byte("ok"))

	log.Println(string(payload))
	log.Println("Exit by push.")
	shutdownCh <- "bye"
}