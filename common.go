package main

import (
	"bytes"
	"encoding/gob"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"math/rand"
	"net/http"
	"net/http/cookiejar"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/Unknwon/goconfig"
)

// SkuMeta - Sku Meta
type SkuMeta struct {
	ID       string `json:"skuid"`
	Name     string `json:"name"`
	VenderID string `json:"venderId"`
	StockURL string `json:"stockurl"`
	Cat      string `json:"cat"`
	Num      int    `json:"num"`
}

// WxMessage - WeChat Markdown Message
type WxMessage struct {
	MsgType  string `json:"msgtype"`
	Markdown struct {
		Content string `json:"content"`
	} `json:"markdown"`
}

// FsMessage - WeChat Markdown Message
type FsMessage struct {
	Title   string `json:"title"`
	Content string `json:"text"`
}

var (
	config       map[string]string
	noticeMsg    = ""
	tr           *http.Transport
	jar          *cookiejar.Jar
	listenClient *http.Client
	stop         = false
	randSrc      rand.Source
	skuState     map[string]bool
	refresh      bool

	skuMetas map[string]*SkuMeta
	area     = ""

	waittime         = 60
	speed    float64 = 1
	ch       chan os.Signal
)

func loadConf() (map[string]string, error) {
	config = make(map[string]string, 5)
	cfg, err := goconfig.LoadConfigFile("config.conf")
	if err != nil {
		log.Println("[-] Load conf file 'config.conf' error ", err)
		return config, err
	}
	// core
	config["cookies"], err = cfg.GetValue("core", "cookies")
	if err != nil {
		log.Println("[-] Load conf core.cookies error ", err)
		return config, err
	}
	config["wxBotKey"], err = cfg.GetValue("core", "wxbotkey")
	if err != nil {
		log.Println("[-] Load conf core.wxbotkey error : ", err)
		return config, err
	}
	config["fsBotKey"], err = cfg.GetValue("core", "fsbotkey")
	if err != nil {
		log.Println("[-] Load conf core.fsbotkey error ", err)
		return config, err
	}
	config["area"], err = cfg.GetValue("core", "area")
	if err != nil {
		log.Println("[-] Load conf core.area error ", err)
		return config, err
	}
	config["webhook"], err = cfg.GetValue("core", "webhook")
	if err != nil {
		log.Println("[-] Load conf core.webhook error ", err)
		return config, err
	}
	config["waittime"], err = cfg.GetValue("core", "waittime")
	if err != nil {
		config["waittime"] = "60"
	}
	config["speed"], err = cfg.GetValue("core", "speed")
	if err != nil {
		config["speed"] = "1"
	}

	area = strings.Replace(config["area"], ",", "_", -1)
	waittime, err = strconv.Atoi(config["waittime"])
	if err != nil {
		waittime = 60
	}
	speed, err = strconv.ParseFloat(config["speed"], 64)
	// speed, err = strconv.Atoi(config["speed"])
	if err != nil {
		speed = 1
	}
	// config["wait"]
	return config, nil
}

func httpPostJSON(sendURL string, sendData []byte) bool {
	// log.Println("[+] httpPostJSON url : ", sendURL)
	client := &http.Client{Transport: tr}
	req, err := http.NewRequest("POST", sendURL, bytes.NewBuffer(sendData))
	if err != nil {
		log.Println("[-] NewRequest error : ", err)
		return false
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Charset", "UTF-8")
	resp, err := client.Do(req)
	if err != nil {
		log.Println("[-] Send msg error : ", err)
		return false
	}
	defer resp.Body.Close()
	return true
}

func sendWeChatBotMsg(content string) {
	msgJSON := WxMessage{
		MsgType: "markdown",
		Markdown: struct {
			Content string `json:"content"`
		}{Content: content},
	}
	msg, _ := json.Marshal(msgJSON)
	sendURL := fmt.Sprintf("%s%s", "https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=", config["wxBotKey"])
	httpPostJSON(sendURL, msg)
}

func sendFeishuBotMsg(content string) {
	msgJSON := FsMessage{
		Title:   "JD Face Mask",
		Content: content,
	}
	msg, _ := json.Marshal(msgJSON)
	sendURL := fmt.Sprintf("%s%s", "https://open.feishu.cn/open-apis/bot/hook/", config["fsBotKey"])
	httpPostJSON(sendURL, msg)
}

func getCallback() string {
	return fmt.Sprintf("jQuery%07v", rand.New(randSrc).Int31n(1000000))
}

func getRandomTs() string {
	return fmt.Sprintf("%d", time.Now().UnixNano()/1e6)
}

func setReqHeader(req *http.Request) {
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_3) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/13.0.5 Safari/605.1.15")
	req.Header.Set("Connection", "keep-alive")
}

func loadSkuMetaCache() bool {
	file, err := os.Open(".skumeta")
	if err != nil {
		log.Println("[-] LoadSkuMetaCache::OpenFile", err.Error())
		return false
	}
	dec := gob.NewDecoder(file)
	skuMetas = make(map[string]*SkuMeta, 0)
	err = dec.Decode(&skuMetas)
	if err != nil {
		log.Println("[-] LoadSkuMetaCache::Decode", err.Error())
		return false
	}
	log.Println("[-] LoadSkuMetaCache Sucess")
	return true
}

func saveSkuMetaCache() bool {
	file, err := os.Create(".skumeta")
	if err != nil {
		log.Println("[-] SaveSkuMetaCache::CreateFile", err.Error())
		return false
	}
	enc := gob.NewEncoder(file)
	err = enc.Encode(skuMetas)
	if err != nil {
		log.Println("[-] SaveSkuMetaCache::Enccode", err.Error())
		return false
	}
	log.Println("[-] SaveSkuMetaCache Sucess")
	return true
}

func loadMask() (map[string]int, error) {
	masks := make(map[string]int, 0)
	f, err := ioutil.ReadFile("masks.json")
	if err != nil {
		log.Println("[-]", err.Error())
		return masks, err
	}
	err = json.Unmarshal(f, &masks)
	if err != nil {
		log.Println("[-]", err.Error())
		return masks, err
	}
	return masks, nil
}

func getMetaByItem(metaClient *http.Client, skuid string) (*SkuMeta, error) {
	var (
		re  *regexp.Regexp
		res [][]string
	)
	meta := &SkuMeta{ID: skuid}
	itemURL := fmt.Sprintf("https://item.jd.com/%s.html", skuid)
	log.Println("[+] ItemURL :", itemURL)
	req, err := http.NewRequest("GET", itemURL, nil)
	if err != nil {
		log.Println("[-] GMBI::NewRequest :", err.Error())
		return meta, err
	}
	setReqHeader(req)
	resp, err := metaClient.Do(req)
	if err != nil {
		log.Println("[-] GMBI::DoRequest: ", err.Error())
		return meta, err
	}
	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		log.Println("[-] GMBI::ReadBody :", err.Error())
		return meta, err
	}
	bodyStr := string(body)
	// Cat
	re = regexp.MustCompile(`(?m)cat:.\[(\d+),(\d+),(\d+)\]`)
	res = re.FindAllStringSubmatch(bodyStr, -1)
	if len(res[0]) == 4 {
		meta.Cat = fmt.Sprintf("%s,%s,%s", res[0][1], res[0][2], res[0][3])
		log.Println("[+] GMBI::Cat :", meta.Cat)
	}
	// VenderId
	re = regexp.MustCompile(`(?m)venderId:(\d+),`)
	res = re.FindAllStringSubmatch(bodyStr, -1)
	if len(res[0]) == 2 {
		meta.VenderID = res[0][1]
		log.Println("[+] GMBI::VenderID :", meta.VenderID)
	}
	// Name/Title
	re = regexp.MustCompile(`(?m)\<title\>(.*?)-京东\<\/title\>`)
	res = re.FindAllStringSubmatch(bodyStr, -1)
	if len(res[0]) == 2 {
		meta.Name = res[0][1]
		log.Println("[+] GMBI::Name :", meta.Name)
	}
	// StockURL
	meta.StockURL = fmt.Sprintf("https://c0.3.cn/stock?skuId=%s&area=%s&venderId=%s&buyNum=1&cat=%s&callback=%s", skuid, area, meta.VenderID, meta.Cat, getCallback())
	log.Println("[+] GMBI::StockURL :", meta.StockURL)
	return meta, nil
}

func getSkuMeta(masks map[string]int) bool {
	metaClient := &http.Client{
		Transport: tr,
	}
	skuMetas = make(map[string]*SkuMeta, 0)
	for skuid, num := range masks {
		meta, err := getMetaByItem(metaClient, skuid)
		if err != nil {
			log.Println("[-] Sku [", skuid, "] Error :", err.Error())
			continue
		}
		meta.Num = num
		skuMetas[skuid] = meta
	}
	if len(skuMetas) == 0 {
		return false
	}
	return true
}
