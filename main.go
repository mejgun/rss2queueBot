package main

import (
	"context"
	"encoding/gob"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/mmcdole/gofeed"
)

const (
	sendedUrlsFile = "sendedUrls.gob"
	configFile     = "config.json"
)

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

func saveToFile(s, dir string, chat int64) {
	var queue Queue
	queue.Caption = s
	queue.Method = "sendText"
	queue.ChatID = chat
	content, _ := json.Marshal(queue)
	ok := false
	for range 99 {
		time.Sleep(1 * time.Second)
		tmpfile, err := os.CreateTemp(dir, "rss_")
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
	guidOrLink := func(item *gofeed.Item) string {
		if len(item.GUID) > 3 {
			return item.GUID
		}
		return item.Link
	}
	if len(urls) == 0 {
		for _, i := range items {
			urls = append(urls, guidOrLink(i))
		}
	}
	exist := func(item *gofeed.Item) bool {
		return slices.Contains(urls, item.GUID) ||
			slices.Contains(urls, item.Link) ||
			slices.Contains(urls, strings.Replace(item.Link, "http://", "https://", 1)) ||
			slices.Contains(urls, strings.Replace(item.Link, "https://", "http://", 1))
	}
	for _, i := range items {
		if !exist(i) {
			fmt.Println(" * Title=", i.Title, "Link=", i.Link)
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

func getFeed(url string, timeout time.Duration) ([]*gofeed.Item, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	fp := gofeed.NewParser()
	feed, err := fp.ParseURLWithContext(url, ctx)
	if feed != nil {
		return feed.Items, err
	}
	return nil, err
}

func tryGetFeed(url string, count uint, sleepTime, timeout time.Duration) ([]*gofeed.Item, error) {
	var i uint
	for {
		time.Sleep(sleepTime)
		feed, err := getFeed(url, timeout)
		if err == nil || i >= count {
			return feed, err
		}
		i++
		time.Sleep(time.Duration(i) * time.Second)
	}
}

func readUrlsFromDump() (map[string][]string, error) {
	var (
		r          io.ReadCloser
		err        error
		sendedUrls map[string][]string
	)
	r, err = os.Open(sendedUrlsFile)
	if err != nil {
		return sendedUrls, err
	}
	dec := gob.NewDecoder(r)
	err = dec.Decode(&sendedUrls)
	if err != nil {
		return sendedUrls, err
	}
	return sendedUrls, r.Close()
}

func writeUrlsToDump(sendedUrls map[string][]string) error {
	var (
		w   io.WriteCloser
		err error
	)
	w, err = os.Create(sendedUrlsFile)
	if err != nil {
		log.Print("encode error :", err)
		return err
	}
	enc := gob.NewEncoder(w)
	err = enc.Encode(sendedUrls)
	if err != nil {
		log.Print("encode error :", err)
		return err
	}
	return w.Close()
}

func readConfig() (
	map[string]int64,
	string,
	int64,
	time.Duration,
	time.Duration,
) {
	type Configuration struct {
		Dir          string
		ErrorChat    int64
		SleepSeconds uint8
		TimeOut      uint8
		Data         []struct {
			Urls []string
			Chat int64
		}
	}
	file, err := os.Open(configFile)
	if err != nil {
		log.Fatal(err)
	}
	decoder := json.NewDecoder(file)
	configuration := Configuration{}
	err = decoder.Decode(&configuration)
	if err != nil {
		log.Fatal(err)
	}
	var urls = make(map[string]int64)
	for _, d := range configuration.Data {
		for _, u := range d.Urls {
			urls[u] = d.Chat
		}
	}
	if configuration.SleepSeconds == 0 {
		configuration.SleepSeconds = 120
	}
	if configuration.TimeOut == 0 {
		configuration.TimeOut = 10
	}
	return urls,
		configuration.Dir,
		configuration.ErrorChat,
		time.Duration(configuration.SleepSeconds) * time.Second,
		time.Duration(configuration.TimeOut) * time.Minute
}

func main() {
	var urlsAndChat map[string]int64
	var dir string
	var errorChat int64
	var sleepTime time.Duration
	var timeout time.Duration
	urlsAndChat, dir, errorChat, sleepTime, timeout = readConfig()
	sendedUrls, err := readUrlsFromDump()
	if err != nil {
		log.Fatalf("cannot read dump file: %s", err)
	}
	for {
		log.Println("new round")
		for url, chat := range urlsAndChat {
			items, err := tryGetFeed(url, 5, sleepTime, timeout)
			if err == nil {
				urls := sendNewItems(items, sendedUrls[url], dir, chat)
				sendedUrls[url] = urls
				log.Println(url, len(urls))
			} else {
				saveToFile(fmt.Sprintf("%s %s", url, err), dir, errorChat)
				log.Println(url, err)
			}
		}
		for key := range sendedUrls {
			if !stringInMap(key, urlsAndChat) {
				delete(sendedUrls, key)
			}
		}
		if err := writeUrlsToDump(sendedUrls); err != nil {
			log.Fatalf("cannot dump urls to file: %s", err)
		}
	}
}
