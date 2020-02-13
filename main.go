package main

import (
	"crypto/tls"
	"flag"
	"fmt"
	"math/rand"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/lizongshen/gocommand"
	"github.com/sirupsen/logrus"
)

func init() {
	// Logger
	vkLog := NewVkLogHook()
	logger = logrus.New()
	logger.SetOutput(os.Stdout)
	logger.AddHook(vkLog)

	tr = &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}
	randSrc = rand.NewSource(time.Now().UnixNano())

	// Flag Parse
	flag.BoolVar(&refresh, "r", false, "Refresh Cache")
	flag.BoolVar(&debug, "d", false, "Debug Mode")
	flag.BoolVar(&verbose, "v", false, "Verbose logging")
	flag.BoolVar(&justListen, "l", false, "Just Run Listener")
	flag.Parse()

	if debug {
		logger.SetLevel(logrus.DebugLevel)
	} else if verbose {
		logger.SetLevel(logrus.InfoLevel)
	} else {
		logger.SetLevel(logrus.ErrorLevel)
	}

	// Config
	config, err := loadConf()
	if err != nil {
		logger.Errorln("[-] ", err.Error())
		return
	}
	logger.Infoln("[+] Load Conf")
	for key, value := range config {
		logger.Infof("[+]   %-10s = %-.50s\n", key, value)
	}
	logger.Infof("[+]   %-10s = %-.50s\n", "areaNext", areaNext)
}

func main() {
	if debug {
		logger.Debug("[D] Start As Debug Mode")
		logger.Debug("[D] But Nothing...")
		// return
	}

	logger.Infoln("[+] Load SKU Meta Data...")
	// 获取 SKU 元数据
	if refresh {
		masks, err := loadMask()
		if err != nil {
			logger.Errorln("[-] ", err.Error())
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

	logger.Infoln("[+] Start to Listening...")
	go sendBotMsg("[ABML] Start to Listening...")

	wg := &sync.WaitGroup{}
	ch = make(chan os.Signal, 1)
	signal.Notify(ch, os.Interrupt, syscall.SIGTERM)

	var o *OrderJD
	if !justListen {
		o = RunOrderJD()
		wg.Add(1)
		go func(wg *sync.WaitGroup, o *OrderJD) {
			defer wg.Done()
			for {
				if stop {
					logger.Infoln("[+] Heartbeat stopping...")
					break
				}
				o.heartbeat()
				time.Sleep(60 * time.Second)
			}
			logger.Infoln("[+] Heartbeat stopped")
		}(wg, o)

		wg.Add(1)
		go func(wg *sync.WaitGroup, o *OrderJD) {
			defer wg.Done()
			o.orderSku()
		}(wg, o)
	}

	time.Sleep(1 * time.Second)

	// lm := RunListenMaskV1()
	lm := RunListenMaskV2()
	lm.doAction = func(skuid string) {
		// Auto Order
		if len(config["cookies"]) > 0 && !justListen {
			o.addSkuID(skuid)
		}
		go func(skuid string) {
			// Run Command
			if len(config["command"]) > 0 {
				_, _, err := gocommand.NewCommand().Exec(config["command"])
				if err != nil {
					logger.Errorln("[!] Command Error", err.Error())
				} else {
					logger.Infoln("[!] Command [", command, "] Exec success!")
				}
			}
			// Webhook
			if len(config["webhook"]) > 0 {
				_, _ = http.Get(fmt.Sprintf("%s%s", config["webhook"], skuid))
				msg := fmt.Sprintf("[ABML] Push [%s] To WebHook", skuid)
				logger.Infoln("[*]", msg)
				sendBotMsg(msg)
			}
		}(skuid)
	}

	wg.Add(1)
	go func(wg *sync.WaitGroup, lm *ListenMaskV2) {
		defer wg.Done()
		lm.listenMask()
	}(wg, lm)

	// Finish
	wg.Add(1)
	go func(wg *sync.WaitGroup, o *OrderJD) {
		defer wg.Done()
		<-ch
		if !justListen {
			o.addSkuID("0")
		}
		stop = true
	}(wg, o)

	wg.Wait()
	sendBotMsg("[ABML] Stoped Listen")
	logger.Infoln("[+] Finish AutoBuyMask")
}
