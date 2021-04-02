package main

import (
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/google/uuid"
)

var port = "9999"

var uniqueStr = uuid.New().String()

func handleRequest(conn net.Conn) {
	io.Copy(ioutil.Discard, conn)
	conn.Close()
}

func runServer() {
	l, err := net.Listen("tcp", ":"+port)
	if err != nil {
		log.Fatal(err)
	}
	defer l.Close()
	for {
		// Listen for an incoming connection.
		conn, err := l.Accept()
		if err != nil {
			log.Fatal(err)
		}
		// Handle connections in a new goroutine.
		go handleRequest(conn)
	}
}

func runClient(host string) {
	host = host + ":" + port
	b := make([]byte, 1024*0124)
	for {
		conn, err := net.Dial("tcp", host)
		if err != nil {
			time.Sleep(time.Second)
			continue
		}
		fmt.Println(host, ": connected")
		for {
			_, err := conn.Write(b)
			if err != nil {
				conn.Close()
				fmt.Println(host, ": disconnected")
				break
			}
		}
	}
	for i := 0; i < 5; i++ {
		go func() {
			for {
				conn, err := net.Dial("tcp", host)
				if err != nil {
					time.Sleep(time.Second)
					continue
				}
				for {
					_, err := conn.Write(b)
					if err != nil {
						conn.Close()
						break
					}
				}
			}
		}()
	}
}

func main() {
	if len(os.Args) == 1 {
		log.Fatal("provide a list of IP addresses")
	}
	go func() {
		http.HandleFunc("/"+uniqueStr, func(w http.ResponseWriter, req *http.Request) {})
		log.Fatal(http.ListenAndServe(":10000", nil))
	}()

	time.Sleep(time.Second * 2)

	go runServer()
	for i := 1; i < len(os.Args); i++ {
		host := os.Args[i]
		resp, err := http.Get("http://" + host + ":10000/" + uniqueStr)
		if err == nil {
			// Skip localhost
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				continue
			}
		}
		go runClient(host)
	}
	time.Sleep(time.Hour * 72)
}
