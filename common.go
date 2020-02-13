package main

import (
	"bytes"
	"encoding/gob"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"math/rand"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/Unknwon/goconfig"
	"github.com/sirupsen/logrus"
	"golang.org/x/text/encoding/simplifiedchinese"
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
	// skuState   map[string]bool
	randSrc    rand.Source
	logger     *logrus.Logger
	tr         *http.Transport
	config     map[string]string
	skuMetas   map[string]*SkuMeta
	ch         chan os.Signal
	debug      bool    = false
	refresh    bool    = false
	verbose    bool    = false
	stop       bool    = false
	areaNext   string  = ""
	waittime   int     = 60
	speed      float64 = 1
	orderdelay int     = 30
	command    string
	justListen bool = false
)

// VkLogHook to send error logs via bot.
type VkLogHook struct{}

// Fire - VkLogHook::Fire
func (hook *VkLogHook) Fire(entry *logrus.Entry) error {
	line, err := entry.String()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Unable to read entry, %v", err)
		return err
	}
	if entry.Level == logrus.ErrorLevel {
		sendBotMsg(line)
	}
	return nil
}

// Levels - VkLogHook::Levels
func (hook *VkLogHook) Levels() []logrus.Level {
	return logrus.AllLevels
}

// NewVkLogHook - Create a hook to be added to an instance of logger
func NewVkLogHook() *VkLogHook {
	return &VkLogHook{}
}

func loadConf() (map[string]string, error) {
	config = make(map[string]string, 5)
	cfg, err := goconfig.LoadConfigFile("config.conf")
	if err != nil {
		logger.Errorln("[-] Load conf file 'config.conf' error ", err)
		return config, err
	}
	// core
	config["cookies"], err = cfg.GetValue("core", "cookies")
	if err != nil {
		logger.Warningln("[-] Load conf core.cookies error")
		config["cookies"] = ""
	}
	config["wxBotKey"], err = cfg.GetValue("core", "wxbotkey")
	if err != nil {
		config["wxBotKey"] = ""
	}
	config["fsBotKey"], err = cfg.GetValue("core", "fsbotkey")
	if err != nil {
		config["fsBotKey"] = ""
	}
	config["paymentpwd"], err = cfg.GetValue("core", "paymentpwd")
	if err != nil {
		config["paymentpwd"] = ""
	}
	config["area"], err = cfg.GetValue("core", "area")
	if err != nil {
		logger.Fatalln("[-] Load conf core.area error ", err)
		return config, err
	}
	config["webhook"], err = cfg.GetValue("core", "webhook")
	if err != nil {
		config["webhook"] = ""
	}
	config["waittime"], err = cfg.GetValue("core", "waittime")
	if err != nil {
		config["waittime"] = "60"
	}
	config["speed"], err = cfg.GetValue("core", "speed")
	if err != nil {
		config["speed"] = "1"
	}
	config["orderdelay"], err = cfg.GetValue("core", "orderdelay")
	if err != nil {
		config["orderdelay"] = "30"
	}
	config["command"], err = cfg.GetValue("core", "command")
	if err != nil {
		config["command"] = ""
	}
	areaNext = strings.Replace(config["area"], ",", "_", -1)
	waittime, err = strconv.Atoi(config["waittime"])
	if err != nil {
		waittime = 60
	}
	speed, err = strconv.ParseFloat(config["speed"], 64)
	if err != nil {
		speed = 1
	}
	orderdelay, err = strconv.Atoi(config["orderdelay"])
	if err != nil {
		orderdelay = 30
	}
	return config, nil
}

func httpPostJSON(sendURL string, sendData []byte) bool {
	client := &http.Client{Transport: tr}
	req, err := http.NewRequest("POST", sendURL, bytes.NewBuffer(sendData))
	if err != nil {
		logger.Errorln("[-] HPJ::NewRequest:", err)
		return false
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Charset", "UTF-8")
	resp, err := client.Do(req)
	if err != nil {
		logger.Errorln("[-] HPJ::DoRequest:", err)
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

func sendBotMsg(content string) {
	if len(config["fsBotKey"]) > 0 {
		sendFeishuBotMsg(content)
	}
	if len(config["wxBotKey"]) > 0 {
		sendWeChatBotMsg(content)
	}
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
		logger.Errorln("[-] LSMC::OpenFile", err.Error())
		return false
	}
	dec := gob.NewDecoder(file)
	skuMetas = make(map[string]*SkuMeta)
	err = dec.Decode(&skuMetas)
	if err != nil {
		logger.Errorln("[-] LSMC::Decode", err.Error())
		return false
	}
	logger.Infoln("[+] LSMC Success")
	return true
}

func saveSkuMetaCache() bool {
	file, err := os.Create(".skumeta")
	if err != nil {
		logger.Errorln("[-] SSMC::CreateFile", err.Error())
		return false
	}
	enc := gob.NewEncoder(file)
	err = enc.Encode(skuMetas)
	if err != nil {
		logger.Errorln("[-] SSMC::Enccode", err.Error())
		return false
	}
	logger.Infoln("[+] SSMC Success")
	return true
}

func loadMask() (map[string]int, error) {
	masks := make(map[string]int)
	f, err := ioutil.ReadFile("masks.json")
	if err != nil {
		logger.Fatalln("[-]", err.Error())
		return masks, err
	}
	err = json.Unmarshal(f, &masks)
	if err != nil {
		logger.Errorln("[-]", err.Error())
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
	logger.Infoln("[+] ItemURL :", itemURL)
	req, err := http.NewRequest("GET", itemURL, nil)
	if err != nil {
		logger.Errorln("[-] GMBI::NewRequest :", err.Error())
		return meta, err
	}
	setReqHeader(req)
	resp, err := metaClient.Do(req)
	if err != nil {
		logger.Errorln("[-] GMBI::DoRequest:: ", err.Error())
		return meta, err
	}
	defer resp.Body.Close()
	reader := simplifiedchinese.GB18030.NewDecoder().Reader(resp.Body)
	body, err := ioutil.ReadAll(reader)
	if err != nil {
		logger.Errorln("[-] GMBI::ReadBody :", err.Error())
		return meta, err
	}
	bodyStr := string(body)
	// Cat
	re = regexp.MustCompile(`(?m)cat:.\[(\d+),(\d+),(\d+)\]`)
	res = re.FindAllStringSubmatch(bodyStr, -1)
	if len(res) > 0 && len(res[0]) == 4 {
		meta.Cat = fmt.Sprintf("%s,%s,%s", res[0][1], res[0][2], res[0][3])
		logger.Infoln("[+] GMBI::Cat :", meta.Cat)
	}
	// VenderId
	re = regexp.MustCompile(`(?m)venderId:(\d+),`)
	res = re.FindAllStringSubmatch(bodyStr, -1)
	if len(res) > 0 && len(res[0]) == 2 {
		meta.VenderID = res[0][1]
		logger.Infoln("[+] GMBI::VenderID :", meta.VenderID)
	}
	// Name/Title
	re = regexp.MustCompile(`(?m)\<title\>(.*?)\<\/title\>`)
	res = re.FindAllStringSubmatch(bodyStr, -1)
	if len(res) > 0 && len(res[0]) == 2 {
		meta.Name = res[0][1]
		logger.Infoln("[+] GMBI::Name :", meta.Name)
	}
	// StockURL
	meta.StockURL = fmt.Sprintf("https://c0.3.cn/stock?skuId=%s&area=%s&venderId=%s&buyNum=1&cat=%s&callback=%s", skuid, areaNext, meta.VenderID, meta.Cat, getCallback())
	logger.Infoln("[+] GMBI::StockURL :", meta.StockURL)
	return meta, nil
}

func getSkuMeta(masks map[string]int) bool {
	metaClient := &http.Client{
		Transport: tr,
	}
	skuMetas = make(map[string]*SkuMeta)
	for skuid, num := range masks {
		meta, err := getMetaByItem(metaClient, skuid)
		if err != nil {
			logger.Errorln("[-] Sku [", skuid, "] Error :", err.Error())
			continue
		}
		meta.Num = num
		skuMetas[skuid] = meta
	}
	return len(skuMetas) != 0
}

func getCallbackBody(body []byte, msg string) []byte {
	if len(body) > 14 {
		body = body[14 : len(body)-1]
	} else {
		msg = fmt.Sprintf("[!] %s::GetCallbackBody Error", msg)
		logger.Errorln(msg, string(body))
		return nil
	}
	return body
}
