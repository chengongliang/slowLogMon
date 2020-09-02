package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	_ "github.com/go-sql-driver/mysql"
	"github.com/gomodule/redigo/redis"
	"github.com/jinzhu/gorm"
	"github.com/spf13/viper"
)

type MyLog struct {
	Timestamp time.Time `json:"@timestamp"`
	Host      string    `json:"host"`
	InputType string    `json:"input_type"`
	Message   string    `json:"message"`
	Offset    int       `json:"offset"`
	Source    string    `json:"source"`
	Type      string    `json:"type"`
}

type SlowLog struct {
	ID           int64     `gorm:"column:id" json:"id"`
	Time         time.Time `gorm:"column:time" json:"time"`
	Target       string    `gorm:"column:target" json:"target"`
	User         string    `gorm:"column:user" json:"user"`
	IP           string    `gorm:"column:ip" json:"ip"`
	Database     string    `gorm:"column:database" json:"database"`
	QueryTime    float64   `gorm:"column:query_time" json:"query_time"`
	LockTime     float64   `gorm:"column:lock_time" json:"lock_time"`
	RowsSent     int64     `gorm:"column:rows_sent" json:"rows_sent"`
	RowsExamined int64     `gorm:"column:rows_examined" json:"rows_examined"`
	RowsAffected int64     `gorm:"column:rows_affected" json:"rows_affected"`
	BytesSent    int64     `gorm:"column:bytes_sent" json:"bytes_sent"`
	Sql          string    `gorm:"column:sql" json:"sql"`
	Stat         int64     `gorm:"column:stat" json:"stat"`
	FinishTime   time.Time `gorm:"column:finish_time" json:"finish_time"`
}

func sendDingTalk(message, token string) {
	type cnt struct {
		Text  string `json:"text"`
		Title string `json:"title"`
	}

	type msg struct {
		MsgType  string `json:"msgtype"`
		Markdown cnt    `json:"markdown"`
	}

	url := "https://oapi.dingtalk.com/robot/send?access_token=" + token
	text := msg{}
	text.MsgType = "markdown"
	text.Markdown.Text = message
	text.Markdown.Title = "SQL报警"
	t, _ := json.Marshal(text)
	r, err := http.Post(url, "application/json;charset=utf-8", bytes.NewReader([]byte(t)))
	if err != nil {
		fmt.Println(time.Now().Format("2006-01-02 15:04:05"), err)
	}
	defer r.Body.Close()
	body, err := ioutil.ReadAll(r.Body)
	if err != nil {
		fmt.Println(time.Now().Format("2006-01-02 15:04:05"), err)
	}
	fmt.Println(time.Now().Format("2006-01-02 15:04:05"), string(body))
}

func alert(alertInterval int64, token string, msgChan <-chan string) {
	var oneMsg string
	for {
		select {
		case <-time.After(time.Duration(alertInterval) * time.Second):
			if oneMsg != "" {
				sendDingTalk(oneMsg, token)
				oneMsg = ""
			}
		case msg := <-msgChan:
			oneMsg += msg
		}
	}
}

func main() {
	fmt.Println(time.Now().Format("2006-01-02 15:04:05"), "start monitor")
	v := viper.New()
	v.SetConfigName("conf")
	v.AddConfigPath(".")
	v.SetConfigType("yaml")
	err := v.ReadInConfig()
	if err != nil {
		panic(fmt.Errorf(time.Now().Format("2006-01-02 15:04:05"), "Fatal error config file: %s \n", err))
	}
	redisUrl := fmt.Sprintf("%s:%d", v.GetString("redis.host"), v.GetInt("redis.port"))
	mysqlUrl := fmt.Sprintf("%s:%s@tcp(%s:%d)/%s", v.GetString("mysql.username"), v.GetString("mysql.passwd"), v.GetString("mysql.host"), v.GetInt("mysql.port"), v.GetString("mysql.db"))
	storeFlag := v.GetBool("mysql.store")
	token := v.GetString("dingTalk.token")
	db, err := gorm.Open("mysql", mysqlUrl+"?charset=utf8mb4&parseTime=True&loc=Local")
	if err != nil {
		fmt.Println(time.Now().Format("2006-01-02 15:04:05"), err)
		return
	}
	defer db.Close()
	sqlRegex := regexp.MustCompile(`\bUser@Host: \S+\[(\w+)\]\s+@\s+\[(\d.*)\][\s\S]*
# Schema: (\w+)[\s\S]*
# Query_time: (\d+.\d+)\s+Lock_time: (\d+.\d+)\s+Rows_sent: (\d+)\s+Rows_examined: (\d+)\s+Rows_affected: (\d+)[\s\S]*
# Bytes_sent: (\d+)[\s\S]*
SET timestamp=(\d+);
([\s\S;]*)`)
	portRegex := regexp.MustCompile(v.GetString("other.portRegex"))
	key := v.GetString("redis.key")
	whiteIPs := v.GetString("white_ip.ip")
	checkInterval := v.GetInt64("other.check_interval")
	alertInterval := v.GetInt64("other.alert_interval")
	ticker := time.Tick(time.Duration(checkInterval) * time.Second)

	var msgChan = make(chan string, 5)
	go alert(alertInterval, token, msgChan)
	for {
		select {
		case <-ticker:
			c, err := redis.Dial("tcp", redisUrl)
			if err != nil {
				fmt.Println(time.Now().Format("2006-01-02 15:04:05"), "Connect to redis error", err)
				return
			}
			//defer c.Close()
			t := time.Now().Format("2006-01-02 15:04:05")
			var slowLog MyLog

			llen, err := c.Do("llen", key)
			if err != nil {
				fmt.Println(err)
			}
			if llen.(int64) == 0 {
				continue
			}
			for i := 0; i < int(llen.(int64)); i++ {
				res, err := c.Do("lpop", key)
				if err != nil {
					fmt.Println(t, "redis lpop failed:", err)
					return
				}
				if res == nil {
					return
				} else if err := json.Unmarshal(res.([]byte), &slowLog); err != nil {
					fmt.Println(t, err)
				}
				params := sqlRegex.FindStringSubmatch(slowLog.Message)
				if len(params) != 12 {
					continue
				}
				var s SlowLog
				s.User = params[1]
				s.IP = params[2]
				s.Database = params[3]
				s.QueryTime, _ = strconv.ParseFloat(params[4], 64)
				s.LockTime, _ = strconv.ParseFloat(params[5], 64)
				s.RowsSent, _ = strconv.ParseInt(params[6], 10, 64)
				s.RowsExamined, _ = strconv.ParseInt(params[7], 10, 64)
				s.RowsAffected, _ = strconv.ParseInt(params[8], 10, 64)
				s.BytesSent, _ = strconv.ParseInt(params[9], 10, 64)
				timeStamp, _ := strconv.ParseInt(params[10], 10, 64)
				s.Time = time.Unix(timeStamp, 0)
				s.Sql = params[11]
				qTime := v.GetFloat64("other.query_time")
				port := "3306"
				portMatch := portRegex.FindStringSubmatch(slowLog.Source)
				if len(portMatch) == 2 {
					port = portMatch[1]
				}
				s.Target = slowLog.Host + ":" + port
				alertFlag := true
				if s.IP == "127.0.0.1" {
					continue
				}
				if v.GetInt("white_ip.stat") == 1 && strings.Contains(whiteIPs, s.IP) {
					alertFlag = false
					s.Stat = 2
				}
				if storeFlag {
					if err := db.Create(&s).Error; err != nil {
						fmt.Println(t, err)
					}
				}
				// fmt.Println(s.QueryTime, alertFlag)
				if s.QueryTime > qTime && alertFlag {
					msg := fmt.Sprintf("# <font face=\"微软雅黑\">慢SQL通知</font>\n\n<br/>\n**地址:** %v\n\n<br/>**DB:** %v\n\n<br/>**来源IP:** %v\n\n<br/>**SQL 时间:** %v\n\n<br/>**执行时间:** %v\n\n<br/>**执行内容:**\n\n<br/>```%v```\n\n<br/>",
						s.Target, s.Database, s.IP, s.Time, s.QueryTime, s.Sql)
					msgChan <- msg
				}
			}
		}
	}
}
