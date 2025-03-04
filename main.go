package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"golang.org/x/text/message"
	"io"
	"log"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/google/go-github/v38/github"
	"github.com/joho/godotenv"
	"golang.org/x/text/language"
)

type YahooFinanceJson struct {
	Chart YahooFinanceChart `json:"chart"`
}

type YahooFinanceChart struct {
	Result []YahooFinanceResult `json:"result"`
}

type YahooFinanceResult struct {
	Meta YahooFinanceMeta `json:"meta"`
}

type YahooFinanceMeta struct {
	Symbol             string  `json:"symbol"`
	RegularMarketPrice float32 `json:"regularMarketPrice"`
	ChartPreviousClose float32 `json:"chartPreviousClose"`
}

type InvestingJson struct {
	Html InvestingHtml `json:"html"`
}

type InvestingHtml struct {
	ChartInfo string `json:"chart_info"`
}

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

// 한국투자증권
type KisResult struct {
	Output struct {
		Rsym string `json:"rsym"`
		Zdiv string `json:"zdiv"`
		Base string `json:"base"`
		Pvol string `json:"pvol"`
		Last string `json:"last"`
		Sign string `json:"sign"`
		Diff string `json:"diff"`
		Rate string `json:"rate"`
		Tvol string `json:"tvol"`
		Tamt string `json:"tamt"`
		Ordy string `json:"ordy"`
	} `json:"output"`
	RtCd  string `json:"rt_cd"`
	MsgCd string `json:"msg_cd"`
	Msg1  string `json:"msg1"`
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

	nameText := s.Name()
	var closePriceText string
	var deltaText string

	if !frac {
		closePriceText = p.Sprintf("현재가: %.0f", closePrice)
		deltaText = p.Sprintf("전일비: %s%.0f (%.2f%%)", percentIcon, compareToPreviousClosePrice, percent)
	} else {
		closePriceText = p.Sprintf("현재가: %.2f", closePrice)
		deltaText = p.Sprintf("전일비: %s%.2f (%.2f%%)", percentIcon, compareToPreviousClosePrice, percent)
	}

	return p.Sprintf("*%s*\n%s\n%s",
		escapeMarkdownText(nameText),
		escapeMarkdownText(closePriceText),
		escapeMarkdownText(deltaText))
}

func escapeMarkdownText(t string) string {
	escapeList := []rune{'_', '*', '[', ']', '(', ')', '~', '`', '>', '#', '+', '-', '=', '|', '{', '}', '.', '!'}
	for _, e := range escapeList {
		t = strings.ReplaceAll(t, string(e), "\\"+string(e))
	}
	return t
}

var shutdownCh chan string

func startGitHubPushListener() {
	shutdownCh = make(chan string)

	m := http.NewServeMux()
	m.HandleFunc("/onGitHubPush", handleOnGitHubPush)

	var server *http.Server

	if os.Getenv("CHEAP_ALTARI_BOT_SERVER_DEV") == "1" {
		addr := ":21092"
		log.Printf("개발 서버네요!!! HTTP addr=%s", addr)
		server = &http.Server{Addr: addr, Handler: m}

		go func() {
			if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				panic(err)
			}
		}()
	} else {
		addr := ":21093"
		log.Printf("실서비스 서버네요!!! HTTPS addr=%s", addr)
		server = &http.Server{Addr: addr, Handler: m}

		go func() {
			certFilePath := os.Args[1]
			keyFilePath := os.Args[2]
			if err := server.ListenAndServeTLS(certFilePath, keyFilePath); err != nil && err != http.ErrServerClosed {
				panic(err)
			}
		}()
	}

	shutdownMsg := <-shutdownCh
	_ = server.Shutdown(context.Background())
	log.Printf("Shutdown message: %s", shutdownMsg)

	log.Println("Gracefully shutdown")
	os.Exit(0)
}

func main() {
	log.Println("cheap-altari-bot: load .env file")
	err := godotenv.Load(".env")
	if err != nil {
		panic(err)
	}

	/*
		a, b := getStockPriceTextFromInvesting("BOTZ")
		log.Println(a)
		log.Println(b)
	*/
	go startGitHubPushListener()

	bot, err := tgbotapi.NewBotAPI(os.Getenv("CHEAP_ALTARI_BOT_TOKEN"))
	if err != nil {
		log.Panic(err)
	}

	bot.Debug = true

	log.Printf("Authorized on account %s", bot.Self.UserName)

	//bot.Request(tgbotapi.DeleteWebhookConfig{})

	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60

	updates := bot.GetUpdatesChan(u)
	for update := range updates {
		handleUpdate(bot, update)
	}
}

func handleUpdate(bot *tgbotapi.BotAPI, update tgbotapi.Update) {
	if update.Message != nil && update.Message.ReplyToMessage == nil { // If we got a message
		log.Printf("[%s] %s", update.Message.From.UserName, update.Message.Text)

		words := strings.Fields(update.Message.Text)

		if len(words) <= 0 {
			return
		}

		if words[0] == "/kdream" {
			dreamStockItem := Basic{
				ItemCode:                    "259960",
				StockName:                   "크래프톤",
				ClosePrice:                  "1000000",
				CompareToPreviousClosePrice: "230000",
			}

			text := getStockItemText(dreamStockItem, false)
			msg := newMessage(update.Message.Chat.ID, text)
			_, _ = bot.Send(msg)

			return
		}

		var stockIdList []string

		if words[0] == "/k" {
			// 크래프톤
			stockIdList = append(stockIdList, "259960")
		} else if words[0] == "/n" {
			// 엔씨소프트
			stockIdList = append(stockIdList, "036570")
		} else if words[0] == "/a" {
			// 아주IB투자
			stockIdList = append(stockIdList, "027360")
		} else if words[0] == "/skh" {
			// SK하이닉스
			stockIdList = append(stockIdList, "000660")
		} else if words[0] == "/energy" {
			// KODEX K-신재생에너지액티브
			stockIdList = append(stockIdList, "385510")
		} else if words[0] == "/kg" {
			// 카카오게임즈
			stockIdList = append(stockIdList, "293490")
		} else if words[0] == "/lgd" {
			// LG디스플레이
			stockIdList = append(stockIdList, "034220")
		} else if words[0] == "/p" {
			// 펄어비스
			stockIdList = append(stockIdList, "263750")
		} else if words[0] == "/c" {
			// 컴투스
			stockIdList = append(stockIdList, "078340")
		} else if words[0] == "/N" {
			// 엔씨소프트
			stockIdList = append(stockIdList, "036570")
			// 넷마블
			stockIdList = append(stockIdList, "251270")
			// 네오위즈
			stockIdList = append(stockIdList, "095660")
		} else if words[0] == "/s" && len(words) > 1 {
			// 종목 직접 지정 ('/s 259960', '/s qsi' 등)
			stockIdList = append(stockIdList, words[1])
		} else if words[0] == "/kospi" {
			// 코스피
			stockIdList = append(stockIdList, "kospi")
		} else if words[0] == "/spy" {
			// SPDR S&P 500 ETF Trust
			stockIdList = append(stockIdList, "SPY")
		} else if words[0] == "/qqq" {
			// Invesco QQQ Trust
			stockIdList = append(stockIdList, "QQQ")
		} else if words[0] == "/botz" {
			stockIdList = append(stockIdList, "BOTZ")
		} else if words[0] == "/qsi" {
			stockIdList = append(stockIdList, "QSI")
		} else if words[0] == "/pump" {
			stockIdList = append(stockIdList, "PUMP")
		}

		if len(stockIdList) > 0 {

			var textAppended string
			for _, stockId := range stockIdList {

				text, err := getStockPriceTextFromYahoo(stockId) // 1차 시도
				if err != nil {
					text, err = getStockPriceTextFromKIS("AMS", stockId) // 2차 시도
					if err != nil {
						text, err = getStockPriceTextFromKIS("NAS", stockId) // 3차 시도
						if err != nil {
							text = "오류"
						}
					}
				}

				textAppended = textAppended + text + "\n"
			}

			msg := newMessage(update.Message.Chat.ID, textAppended)
			_, _ = bot.Send(msg)

			return
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
		}

		if imageId != "" {
			ibytes, err := os.ReadFile(filepath.Join("./images", imageId))

			if err == nil {

				file := tgbotapi.FileBytes{
					Name:  imageId,
					Bytes: ibytes,
				}

				photo := tgbotapi.NewPhoto(update.Message.Chat.ID, file)
				_, _ = bot.Send(photo)

				return
			}
		}

		msg := newMessage(update.Message.Chat.ID, "오류")
		//msg.ReplyToMessageID = update.Message.MessageID
		_, _ = bot.Send(msg)
	}
}

func newMessage(chatId int64, text string) tgbotapi.MessageConfig {
	return tgbotapi.MessageConfig{
		BaseChat: tgbotapi.BaseChat{
			ChatID:           chatId,
			ReplyToMessageID: 0,
		},
		Text:                  text,
		ParseMode:             "MarkdownV2",
		DisableWebPagePreview: false,
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
	defer func(Body io.ReadCloser) {
		_ = Body.Close()
	}(resp.Body)

	// 결과 출력
	data, err := io.ReadAll(resp.Body)
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

func getStockPriceTextFromKIS(exchangeId, stockId string) (string, error) {
	var getUrl string
	var frac bool
	if len(stockId) > 0 && ((stockId[0] >= 'a' && stockId[0] <= 'z') || (stockId[0] >= 'A' && stockId[0] <= 'Z')) {
		getUrl = fmt.Sprintf("http://localhost:26704/query?excd=%s&symb=%s", exchangeId, strings.ToUpper(stockId))
		frac = true
	} else {
		// 나머지는 다 네이버 파이낸셜로...
		return getStockPriceText(stockId)
	}

	req, _ := http.NewRequest("GET", getUrl, nil)

	client := new(http.Client)
	resp, err := client.Do(req)

	if err != nil {
		panic(err)
	}
	defer func(Body io.ReadCloser) {
		_ = Body.Close()
	}(resp.Body)

	// 결과 출력
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		panic(err)
	}
	fmt.Printf("%s\n", string(data))

	var kisResult KisResult
	if err := json.Unmarshal(data, &kisResult); err == nil {

		if len(kisResult.Output.Last) == 0 {
			return "", errors.New("empty array")
		}

		base, _ := strconv.ParseFloat(kisResult.Output.Base, 64)
		last, _ := strconv.ParseFloat(kisResult.Output.Last, 64)

		basic := Basic{
			StockName:                   kisResult.Output.Rsym[4:],
			ClosePrice:                  fmt.Sprintf("%.2f", last),
			CompareToPreviousClosePrice: fmt.Sprintf("%.2f", last-base),
		}

		return getStockItemText(basic, frac), nil
	} else {
		return "", err
	}

	return "", nil
}

func getStockPriceTextFromYahoo(stockId string) (string, error) {
	var getUrl string
	var frac bool
	if stockId == "kospi" {
		return getStockPriceText(stockId)
	} else if len(stockId) > 0 && ((stockId[0] >= 'a' && stockId[0] <= 'z') || (stockId[0] >= 'A' && stockId[0] <= 'Z')) {
		getUrl = fmt.Sprintf("https://query1.finance.yahoo.com/v8/finance/chart/%s?interval=3mo", strings.ToUpper(stockId))
		frac = true
	} else {
		// 나머지는 다 네이버 파이낸셜로...
		return getStockPriceText(stockId)
	}

	req, _ := http.NewRequest("GET", getUrl, nil)

	client := new(http.Client)
	resp, err := client.Do(req)

	if err != nil {
		panic(err)
	}
	defer func(Body io.ReadCloser) {
		_ = Body.Close()
	}(resp.Body)

	// 결과 출력
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		panic(err)
	}
	fmt.Printf("%s\n", string(data))

	var yahooFinanceJson YahooFinanceJson
	if err := json.Unmarshal(data, &yahooFinanceJson); err == nil {

		if len(yahooFinanceJson.Chart.Result) == 0 {
			return "", errors.New("empty array")
		}

		result := yahooFinanceJson.Chart.Result[0]

		basic := Basic{
			StockName:                   result.Meta.Symbol,
			ClosePrice:                  fmt.Sprintf("%.2f", result.Meta.RegularMarketPrice),
			CompareToPreviousClosePrice: fmt.Sprintf("%.2f", result.Meta.RegularMarketPrice-result.Meta.ChartPreviousClose),
		}

		return getStockItemText(basic, frac), nil
	} else {
		return "", err
	}

	return "", nil
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
