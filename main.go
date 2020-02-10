package main

import (
	"crypto/tls"
	"flag"
	"log"
	"math/rand"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"time"
)

func init() {
	tr = &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}
	randSrc = rand.NewSource(time.Now().UnixNano())

	// Flag Parse
	flag.BoolVar(&refresh, "r", false, "Refresh Cache")
	flag.Parse()

	// Config
	config, err := loadConf()
	if err != nil {
		log.Println("[-] Error :", err.Error())
		return
	}
	log.Println("[+] Load Conf")
	for key, value := range config {
		log.Println("[+]   ", key, "=", value)
	}
	log.Println("[+]   area2 =", area)
}

func main() {
	log.Println("[+] Load SKU Meta Data...")
	// 获取 SKU 元数据
	if refresh {
		masks, err := loadMask()
		if err != nil {
			log.Println("[-] Error :", err.Error())
			return
		}
		if !getSkuMeta(masks) {
			return
		}
		saveSkuMetaCache()
	} else {
		if !loadSkuMetaCache() {
			return
		}
	}

	log.Println("[+] Init skuState for Listener...")
	skuState = make(map[string]bool, len(skuMetas))
	for skuid := range skuMetas {
		skuState[skuid] = true
	}

	// Listen
	log.Println("[+] Listening...")
	go sendFeishuBotMsg("[ABML] Start to Listening")

	wg := &sync.WaitGroup{}
	ch = make(chan os.Signal, 1)
	signal.Notify(ch, os.Interrupt, os.Kill)

	wg.Add(1)
	go func(wg *sync.WaitGroup) {
		defer wg.Done()
		// #######################
		// #####  Run Task
		// #######################
		// lm := RunListenMaskV1()
		// lm.listenMask()

		lm := RunListenMaskV2()
		lm.listenMask()
		// #######################
		// #####  Run Task End
		// #######################
	}(wg)

	wg.Add(1)
	go func(wg *sync.WaitGroup) {
		defer wg.Done()
		select {
		case <-ch:
			stop = true
			break
		}
	}(wg)

	wg.Wait()
	sendFeishuBotMsg("[ABML] Stoped Listen")
	log.Println("[+] Finish AutoBuyMask")
}
