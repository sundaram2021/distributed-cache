package main

import (
	"flag"
	"fmt"
	"net/http"
	"strings"

	src "github.com/sundaram2021/distributed-cache/src"
)

var port string
var peers string

func main() {
	flag.StringVar(&port, "port", ":8080", "HTTP server port")
	flag.StringVar(&peers, "peers", "", "Comma-separated list of peer addresses")

	flag.Parse()

	peerList := strings.Split(peers, ",")

	cs := src.NewCacheServer(append(peerList, "self"))
	http.HandleFunc("/set", cs.SetHandler)
	http.HandleFunc("/get", cs.GetHandler)
	err := http.ListenAndServe(port, nil)
	if err != nil {
		fmt.Println(err)
		return
	}
}