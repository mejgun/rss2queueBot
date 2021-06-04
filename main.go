package main

import (
	"encoding/gob"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/mmcdole/gofeed"
)

var sendedUrlsFile = "sendedUrls.gob"
var sendedUrls map[string][]string

const waitAfterErr = time.Duration(time.Second)

// Queue struct
type Queue struct {
	Method  string `json:"method"`
	Caption string `json:"caption"`
	ChatID  int64  `json:"chat_id"`
}

func stringInMap(a string, list map[string]int64) bool {
	for b := range list {
		if b == a {
			return true
		}
	}
	return false
}

func stringInSlice(a string, list []string) bool {
	for _, b := range list {
		if b == a {
			return true
		}
	}
	return false
}

func guidOrLink(i *gofeed.Item) string {
	if len(i.GUID) > 3 {
		return i.GUID
	}
	return i.Link
}

func saveToFile(s, dir string, chat int64) {
	var queue Queue
	queue.Caption = s
	queue.Method = "sendText"
	queue.ChatID = chat
	content, _ := json.Marshal(queue)
	ok := false
	for i := 0; i < 99; i++ {
		time.Sleep(1 * time.Second)
		tmpfile, err := ioutil.TempFile(dir, "rss_")
		if err != nil {
			log.Println("tmp file create error:", err, dir)
			continue
		}
		if _, err := tmpfile.Write(content); err != nil {
			log.Println("json save to file error:", err)
			continue
		}
		if err := tmpfile.Close(); err != nil {
			log.Println("tmp file close error:", err)
			continue
		}
		ok = true
		break
	}
	if !ok {
		log.Fatal("queue file create error")
	}
}

func sendNewItems(items []*gofeed.Item, urls []string, dir string, chat int64) []string {
	if len(urls) == 0 {
		for _, i := range items {
			urls = append(urls, guidOrLink(i))
		}
	}
	for _, i := range items {
		if stringInSlice(i.GUID, urls) ||
			stringInSlice(i.Link, urls) ||
			stringInSlice(strings.Replace(i.Link, "http://", "https://", 1), urls) ||
			stringInSlice(strings.Replace(i.Link, "https://", "http://", 1), urls) {
		} else {
			fmt.Println("Title=", i.Title, "Link=", i.Link)
			text := fmt.Sprintf("%s %s", i.Title, i.Link)
			saveToFile(text, dir, chat)
			urls = append(urls, guidOrLink(i))
		}
	}
	if len(urls) > 999 {
		urls = urls[1:]
	}
	return urls
}

func getFeed(url string) ([]*gofeed.Item, error) {
	fp := gofeed.NewParser()
	fp.Client = &http.Client{Timeout: 5 * time.Minute}
	feed, err := fp.ParseURL(url)
	if err == nil {
		return feed.Items, nil
	}
	return nil, err
}

func tryGetFeed(url string, count uint) ([]*gofeed.Item, error) {
	var i uint
	for {
		feed, err := getFeed(url)
		if err == nil || i >= count {
			return feed, err
		}
		i++
		time.Sleep(waitAfterErr)
	}
}

func readUrlsFromDump() error {
	var r io.ReadCloser
	r, _ = os.Open(sendedUrlsFile)
	dec := gob.NewDecoder(r)
	err := dec.Decode(&sendedUrls)
	r.Close()
	if err != nil {
		log.Print("decode error :", err)
		return err
	}
	return nil
}

func writeUrlsToDump() error {
	// fmt.Println(urls) // debug
	var w io.WriteCloser
	w, _ = os.Create(sendedUrlsFile)
	enc := gob.NewEncoder(w)
	err := enc.Encode(sendedUrls)
	w.Close()
	if err != nil {
		log.Print("encode error :", err)
		return err
	}
	return nil
}

func readConfig() (map[string]int64, string, int64, uint8) {
	type Configuration struct {
		Dir       string
		ErrorChat int64
		SleepTime uint8
		Data      []struct {
			Urls []string
			Chat int64
		}
	}
	file, err := os.Open("config.json")
	if err != nil {
		log.Panic(err)
	}
	decoder := json.NewDecoder(file)
	configuration := Configuration{}
	err = decoder.Decode(&configuration)
	if err != nil {
		log.Panic(err)
	}
	var urls map[string]int64 = map[string]int64{}
	for _, d := range configuration.Data {
		for _, u := range d.Urls {
			urls[u] = d.Chat
		}
	}
	return urls, configuration.Dir, configuration.ErrorChat, configuration.SleepTime
}

func main() {
	var urlsAndChat map[string]int64
	var dir string
	var errorChat int64
	var sleepTime uint8
	urlsAndChat, dir, errorChat, sleepTime = readConfig()
	sendedUrls = make(map[string][]string)
	readUrlsFromDump()
	for {
		log.Print("get...")
		for url, chat := range urlsAndChat {
			items, err := tryGetFeed(url, 5)
			if err == nil {
				urls := sendNewItems(items, sendedUrls[url], dir, chat)
				sendedUrls[url] = urls
				fmt.Println(url, len(urls)) // debug
			} else {
				saveToFile(fmt.Sprintf("%s %s", url, err), dir, errorChat)
				fmt.Println(url, err)
			}
		}
		for key := range sendedUrls {
			if !stringInMap(key, urlsAndChat) {
				delete(sendedUrls, key)
			}
		}
		writeUrlsToDump()
		log.Print("sleep...")
		time.Sleep(time.Duration(sleepTime) * time.Minute)
	}
}
