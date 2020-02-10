## AutoBuyMask

## V2

查询接口 [https://c0.3.cn/stocks](https://c0.3.cn/stocks?callback=jQuery2913750&type=getstocks&skuIds=851157,1938795,45923412989,45006657879,46443156559&area=1_72_2799_0&_=1581182713693)

## Usage

只说 V2 的

获取元数据并监控 `./autobuymask -r` **一定要先获取元数据**
利用缓存开始监控 `./autobuymask`

### 修改配置文件

自己改名

**config.cconf**

```ini
[core]
; https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=xxxxxxxxx
wxbotkey=企业微信机器人 Key
; https://open.feishu.cn/open-apis/bot/hook/xxxxxxxxxxxx
fsbotkey=飞书机器人 Key
area=1,72,2799,0
; 有货信息推送
; webhook=http://xxxx/webhook/
; eg. http://xxxx/webhook/[skuid] - http://xxxx/webhook/51137726169
; eg. http://xxxx/webhook/?id=[skuid] - http://xxxx/webhook/?id=51137726169
webhook=
; V2 暂时用不到 -.-
cookies=
; 成功查询到商品信息，推送间隔时间。(单位 秒)
waittime=30
; 监控间隔时间。(单位 秒)
speed=0.2
```

**masks.json**

key = skuid
value = num (下单数量，暂时没啥用)

```json
{
  "skuid1":1,
  "skuid2":1,
}
```