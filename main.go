// Copyright (c) 2015-2024 MinIO, Inc.
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
	"context"
	"crypto/rand"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"strconv"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/dustin/go-humanize"
	"github.com/google/uuid"
	"golang.org/x/sys/unix"
)

var port = func() string {
	p := os.Getenv("HPERF_PORT")
	if p == "" {
		p = "9999"
	}
	return p
}()

var selfDetectPort = func() string {
	if sp := os.Getenv("HPERF_SELF_PORT"); sp != "" {
		return sp
	}
	sp, err := strconv.Atoi(port)
	if err != nil {
		log.Fatal(err)
	}
	sp++
	return strconv.Itoa(sp)
}()

var uniqueStr = uuid.New().String()

var (
	dataIn  uint64
	dataOut uint64
)

const dialTimeout = 1 * time.Second

func printDataOut() {
	for {
		time.Sleep(time.Second)
		lastDataIn := atomic.SwapUint64(&dataIn, 0)
		lastDataOut := atomic.SwapUint64(&dataOut, 0)
		fmt.Printf("Bandwidth:  %s/s RX  |  %s/s TX\n", humanize.Bytes(lastDataIn), humanize.Bytes(lastDataOut))
	}
}

// Discard is just like io.Discard without the io.ReaderFrom compatible
// implementation which is buggy on NUMA systems, we have to use a simpler
// io.Writer implementation alone avoids also unnecessary buffer copies,
// and as such incurred latencies.
var Discard io.Writer = discard{}

// discard is /dev/null for Golang.
type discard struct{}

func (discard) Write(p []byte) (int, error) {
	atomic.AddUint64(&dataIn, uint64(len(p)))
	return len(p), nil
}

func runServer(host string) {
	http.HandleFunc("/devnull", func(w http.ResponseWriter, r *http.Request) {
		buf := make([]byte, 1*humanize.MiByte)
		io.CopyBuffer(Discard, r.Body, buf)
	})
	s := &http.Server{
		Addr:           net.JoinHostPort(host, port),
		MaxHeaderBytes: 1 << 20,
	}
	s.ListenAndServe()
}

// DialContext is a function to make custom Dial for internode communications
type DialContext func(ctx context.Context, network, address string) (net.Conn, error)

func setTCPParametersFn() func(network, address string, c syscall.RawConn) error {
	return func(network, address string, c syscall.RawConn) error {
		c.Control(func(fdPtr uintptr) {
			// got socket file descriptor to set parameters.
			fd := int(fdPtr)

			_ = unix.SetsockoptInt(fd, unix.SOL_SOCKET, unix.SO_REUSEADDR, 1)

			_ = unix.SetsockoptInt(fd, unix.SOL_SOCKET, unix.SO_REUSEPORT, 1)

			{
				// Enable big buffers
				_ = unix.SetsockoptInt(fd, unix.SOL_SOCKET, unix.SO_SNDBUF, 8*humanize.MiByte)

				_ = unix.SetsockoptInt(fd, unix.SOL_SOCKET, unix.SO_RCVBUF, 8*humanize.MiByte)
			}

			// Enable TCP open
			// https://lwn.net/Articles/508865/ - 32k queue size.
			_ = syscall.SetsockoptInt(fd, syscall.SOL_TCP, unix.TCP_FASTOPEN, 32*1024)

			// Enable TCP fast connect
			// TCPFastOpenConnect sets the underlying socket to use
			// the TCP fast open connect. This feature is supported
			// since Linux 4.11.
			_ = syscall.SetsockoptInt(fd, syscall.IPPROTO_TCP, unix.TCP_FASTOPEN_CONNECT, 1)

			// Enable TCP quick ACK, John Nagle says
			// "Set TCP_QUICKACK. If you find a case where that makes things worse, let me know."
			_ = syscall.SetsockoptInt(fd, syscall.IPPROTO_TCP, unix.TCP_QUICKACK, 1)

			/// Enable keep-alive
			{
				_ = unix.SetsockoptInt(fd, unix.SOL_SOCKET, unix.SO_KEEPALIVE, 1)

				// The time (in seconds) the connection needs to remain idle before
				// TCP starts sending keepalive probes
				_ = syscall.SetsockoptInt(fd, syscall.IPPROTO_TCP, syscall.TCP_KEEPIDLE, 15)

				// Number of probes.
				// ~ cat /proc/sys/net/ipv4/tcp_keepalive_probes (defaults to 9, we reduce it to 5)
				_ = syscall.SetsockoptInt(fd, syscall.IPPROTO_TCP, syscall.TCP_KEEPCNT, 5)

				// Wait time after successful probe in seconds.
				// ~ cat /proc/sys/net/ipv4/tcp_keepalive_intvl (defaults to 75 secs, we reduce it to 15 secs)
				_ = syscall.SetsockoptInt(fd, syscall.IPPROTO_TCP, syscall.TCP_KEEPINTVL, 15)
			}
		})
		return nil
	}
}

// NewInternodeDialContext setups a custom dialer for internode communication
func NewInternodeDialContext(dialTimeout time.Duration) DialContext {
	d := &net.Dialer{
		Timeout: dialTimeout,
		Control: setTCPParametersFn(),
	}
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		return d.DialContext(ctx, network, addr)
	}
}

// Reader to read random data.
type netperfReader struct {
	doneCh <-chan struct{}
	buf    []byte
}

func (m *netperfReader) Read(b []byte) (int, error) {
	select {
	case <-m.doneCh:
		return 0, io.EOF
	default:
	}
	n := copy(b, m.buf)
	atomic.AddUint64(&dataOut, uint64(n))
	return n, nil
}

func runClient(host string) {
	host = net.JoinHostPort(host, port)
	proc := 32

	// For more details about various values used here refer
	// https://golang.org/pkg/net/http/#Transport documentation
	tr := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           NewInternodeDialContext(10 * time.Second),
		MaxIdleConnsPerHost:   1024,
		WriteBufferSize:       64 << 10, // 64KiB moving up from 4KiB default
		ReadBufferSize:        64 << 10, // 64KiB moving up from 4KiB default
		IdleConnTimeout:       15 * time.Second,
		ResponseHeaderTimeout: 15 * time.Minute, // Conservative timeout is the default (for MinIO internode)
		TLSHandshakeTimeout:   10 * time.Second,
		// Go net/http automatically unzip if content-type is
		// gzip disable this feature, as we are always interested
		// in raw stream.
		DisableCompression: true,
	}

	clnt := &http.Client{
		Transport: tr,
	}

	ctx, cancel := context.WithCancel(context.Background())
	r := &netperfReader{doneCh: ctx.Done()}
	r.buf = make([]byte, 1*humanize.MiByte)
	rand.Read(r.buf)

	var wg sync.WaitGroup
	for i := 0; i < proc; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			// Establish the connection.
			for {
				req, err := http.NewRequestWithContext(ctx, http.MethodPut, "http://"+host+"/devnull", nil)
				if err != nil {
					log.Println("Client-Error-New", err)
					time.Sleep(dialTimeout)
					continue
				}
				req.Body = io.NopCloser(r)
				req.ContentLength = -1

				resp, err := clnt.Do(req)
				if err != nil {
					log.Println("Client-Error-Do", err)
					time.Sleep(dialTimeout)
					continue
				}
				io.Copy(io.Discard, resp.Body)
				resp.Body.Close()

				if resp.StatusCode != http.StatusOK {
					log.Println("Client-Error-Response", resp.Status)
					time.Sleep(dialTimeout)
					continue
				}
			}
		}()
	}

	wg.Wait()
	cancel()
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
		Addr:           ":" + selfDetectPort,
		MaxHeaderBytes: 1 << 20,
	}

	go func() {
		http.HandleFunc("/"+uniqueStr, func(w http.ResponseWriter, req *http.Request) {})
		s.ListenAndServe()
	}()
	log.Println("Starting HTTP service to skip self.. waiting for 10secs for services to be ready")
	time.Sleep(time.Second * 10)

	var serverRunningOnce sync.Once
	for host := range hostMap {
		resp, err := http.Get("http://" + host + ":" + selfDetectPort + "/" + uniqueStr)
		if err == nil && resp.StatusCode == http.StatusOK {
			resp.Body.Close() // close the connection.
			s.Close()         // close the server as we are done.
			log.Println("HTTP service closed after successful skip...")
			serverRunningOnce.Do(func() {
				go runServer(host)
			})
			continue
		}
		go runClient(host)
	}

	go printDataOut()
	time.Sleep(time.Hour * 72)
}
