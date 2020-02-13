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
	listenClient  *http.Client
	skuNum        string
	listenURL     string
	reqStocks     *http.Request
	skuState      map[string]bool
	skuLimitState map[string]bool
	doAction      func(string)
	stocks        map[string]map[string]interface{}
	randomUA      string
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
	// Stocks
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
	lm.reqStocks, err = http.NewRequest("GET", lm.listenURL, nil)
	if err != nil {
		logger.Errorln("[-] GLR::NewRequest::Stocks", err.Error())
	}

}

func (lm *ListenMaskV2) listenStocks() {
	// lm.subListenMask()
	// defer wg.Done()
	lm.reqStocks.Header.Set("User-Agent", lm.randomUA)
	resp, err := lm.listenClient.Do(lm.reqStocks)
	if err != nil {
		logger.Errorln("[-] LS::DoRequest", err.Error())
		return
	}
	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		logger.Errorln("[-] LS::ReadBody", err.Error())
		return
	}
	body = getCallbackBody(body, "LS")
	lm.stocks = make(map[string]map[string]interface{})
	err = json.Unmarshal(body, &lm.stocks)
	if err != nil {
		logger.Errorln("[-] LS::UnmarshalBody", err.Error())
		logger.Warnln(string(body))
	}
}
func (lm *ListenMaskV2) listenLimit() {
	data := url.Values{}
	data.Set("skus", lm.skuNum)
	reqLimit, err := http.NewRequest("POST", "https://cart.jd.com/getLimitInfo.action", strings.NewReader(data.Encode()))
	if err != nil {
		logger.Errorln("[-] GLR::NewRequest::Listen", err.Error())
	}
	reqLimit.Header.Set("User-Agent", lm.randomUA)
	reqLimit.Header.Add("Content-Type", "application/x-www-form-urlencoded")
	resp, err := lm.listenClient.Do(reqLimit)
	if err != nil {
		logger.Errorln("[-] LL::DoRequest", err.Error())
		return
	}
	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		logger.Errorln("[-] LL::ReadBody", err.Error())
		return
	}
	result := make(map[string]interface{})
	err = json.Unmarshal(body, &result)
	if err != nil {
		logger.Errorln("[-] LL::UnmarshalBody", err.Error())
		logger.Infoln(string(body))
		return
	}
	if result["limitResult"] == nil {
		fmt.Println(result)
		return
	}
	limit := result["limitResult"].(string)
	limitMsg := make([]string, 0)
	for skuid := range lm.skuState {
		if strings.Contains(limit, skuid) {
			lm.skuLimitState[skuid] = false
			limitMsg = append(limitMsg, skuid)
		} else {
			lm.skuLimitState[skuid] = true
		}
	}
	logger.Infoln("[!] Limit:", strings.Join(limitMsg, ","))
}

func (lm *ListenMaskV2) subListenMask() {
	for skuid, stock := range lm.stocks {
		// Not onsell or already doAction
		if !lm.skuState[skuid] || !lm.skuLimitState[skuid] {
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
		if i%50 == 0 || i == 0 {
			logger.Infoln("[+] httpGetForListen ===== 第", i, "次 =====")
			lm.listenLimit()
			time.Sleep(1 * time.Second)
		}
		i++
		lm.randomUA = browser.Random()
		lm.listenStocks()
		lm.subListenMask()
		time.Sleep(time.Duration(speed) * time.Second)
	}
	logger.Infoln("[+] Stop Listener")
}

// RunListenMaskV2 - Run Listen Mask V2
func RunListenMaskV2() *ListenMaskV2 {
	lm := &ListenMaskV2{}
	lm.initHTTPClient()
	lm.skuState = make(map[string]bool, len(skuMetas))
	lm.skuLimitState = make(map[string]bool, len(skuMetas))
	for skuid := range skuMetas {
		lm.skuState[skuid] = true
		lm.skuLimitState[skuid] = false
	}
	return lm
}
