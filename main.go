package main

import (
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"sync/atomic"
	"time"

	humanize "github.com/dustin/go-humanize"
	"github.com/google/uuid"
)

var port = "9999"

var uniqueStr = uuid.New().String()

var oneMB = 1024 * 1024

var dataIn uint64
var dataOut uint64

func printDataOut() {
	for {
		time.Sleep(time.Second)
		lastDataIn := atomic.SwapUint64(&dataIn, 0)
		lastDataOut := atomic.SwapUint64(&dataOut, 0)
		fmt.Printf("Bandwidth:  %s/s RX  |  %s/s TX\n", humanize.Bytes(lastDataIn), humanize.Bytes(lastDataOut))
	}
}

func handleRequest(conn net.Conn) {
	b := make([]byte, oneMB)
	for {
		n, err := conn.Read(b)
		if err != nil {
			break
		}
		atomic.AddUint64(&dataIn, uint64(n))
	}
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
	b := make([]byte, oneMB)
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
			atomic.AddUint64(&dataOut, uint64(oneMB))
		}
	}
	for i := 0; i < 16; i++ {
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
					atomic.AddUint64(&dataOut, uint64(oneMB))
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
	go printDataOut()
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
