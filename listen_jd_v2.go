package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"strings"
	"time"

	browser "github.com/EDDYCJY/fake-useragent"
)

// ListenMaskV2 - Listen Mask V2
type ListenMaskV2 struct {
	listenClient *http.Client
	skuNum       string
	listenURL    string
	req          *http.Request
	skuState     map[string]bool
	doAction     func(string)
}

func (lm *ListenMaskV2) initHTTPClient() {
	logger.Infoln("[+] [V2] Init Http Client and Cookies")
	lm.listenClient = &http.Client{
		Transport: tr,
	}
}

func (lm *ListenMaskV2) getSkuNumStr() {
	logger.Infoln("[+] [V2] Make SkuNum")
	skuIds := []string{}
	for skuid := range skuMetas {
		skuIds = append(skuIds, skuid)
	}
	lm.skuNum = strings.Join(skuIds, ",")
}

func (lm *ListenMaskV2) genListenReq() {
	stockURL, err := url.Parse("https://c0.3.cn/stocks")
	if err != nil {
		return
	}
	params := url.Values{}
	params.Set("type", "getstocks")
	params.Set("skuIds", lm.skuNum)
	params.Set("area", areaNext)
	params.Set("callback", getCallback())
	params.Set("_", getRandomTs())
	stockURL.RawQuery = params.Encode()
	lm.listenURL = stockURL.String()
	logger.Infoln("[+] StockURL", lm.listenURL)
	req, err := http.NewRequest("GET", lm.listenURL, nil)
	if err != nil {
		logger.Errorln("[-]", err.Error())
		return
	}
	lm.req = req
}

func (lm *ListenMaskV2) httpGetForListen() {
	randomUA := browser.Random()
	lm.req.Header.Set("User-Agent", randomUA)
	resp, err := lm.listenClient.Do(lm.req)
	if err != nil {
		logger.Errorln("[-] HGFL::DoRequest", err.Error())
		return
	}
	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		logger.Errorln("[-] HGFL::ReadBody", err.Error())
		return
	}
	body = getCallbackBody(body, "HGFL")
	stocks := make(map[string]map[string]interface{}, 0)
	err = json.Unmarshal(body, &stocks)
	if err != nil {
		logger.Errorln("[-] HGFL::UnmarshalBody", err.Error())
		logger.Warnln("[-] listenURL", lm.listenURL)
		logger.Warnln(string(body))
		return
	}
	for skuid, stock := range stocks {
		// Not onsell or already doAction
		if !lm.skuState[skuid] {
			continue
		}
		// StockState 库存状态编号 33 = 现货,39,40,36,34 = 无货
		// skuState 上柜状态编号 0 = 下柜, 1 = 上柜
		if stock["StockState"].(float64) == 33 && stock["skuState"].(float64) == 1 {
			lm.skuState[skuid] = false
			// Notice
			go func(skuid string) {
				msg := fmt.Sprintf("[ABML] Congratulation!!! https://item.jd.com/%s.html 有货!", skuid)
				sendBotMsg(msg)
				logger.Infoln("[+]", msg)
				logger.Infoln("[+]", msg)
				logger.Infoln("[+]", msg)
			}(skuid)
			// 下单相关
			lm.doAction(skuid)
			time.Sleep(1 * time.Second)
			// Reset State
			go func(skuid string) {
				time.Sleep(time.Duration(waittime) * time.Second)
				lm.skuState[skuid] = true
			}(skuid)
		}
	}
}

func (lm *ListenMaskV2) listenMask() {
	lm.getSkuNumStr()
	lm.genListenReq()
	i := 0
	for {
		if stop {
			break
		}
		if i%(60*100) == 0 {
			go sendBotMsg("[ABML] I am listening...")
		}
		i++
		if i%30 == 0 {
			logger.Infoln("[+] httpGetForListen ===== 第", i, "次 =====")
		}
		// logger.Debugln("[+] httpGetForListen ===== 第", i, "次 =====")
		lm.httpGetForListen()
		time.Sleep(time.Duration(speed) * time.Second)
	}
	logger.Infoln("[+] Stop Listener")
}

// RunListenMaskV2 - Run Listen Mask V2
func RunListenMaskV2() *ListenMaskV2 {
	lm := &ListenMaskV2{}
	lm.initHTTPClient()
	lm.skuState = make(map[string]bool, len(skuMetas))
	for skuid := range skuMetas {
		lm.skuState[skuid] = true
	}
	return lm
}
