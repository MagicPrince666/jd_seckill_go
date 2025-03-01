package main

import (
	"errors"
	"fmt"
	"jd_seckill_go/common"
	"jd_seckill_go/conf"
	"jd_seckill_go/jd_seckill"
	"log"
	"net/http"
	"os"
	"runtime"
	"sync"
	"time"

	"github.com/Albert-Zhan/httpc"
	"github.com/tidwall/gjson"
)

var client *httpc.HttpClient

var cookieJar *httpc.CookieJar

var config *conf.Config

var wg *sync.WaitGroup

func init() {
	//客户端设置初始化
	client = httpc.NewHttpClient()
	cookieJar = httpc.NewCookieJar()
	client.SetCookieJar(cookieJar)
	client.SetRedirect(func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	})
	//配置文件初始化
	confFile := "./conf.ini"
	if !common.Exists(confFile) {
		log.Println("配置文件不存在，程序退出")
		os.Exit(0)
	}
	config = &conf.Config{}
	config.InitConfig(confFile)

	wg = new(sync.WaitGroup)
	wg.Add(1)
}

func main() {
	runtime.GOMAXPROCS(runtime.NumCPU())
	getJdTime()

	//用户登录
	user := jd_seckill.NewUser(client, config)
	wlfstkSmdl, err := user.QrLogin()
	if err != nil {
		os.Exit(0)
	}
	ticket := ""
	for {
		ticket, err = user.QrcodeTicket(wlfstkSmdl)
		if err == nil && ticket != "" {
			break
		}
		time.Sleep(2 * time.Second)
	}
	_, err = user.TicketInfo(ticket)
	if err == nil {
		log.Println("登录成功")
		//刷新用户状态和获取用户信息
		if status := user.RefreshStatus(); status == nil {
			for {
				userInfo, _ := user.GetUserInfo()
				log.Println("用户:" + userInfo)
				//开始预约,预约过的就重复预约
				seckill := jd_seckill.NewSeckill(client, config)
				seckill.MakeReserve()
				//等待抢购/开始抢购
				nowLocalTime := time.Now().UnixNano() / 1e6
				jdTime, _ := getJdTime()
				buyDate := config.Read("config", "buy_time")
				loc, _ := time.LoadLocation("Local")
				t, _ := time.ParseInLocation("2006-01-02 15:04:05", buyDate, loc)
				buyTime := t.UnixNano() / 1e6
				diffTime := nowLocalTime - jdTime
				if buyTime-nowLocalTime < 0 {
					log.Println("设定时间已过,重新设置")
					dateDiff := (nowLocalTime-buyTime)/(24*3600*1000) + 1
					log.Println(fmt.Sprintf("相差天数为【%d】天", dateDiff))
					buyTime += dateDiff * 24 * 3600 * 1000
					buyDate = time.Unix(buyTime/1000, 0).Format("2006-01-02 15:04:05")
				}
				if jdTime == 0 {
					diffTime = 50
				}
				log.Println(fmt.Sprintf("正在等待到达设定时间:%s，检测本地时间与京东服务器时间误差为【%d】毫秒", buyDate, diffTime))
				timerTime := buyTime - diffTime
				if timerTime <= 0 {
					log.Println("请设置抢购时间")
					os.Exit(0)
				}
				log.Println(fmt.Sprintf("等待时间: %d毫秒", time.Duration(timerTime/36000)*time.Millisecond))
				time.Sleep(time.Duration(timerTime/36000) * time.Millisecond)
				//开启任务
				log.Println("时间到达，开始执行……")
				start(seckill, 5)
				wg.Wait()
			}
		}
	} else {
		log.Println("登录失败")
	}
}

func getJdTime() (int64, error) {
	req := httpc.NewRequest(client)
	req.SetHeader("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/88.0.4324.182 Safari/537.36")
	resp, body, err := req.SetUrl("https://api.m.jd.com/client.action?functionId=queryMaterialProducts&client=wh5").SetMethod("get").Send().End()
	if err != nil || resp.StatusCode != http.StatusOK {
		log.Println("获取京东服务器时间失败")
		return 0, errors.New("获取京东服务器时间失败")
	}
	jdtime := gjson.Get(body, "currentTime2").Int()
	return jdtime, nil
}

func start(seckill *jd_seckill.Seckill, taskNum int) {
	for i := 1; i <= taskNum; i++ {
		go func(seckill *jd_seckill.Seckill) {
			seckill.RequestSeckillUrl()
			seckill.SeckillPage()
			seckill.SubmitSeckillOrder()
		}(seckill)
	}
}
