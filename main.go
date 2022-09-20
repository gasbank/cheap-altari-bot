package main

import (
	"context"
	"encoding/json"
	"fmt"
	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
	"golang.org/x/text/message"
	"io/ioutil"
	"log"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/go-redis/redis/v8"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/google/go-github/v38/github"
	"github.com/joho/godotenv"
	"golang.org/x/text/language"
)

var rdbContext = context.Background()
var rdb *redis.Client

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

	rdb = redis.NewClient(&redis.Options{
		Addr:     "",
		Password: "", // no password set
		DB:       0,  // use default DB
	})

	//subscriber := rdb.Subscribe(rdbContext, "cheap-altari-bot")

	/*
	updates := bot.GetUpdatesChan(u)
	for update := range updates {
		handleUpdate(bot, update)
	}
	*/

	subscriber := rdb.Subscribe(rdbContext, "cheap-altari-bot")
	for {
		msg, err := subscriber.ReceiveMessage(rdbContext)
		if err != nil {
			continue
		}

		var update tgbotapi.Update
		if err := json.Unmarshal([]byte(msg.Payload), &update); err != nil {
			continue
		}

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
			msg := tgbotapi.NewMessage(update.Message.Chat.ID, text)
			_, _ = bot.Send(msg)

			return
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
		} else if words[0] == "/kg" {
			// 카카오게임즈
			stockId = "293490"
		} else if words[0] == "/lgd" {
			// LG디스플레이
			stockId = "034220"
		} else if words[0] == "/s" && len(words) > 1 {
			// 종목 직접 지정 ('/s 259960')
			stockId = words[1]
		} else if words[0] == "/kospi" {
			// 코스피
			stockId = "kospi"
		} else if words[0] == "/spy" {
			// SPDR S&P 500 ETF Trust
			stockId = "SPY"
		} else if words[0] == "/qqq" {
			// Invesco QQQ Trust
			stockId = "QQQ"
		} else if words[0] == "/botz" {
			// Invesco QQQ Trust
			stockId = "BOTZ"
		}

		if stockId != "" {
			text, err := getStockPriceTextFromInvesting(stockId)
			//msg.ReplyToMessageID = update.Message.MessageID
			if err != nil {
				text = "오류"
			}
			msg := tgbotapi.NewMessage(update.Message.Chat.ID, text)
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

				return
			}
		}

		msg := tgbotapi.NewMessage(update.Message.Chat.ID, "오류")
		//msg.ReplyToMessageID = update.Message.MessageID
		_, _ = bot.Send(msg)
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

func findTextDataBySpanIdAnchors(node *html.Node, spanId string) string {
	for c := node.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == html.ElementNode {
			if c.DataAtom == atom.Span {
				for _, v := range c.Attr {
					if v.Key == "id" && v.Val == spanId {
						for cc := c.FirstChild; cc != nil; cc = cc.NextSibling {
							if cc.Type == html.TextNode {
								return cc.Data
							}
						}
					}
				}
			}
		}

		childRet := findTextDataBySpanIdAnchors(c, spanId)
		if childRet != "" {
			return childRet
		}
	}

	return ""
}

func getStockPriceTextFromInvesting(stockId string) (string, error) {
	var getUrl string
	var frac bool
	var referer string
	if stockId == "QQQ" {
		getUrl = "https://jp.investing.com/common/modules/js_instrument_chart/api/data.php?pair_id=651&pair_id_for_news=651&chart_type=area&pair_interval=86400&candle_count=120&events=yes&volume_series=yes"
		referer = "https://jp.investing.com/etfs/powershares-qqqq"
		frac = true
	} else if stockId == "BOTZ" {
		getUrl = "https://jp.investing.com/common/modules/js_instrument_chart/api/data.php?pair_id=995739&pair_id_for_news=995739&chart_type=area&pair_interval=86400&candle_count=120&events=yes&volume_series=yes"
		referer = "https://jp.investing.com/etfs/global-x-robotics---ai-usd"
		frac = true
	} else {
		// 나머지는 다 네이버 파이낸셜로...
		return getStockPriceText(stockId)
	}

	req, _ := http.NewRequest("GET", getUrl, nil)
	req.Header.Set("referer", referer)
	req.Header.Set("user-agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/104.0.5112.102 Safari/537.36")
	req.Header.Set("x-requested-with", "XMLHttpRequest")

	client := new(http.Client)
	resp, err := client.Do(req)

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

	var investingJson InvestingJson
	if err := json.Unmarshal(data, &investingJson); err == nil {
		node, err := html.Parse(strings.NewReader(investingJson.Html.ChartInfo))
		if err != nil {
			log.Fatal(err)
		}

		basic := Basic{
			StockName:                   findTextDataBySpanIdAnchors(node, "chart-info-symbol"),
			ClosePrice:                  findTextDataBySpanIdAnchors(node, "chart-info-last"),
			CompareToPreviousClosePrice: findTextDataBySpanIdAnchors(node, "chart-info-change"),
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
