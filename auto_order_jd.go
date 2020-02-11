package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/tidwall/gjson"
	"golang.org/x/text/encoding/simplifiedchinese"
)

// JDOrderDetail - JD Order Detail
type JDOrderDetail struct {
	Address    string   `form:"address"`
	Receiver   string   `form:"receiver"`
	TotalPrice string   `form:"total_price"`
	Items      []string `form:"items"`
}

// JDCartItem - JD Cart Item Detail
type JDCartItem struct {
	SkuID      string `form:"skuid"`
	Name       string `form:"name"`
	VenderID   string `form:"verder_id"`
	Count      string `form:"count"`
	UnitPrice  string `form:"unit_price"`
	TotalPrice string `form:"total_price"`
	IsSelected bool   `form:"is_selected"`
	PType      string `form:"p_type"`
	TargetID   string `form:"target_id"`
	PromoID    string `form:"promo_id"`
}

// OrderJD - Order JD
type OrderJD struct {
	orderClient  *http.Client
	heartbeatReq *http.Request
	skuChan      chan string
	skuState     map[string]bool
	riskControl  string
}

func (o *OrderJD) initHTTPClient() {
	logger.Infoln("[+] [JD] Init Http Client and Cookies")
	cookies := []*http.Cookie{}
	for _, c := range strings.Split(config["cookies"], ";") {
		kv := strings.Split(strings.Trim(c, " "), "=")
		cookie := &http.Cookie{Name: kv[0], Value: kv[1], Path: "/", MaxAge: 86400}
		cookies = append(cookies, cookie)
	}
	jar, _ := cookiejar.New(nil)
	// passport
	u, _ := url.Parse("https://passport.jd.com")
	jar.SetCookies(u, cookies)
	// order
	// u, _ := url.Parse("https://jd.com")
	// jar.SetCookies(u, cookies)
	o.orderClient = &http.Client{
		Jar:       jar,
		Transport: tr,
	}
}

func (o *OrderJD) heartbeat() {
	heartbeatURL := fmt.Sprintf("https://passport.jd.com/new/helloService.ashx?callback=%s&_=%s", getCallback(), getRandomTs())
	req, err := http.NewRequest("GET", heartbeatURL, nil)
	if err != nil {
		logger.Errorln("[-] HB::NewRequest:", err.Error())
		return
	}
	setReqHeader(req)
	req.Header.Set("Referer", "https://order.jd.com/center/list.action")
	resp, err := o.orderClient.Do(req)
	if err != nil {
		logger.Errorln("[-] HB::DoRequest", err.Error())
		return
	}
	defer resp.Body.Close()
	reader := simplifiedchinese.GB18030.NewDecoder().Reader(resp.Body)
	body, err := ioutil.ReadAll(reader)
	if err != nil {
		logger.Errorln("[-] HB::ReadBody", err.Error())
		return
	}
	jsonObj := make(map[string]interface{}, 0)
	err = json.Unmarshal(getCallbackBody(body, "HB"), &jsonObj)
	if err != nil {
		logger.Errorln("[-] HB::UnmarshalBody", err.Error())
		logger.Errorln("[-] HeartbeatURL", heartbeatURL)
		logger.Debug(string(body))
		return
	}
	logger.Infof("[+] User [ %s ] is alive!\n", jsonObj["nick"])
}

func (o *OrderJD) cancelAllItem() bool {
	cancleURL := "https://cart.jd.com/cancelAllItem.action"
	data := url.Values{}
	data.Set("t", "0")
	data.Set("outSkus", "")
	data.Set("random", getRandomTs())
	req, err := http.NewRequest("POST", cancleURL, strings.NewReader(data.Encode()))
	if err != nil {
		logger.Errorln("[-] CAI::NewRequest:", err)
		return false
	}
	setReqHeader(req)
	resp, err := o.orderClient.Do(req)
	if err != nil {
		logger.Errorln("[-] CAI::DoRequest", err)
		return false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return false
	}
	logger.Debug("[D] CancelAllItem success")
	return true
}

func strToInt(s string) int {
	i, err := strconv.Atoi(s)
	if err != nil {
		return 0
	}
	return i
}

func getTagValue(v string) string {
	return strings.Trim(v, " \t\r\n")
}

func (o *OrderJD) getCartDetail() (map[string]*JDCartItem, error) {
	cartItems := make(map[string]*JDCartItem, 0)
	cartURL := "https://cart.jd.com/cart.action"
	req, err := http.NewRequest("GET", cartURL, nil)
	if err != nil {
		logger.Errorln("[-] GCD::NewRequest:", err)
		return cartItems, err
	}
	setReqHeader(req)
	req.Header.Set("Referer", "https://order.jd.com/center/list.action")
	resp, err := o.orderClient.Do(req)
	if err != nil {
		logger.Errorln("[-] GCD::DoRequest", err)
		return cartItems, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		logger.Errorln("[-] GCD::Respon ", resp.StatusCode, resp.Status)
		return cartItems, err
	}
	reader := simplifiedchinese.GB18030.NewDecoder().Reader(resp.Body)
	doc, err := goquery.NewDocumentFromReader(reader)
	if err != nil {
		logger.Errorln("[-] GCD::NewDocument ", err.Error())
		return cartItems, err
	}
	doc.Find(".item-item.item-single").Each(func(i int, item *goquery.Selection) {
		skuid, exists := item.Attr("skuid")
		if !exists {
			logger.Errorln("[!] Not Found skuid on item")
			return
		}
		incrementItemID, exists := item.Find(".increment").Attr("id")
		if !exists {
			logger.Errorln("[!] Not Found increment in item")
			return
		}
		itemAttrList := strings.Split(incrementItemID, "_")
		// ["increment","8888","7498167","1","1","0"]
		// ["increment","8888","59959871348","1","11","0","33739343507"]
		var promoID = "0"
		if len(itemAttrList) == 7 {
			promoID = itemAttrList[6]
		}
		jdci := &JDCartItem{
			SkuID:      skuid,
			Name:       getTagValue(item.Find("div.p-name a").Text()),
			VenderID:   item.AttrOr("venderid", "9999"),
			Count:      item.AttrOr("num", "99999"),
			UnitPrice:  getTagValue(item.Find("div.p-price strong").Text())[1:],
			TotalPrice: getTagValue(item.Find("div.p-sum strong").Text())[1:],
			PType:      itemAttrList[4],
			PromoID:    promoID,
			TargetID:   promoID,
			IsSelected: false,
		}
		cartItems[skuid] = jdci
	})
	if len(cartItems) == 0 {
		logger.Debug("[D] CetCartDetail::Doc", doc)
		return cartItems, errors.New("Cart Is Empty")
	}
	logger.Debug("[D] CetCartDetail success")
	return cartItems, nil
}

func (o *OrderJD) changeItemNumInCart(num int, item *JDCartItem) bool {
	changeURL := "https://cart.jd.com/changeNum.action"
	var r http.Request
	r.ParseForm()
	r.Form.Add("t", "0")
	r.Form.Add("outSkus", "")
	r.Form.Add("random", getRandomTs())
	r.Form.Add("venderId", item.VenderID)
	r.Form.Add("pid", item.SkuID)
	r.Form.Add("pcount", item.Count)
	r.Form.Add("ptype", item.PType)
	r.Form.Add("targetId", item.TargetID)
	r.Form.Add("promoID", item.PromoID)
	bodystr := strings.TrimSpace(r.Form.Encode())
	req, err := http.NewRequest("POST", changeURL, strings.NewReader(bodystr))
	if err != nil {
		logger.Errorln("[-] CINIC::NewRequest:", err.Error())
		return false
	}
	setReqHeader(req)
	req.Header.Add("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Referer", "https://cart.jd.com/cart")
	resp, err := o.orderClient.Do(req)
	if err != nil {
		logger.Errorln("[-] CINIC::DoRequest", err)
		return false
	}
	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		logger.Errorln("[-] CINIC::ReadBody", err.Error())
		return false
	}
	// sortedWebCartResult.ids 选择状态的 SKUID 列表
	if gjson.GetBytes(body, "sortedWebCartResult.ids").String() == item.SkuID {
		logger.Debug("[D] changeItemNumInCart success")
		return true
	}
	logger.Debug("[D] CINIC::Body", string(body))
	return false
}

func (o *OrderJD) addItemToCart(skuID string, num int) bool {
	_gateURL := "https://cart.jd.com/gate.action"
	gateURL := fmt.Sprintf("%s?pid=%s&pcount=%d&ptype=1", _gateURL, skuID, num)
	req, err := http.NewRequest("GET", gateURL, nil)
	if err != nil {
		logger.Errorln("[-] AITC::NewRequest:", err.Error())
		return false
	}
	setReqHeader(req)
	req.Header.Set("Referer", "https://cart.jd.com/cart.action")
	resp, err := o.orderClient.Do(req)
	if err != nil {
		logger.Errorln("[-] AITC::DoRequest", err.Error())
		return false
	}
	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		logger.Errorln("[-] GCPD::ReadBody", err.Error())
		return false
	}

	if strings.Contains(string(body), "商品已成功加入购物车") {
		logger.Debug("[D] AddItemToCart success")
		return true
	}

	l, err := resp.Location()
	if err != nil {
		logger.Errorln("[-] AITC::Location", err.Error())
		return false
	}
	if strings.Contains(l.String(), "https://cart.jd.com/addToCart.html") ||
		strings.Contains(l.String(), "https://cart.jd.com/cart.action") {
		logger.Infof("[+] [%s] add to cart success!", skuID)
		logger.Debug("[D] AddItemToCart success")
		return true
	}
	logger.Errorln("[-] AITC::Location=", l.String())
	return false
}

// 获取订单结算页面信息
// 该方法会返回订单结算页面的详细信息：商品名称、价格、数量、库存状态等。
func (o *OrderJD) getCheckoutPageDetail() error {
	tradeURL := fmt.Sprintf("http://trade.jd.com/shopping/order/getOrderInfo.action?rid=%s", getRandomTs())
	req, err := http.NewRequest("GET", tradeURL, nil)
	if err != nil {
		logger.Errorln("[-] GCPD::NewRequest:", err.Error())
		return err
	}
	setReqHeader(req)
	req.Header.Set("Referer", "https://cart.jd.com/cart.action")
	resp, err := o.orderClient.Do(req)
	if err != nil {
		logger.Errorln("[-] GCPD::DoRequest", err.Error())
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		err = errors.New("Get Order Info Error")
		logger.Errorln("[-] GCPD::DoRequest", err.Error())
		return err
	}
	// reader := simplifiedchinese.GB18030.NewDecoder().Reader(resp.Body)
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		logger.Errorln("[-] GCPD::ReadBody", err.Error())
		return err
	}
	bodyStr := string(body)
	if strings.Contains(bodyStr, "刷新太频繁了") {
		err = errors.New("刷新太频繁了")
		return err
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(bodyStr))
	if err != nil {
		logger.Errorln("[-] GCPD::NewDocument ", err.Error())
		return err
	}
	if strings.Contains(doc.Find("span#p-state").Text(), "无货") {
		err = errors.New("Sku is gone")
		return err
	}
	o.riskControl = doc.Find("#riskControl").AttrOr("value", "")
	if verbose || debug {
		orderDetail := &JDOrderDetail{
			Address:    doc.Find("span#sendAddr").Text(),      // remove '寄送至： ' from the begin
			Receiver:   doc.Find("span#sendMobile").Text(),    // remove '收件人:' from the begin
			TotalPrice: doc.Find("span#sumPayPriceId").Text(), // remove '￥' from the begin
			Items:      []string{},
		}
		logger.Infoln("[+] Order Detail:", orderDetail)
	}
	if o.riskControl == "" {
		return errors.New("RiskControl has not got")
	}
	logger.Debug("[D] GetCheckoutPageDetail success")
	return nil
}

func encryptPaymentPwd(pwd string) string {
	res := ""
	n := len(pwd)
	for i := 0; i < n; i++ {
		res += fmt.Sprintf("u3%c", pwd[i])
	}
	return res
}

// 提交订单
// 1.该方法只适用于普通商品的提交订单
// 2.提交订单时，会对购物车中勾选✓的商品进行结算
func (o *OrderJD) submitOrder() bool {
	submitURL := "https://trade.jd.com/shopping/order/submitOrder.action"
	data := url.Values{}
	// 预售商品订单
	// ....
	// submitOrderParam.eid: GX6IM4NTJ6FH5PSTNW6A3DSYGTXQSGTX6T4GMSSFUERD7Y4PR2KXHIRDSZACKONK22D3LLNJBHNZ4JEIFYYT5Q3X6Q
	// submitOrderParam.fp: 19e893513f69af8bfceaa200d0ea2f74
	// 普通商品订单
	data.Set("overseaPurchaseCookies", "")
	data.Set("vendorRemarks", "[]")
	data.Set("submitOrderParam.sopNotPutInvoice", "false")
	data.Set("submitOrderParam.trackID", "TestTrackId")
	data.Set("submitOrderParam.ignorePriceChange", "0")
	// 支付密码
	if len(config["paymentpwd"]) > 0 {
		data.Set("submitOrderParam.payPassword", encryptPaymentPwd(config["paymentpwd"]))
	}
	// jxj trackId 风控相关 order.js
	data.Set("submitOrderParam.jxj", "1")
	data.Set("submitOrderParam.trackId", "2f69d65eadf9e6b15701eb5875f4b8e9")
	data.Set("riskControl", o.riskControl) // 就是是 csrf 的东西
	req, err := http.NewRequest("POST", submitURL, strings.NewReader(data.Encode()))
	if err != nil {
		logger.Errorln("[-] SO::NewRequest:", err)
		return false
	}
	setReqHeader(req)
	req.Header.Set("Referer", "http://trade.jd.com/shopping/order/getOrderInfo.action")
	resp, err := o.orderClient.Do(req)
	if err != nil {
		logger.Errorln("[-] SO::DoRequest", err)
		return false
	}
	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		logger.Errorln("[-] SO::ReadBody", err.Error())
		return false
	}
	obj := gjson.GetManyBytes(body, "success", "orderId", "message", "resultCode")
	if obj[0].Bool() {
		logger.Infoln("[+] Order Success! OrderNo:", obj[1])
		return true
	}
	code := obj[3].Int()
	msg := obj[2].String()
	if code == 0 {
		msg += "(下单商品可能为第三方商品，将切换为普通发票进行尝试)"
	} else if code == 60077 {
		msg += "(可能是购物车为空 或 未勾选购物车中商品)"
	} else if code == 60123 {
		msg += "(需要在 config.conf 文件中配置支付密码)"
	}
	logger.Debug("[D] SubmitOrder", string(body))
	return false
}

func (o *OrderJD) doOrder(skuID string) {
	logger.Infoln("[+] Do Order [", skuID, "]")
	i := 1
	for {
		if i == 2 {
			break
		}
		logger.Infof("[+] ===== 第 [ %d / %d ] 次尝试提交订单 =====\n", i, 3)
		msg := fmt.Sprintf("[!!!] [%d/3 submit] [%s] Order ", i, skuID)
		if !o.cancelAllItem() {
			logger.Errorln("[-] DoOrder : Cannot cancel all item")
		}
		cartItems, err := o.getCartDetail()
		if err != nil {
			logger.Errorln("[-] DoOrder : ", err.Error())
		}
		ret := false
		if item, ok := cartItems[skuID]; ok {
			logger.Debug("changeItemNumInCart")
			ret = o.changeItemNumInCart(1, item)
		} else {
			logger.Debug("addItemToCart")
			ret = o.addItemToCart(skuID, 1)
		}
		if ret {
			err := o.getCheckoutPageDetail()
			if err == nil {
				if o.submitOrder() {
					msg += "Success!"
					go sendBotMsg(msg)
				} else {
					msg += "Fail!"
				}
			} else {
				msg += "Fail!"
				logger.Errorln("[-] DoOrder :", err.Error())
			}
		} else {
			msg += "Fail!"
		}
		logger.Infoln(msg)
		time.Sleep(5 * time.Second)
		i++
	}
}

func (o *OrderJD) orderSku() {
	w := &sync.WaitGroup{}
	for {
		logger.Infoln("[+] [JD] Waiting OrderSku chan...")
		select {
		case skuID := <-o.skuChan:
			logger.Infoln("[+] skuID := <-o.skuChan:", skuID)
			if skuID == "0" {
				goto Loop
			}
			if o.skuState[skuID] {
				continue
			}
			logger.Infoln("[+] Order [", skuID, "]")
			o.skuState[skuID] = true
			w.Add(1)
			go func() {
				defer w.Done()
				o.doOrder(skuID)
			}()
			// Reset
			w.Add(1)
			go func() {
				defer w.Done()
				time.Sleep(time.Duration(orderdelay) * time.Second)
				o.skuState[skuID] = false
			}()
			break
		}
	}
Loop:
	w.Wait()
	logger.Infoln("[+] Finish orderSku")
}

func (o *OrderJD) addSkuID(skuID string) {
	logger.Infoln("[+] [JD] Add Order ", skuID)
	o.skuChan <- skuID
	return
}

// RunOrderJD - Run Order Sku
func RunOrderJD() *OrderJD {
	o := &OrderJD{}
	o.initHTTPClient()
	o.skuChan = make(chan string, 1)
	o.skuState = make(map[string]bool, len(skuMetas))
	for skuid := range skuMetas {
		o.skuState[skuid] = false
	}
	return o
}
