package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// ListenMaskV2 - Listen Mask V2
type ListenMaskV2 struct {
	listenClient *http.Client
	skuNum       string
	listenURL    string
	req          *http.Request
}

func (lm *ListenMaskV2) initHTTPClient() {
	log.Println("[+] Init Http Client and Cookies")
	lm.listenClient = &http.Client{
		// Jar:       jar,
		Transport: tr,
	}
}

func (lm *ListenMaskV2) getSkuNumStr() {
	log.Println("[+] Make SkuNum")
	skuIds := []string{}
	for skuid := range skuMetas {
		skuIds = append(skuIds, skuid)
	}
	lm.skuNum = strings.Join(skuIds, ",")
}

func (lm *ListenMaskV2) genListenReq() {
	log.Println("[-] genListenReq")
	stockURL, err := url.Parse("https://c0.3.cn/stock")
	if err != nil {
		return
	}
	// Params
	params := url.Values{}
	params.Set("type", "getstocks")
	params.Set("skuIds", lm.skuNum)
	params.Set("area", area)
	params.Set("callback", getCallback())
	params.Set("_", getRandomTs())
	stockURL.RawQuery = params.Encode()
	lm.listenURL = stockURL.String()
	req, err := http.NewRequest("GET", lm.listenURL, nil)
	if err != nil {
		log.Println("[-]", err.Error())
		return
	}
	setReqHeader(req)
	req.Header.Set("Content-Type", "application/json")
	lm.req = req
}

func (lm *ListenMaskV2) httpGetForListen() {
	resp, err := lm.listenClient.Do(lm.req)
	if err != nil {
		log.Println("[-] HGFL::DoRequest", err.Error())
		return
	}
	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		log.Println("[-] HGFL::ReadBody", err.Error())
		return
	}
	if len(body) > 14 {
		body = body[14 : len(body)-1]
	} else {
		log.Println("[!] HGFL::Body :", string(body))
		go sendFeishuBotMsg("[!] HGFL::Body Error")
	}

	// a := gjson.Get(string(body), "venderId").String()
	// gjson.GetBytes(body,"")
	// gjson.ParseBytes()
	stocks := make(map[string]map[string]interface{}, 0)
	err = json.Unmarshal(body, &stocks)
	if err != nil {
		log.Println("[-] HGFL::UnmarshalBody", err.Error())
		log.Println("[-] listenURL", lm.listenURL)
		fmt.Println(string(body))
		return
	}
	for skuid := range stocks {
		// 已经执行 webhook || not onsell
		if !skuState[skuid] {
			continue
		}
		// StockState 库存状态编号 33 = 现货,39,40,36,34 = 无货
		// skuState 上柜状态编号 0 = 下柜, 1 = 上柜
		if stocks[skuid]["StockState"] == "33" && stocks[skuid]["skuState"] == "1" {
			skuState[skuid] = false
			// Webhook
			go http.Get(fmt.Sprintf("%s%s", config["webhook"], skuid))
			go func(skuid string) {
				msg := fmt.Sprintf("[ABML] Push [%s] To WebHook", skuid)
				log.Println("[*]", msg)
				sendFeishuBotMsg(msg)
			}(skuid)
			// 下单
			// TODO: 实现京东自动下单
			time.Sleep(1 * time.Second)
			// Reset State
			go func(skuid string) {
				time.Sleep(time.Duration(waittime) * time.Second)
				skuState[skuid] = true
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
		if i%(60*10) == 0 {
			go sendFeishuBotMsg("[ABML] I am listening...")
		}
		i++
		if i%60 == 0 {
			log.Println("[+] httpGetForListen ===== 第", i, "次 =====")
		}
		lm.httpGetForListen()
		time.Sleep(time.Duration(speed) * time.Second)
	}
	log.Println("[+] Stop Listener")
}

// RunListenMaskV2 - Run Listen Mask V2
func RunListenMaskV2() *ListenMaskV2 {
	lm := &ListenMaskV2{}
	lm.initHTTPClient()
	// go lm.listenMask()
	return lm
}
