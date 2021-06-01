// Copyright (c) 2015-2021 MinIO, Inc.
//
// This file is part of MinIO Object Storage stack
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program.  If not, see <http://www.gnu.org/licenses/>.
package main

import (
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net"
	"net/http"
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
	defer conn.Close()
	n, err := io.Copy(ioutil.Discard, conn)
	if err != nil {
		return
	}
	atomic.AddUint64(&dataIn, uint64(n))
}

func runServer() {
	l, err := net.Listen("tcp", net.JoinHostPort("", port))
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
			n, err := conn.Write(b)
			if err != nil {
				conn.Close()
				fmt.Println(host, ": disconnected")
				break
			}
			atomic.AddUint64(&dataOut, uint64(n))
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
					n, err := conn.Write(b)
					if err != nil {
						conn.Close()
						break
					}
					atomic.AddUint64(&dataOut, uint64(n))
				}
			}
		}()
	}
}

func main() {
	flag.Parse()
	if flag.NArg() == 0 {
		log.Fatal("provide a list of hostnames or IP addresses")
	}

	hostMap := make(map[string]struct{}, flag.NArg())
	for _, host := range flag.Args() {
		if _, ok := hostMap[host]; ok {
			log.Fatalln("duplicate arguments found, please make sure all arguments are unique")
		}
		hostMap[host] = struct{}{}
	}

	s := &http.Server{
		Addr:           ":10000",
		MaxHeaderBytes: 1 << 20,
	}

	go func() {
		http.HandleFunc("/"+uniqueStr, func(w http.ResponseWriter, req *http.Request) {})
		s.ListenAndServe()
	}()
	log.Println("Starting HTTP service to skip self..")
	time.Sleep(time.Second * 2)

	go runServer()
	go printDataOut()
	for host := range hostMap {
		resp, err := http.Get("http://" + host + ":10000/" + uniqueStr)
		if err == nil && resp.StatusCode == http.StatusOK {
			resp.Body.Close() // close the connection.
			s.Close()         // close the server as we are done.
			log.Println("HTTP service closed after successful skip...")
			continue
		}
		go runClient(host)
	}
	time.Sleep(time.Hour * 72)
}
