package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"time"

	"github.com/tidwall/gjson"
)

// ListenMaskV1 - Listen Mask V1
type ListenMaskV1 struct {
	listenClient *http.Client
	skuNum       string
	skuState     map[string]bool
}

func (lm *ListenMaskV1) httpGetForListen() {
	stockStateURL, err := url.Parse("https://fts.jd.com/areaStockState/mget")
	if err != nil {
		return
	}
	// Params
	params := url.Values{}
	params.Set("ch", "1")
	params.Set("skuNum", lm.skuNum)
	params.Set("coordnate", "")
	params.Set("area", config["area"])
	params.Set("callback", getCallback())
	params.Set("_", getRandomTs())
	stockStateURL.RawQuery = params.Encode()
	stockURL := stockStateURL.String()
	req, err := http.NewRequest("GET", stockURL, nil)
	if err != nil {
		logger.Errorln("[-]", err.Error())
		return
	}
	setReqHeader(req)
	req.Header.Set("Referer", "https://cart.jd.com/cart.action?r=0.9760129766115194")
	resp, err := lm.listenClient.Do(req)
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
	state := make(map[string]map[string]interface{})
	err = json.Unmarshal(body, &state)
	if err != nil {
		logger.Errorln("[-] HGFL::UnmarshalBody", err.Error())
		logger.Errorln("[-] StockURL", stockURL)
		fmt.Println(string(body))
		return
	}
	for skuid := range state {
		// 已经执行 webhook || not onsell
		if !lm.skuState[skuid] {
			continue
		}
		// 库存状态编号 33,39,40,36,34
		// 33 = 现货
		// 34 = 无货
		if state[skuid]["a"] == "33" {
			// 是否上架
			if ok, err := lm.isSkuOnSell(skuid); err != nil || !ok {
				// logger.Errorln("[-] Sku [", skuid, "] is not onsell")
				lm.skuState[skuid] = false
				continue
			}
			// Webhook
			lm.skuState[skuid] = false

			// time.Sleep(2 * time.Second)
			go func(skuid string) {
				if len(config["webhook"]) > 0 {
					_, err = http.Get(fmt.Sprintf("%s%s_%d", config["webhook"], skuid, skuMetas[skuid].Num))
					msg := fmt.Sprintf("[ABML] Push [%s] To WebHook", skuid)
					logger.Infoln("[*]", msg)
					sendBotMsg(msg)
				}
			}(skuid)

			// Reset State
			go func(skuid string) {
				time.Sleep(time.Duration(waittime) * time.Second)
				lm.skuState[skuid] = true
			}(skuid)
		}
	}
}

func (lm *ListenMaskV1) isSkuOnSell(skuid string) (bool, error) {
	state := false
	req, err := http.NewRequest("GET", skuMetas[skuid].StockURL, nil)
	if err != nil {
		logger.Errorln("[-] ISOS::NewRequest :", err.Error())
		return state, err
	}
	setReqHeader(req)
	resp, err := lm.listenClient.Do(req)
	if err != nil {
		logger.Errorln("[-] ISOS::DoRequest: ", err.Error())
		return state, err
	}
	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		logger.Errorln("[-] GetVenderID::ReadBody :", err.Error())
		return state, err
	}
	body = getCallbackBody(body, "ISOS")
	stockSkuState := gjson.Get(string(body), "stock.skuState").Int()
	state = stockSkuState == 1
	return state, nil
}

func (lm *ListenMaskV1) initHTTPClient() {
	logger.Infoln("[+] Init Http Client and Cookies")
	// HTTP Client and Cookie
	cookies := []*http.Cookie{}
	for _, c := range strings.Split(config["cookies"], ";") {
		kv := strings.Split(strings.Trim(c, " "), "=")
		cookie := &http.Cookie{Name: kv[0], Value: kv[1], Path: "/", MaxAge: 86400}
		cookies = append(cookies, cookie)
	}
	u, _ := url.Parse("https://fts.jd.com")
	jar, _ := cookiejar.New(nil)
	jar.SetCookies(u, cookies)
	lm.listenClient = &http.Client{
		Jar:       jar,
		Transport: tr,
	}
}

func (lm *ListenMaskV1) getSkuNumStr() {
	logger.Infoln("[+] Make SkuNum")
	skuNum := ""
	for skuid, meta := range skuMetas {
		skuNum += fmt.Sprintf("%s,%d;", skuid, meta.Num)
	}
	lm.skuNum = skuNum
}

func (lm *ListenMaskV1) listenMask() {
	lm.initHTTPClient()
	lm.getSkuNumStr()
	i := 0
	for {
		if stop {
			break
		}
		if i%(60*5) == 0 {
			go sendBotMsg(fmt.Sprintf("[ABML] I am listening... %d", i))
		}
		i++
		if i%60 == 0 {
			logger.Infoln("[+] ========== 第", i, "次 ==========")
		}
		lm.httpGetForListen()
		time.Sleep(time.Duration(speed) * time.Second)
	}
}
