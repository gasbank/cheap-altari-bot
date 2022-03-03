package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

type Basic struct {
	StockName string `json:"stockName"`
	ClosePrice string `json:"closePrice"`
	CompareToPreviousClosePrice string `json:"CompareToPreviousClosePrice"`
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
	// GET 호출
	resp, err := http.Get(fmt.Sprintf("https://m.stock.naver.com/api/stock/%s/basic", stockId))
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
	var basic Basic
	if err := json.Unmarshal(data, &basic); err != nil {
		return "", err
	}

	closePrice, err := strconv.Atoi(strings.ReplaceAll(basic.ClosePrice, ",", ""))
	if err != nil {
		return "", err
	}

	compareToPreviousClosePrice, err := strconv.Atoi(strings.ReplaceAll(basic.CompareToPreviousClosePrice, ",", ""))
	if err != nil {
		return "", err
	}

	if closePrice == 0 || compareToPreviousClosePrice == 0 {
		return "", nil
	}

	prevClosePrice := closePrice - compareToPreviousClosePrice
	percent := float64(compareToPreviousClosePrice) / float64(prevClosePrice) * 100

	fmt.Println(basic.ClosePrice)

	var percentIcon string
	if percent > 0 {
		percentIcon = "🔺"
	} else if percent < 0 {
		percentIcon = "🦋"
	} else {
		percentIcon = ""
	}
	text := fmt.Sprintf("%s\n현재가: %s\n전일비: %s%s (%.2f%%)", basic.StockName, basic.ClosePrice, percentIcon, basic.CompareToPreviousClosePrice, percent)
	return text, nil
}