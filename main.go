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
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/dustin/go-humanize"
	"github.com/minio/pkg/v3/ellipses"
	"golang.org/x/sys/unix"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/lipgloss/table"
)

// dataPoint is collected every second+- a few micro/nano seconds.
type dataPoint struct {
	Created time.Time
	// Local IP Address is the address which listens for data
	Local string
	// Remote is the IP Address specified on the sender
	Remote string
	// LowestTransferMS - lowest time seen (in MS) transferring 1xPayload
	LowestTransferMS int64
	// HighestTransferMS - highest time seen (in MS) to transferring 1xPayload
	HighestTransferMS int64

	// TTFBHigh - highest time to first byte
	TTFBHigh int64
	// TTFBLow - lowest time to first byte
	TTFBLow int64

	// RX - Total transmitted amount
	RX uint64
	// RXCount - total sent http requests
	RXCount uint64
	// TX - Total received amount
	TX uint64
	// TXCount - total received http requests
	TXCount uint64

	// The number of errors seen
	Errors uint64
}

var (
	lastRow      = 0
	headerStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("#f2f2f2")).Padding(0, 1)
	tableStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("#f2f2f2")).Padding(0, 1).Align(lipgloss.Right)
	baseTable    *table.Table
	latencyTable *table.Table
	streamTable  *table.Table
	fullTable    *table.Table
	finalTable   *table.Table
	styleFunc    = func(row, col int) lipgloss.Style {
		switch {
		case row == 0:
			return headerStyle
		default:
			return tableStyle
		}
	}
)

func createTables() {
	finalTable = table.New().
		Border(lipgloss.NormalBorder()).
		BorderStyle(lipgloss.NewStyle().Foreground(lipgloss.Color("99")))

	if latency {
		finalTable.Headers("Local", "Remote", "TX(ms) high/low", "Err #")
		return
	} else if stream {
		finalTable.Headers("Local", "Remote", "RX", "TX")
		return
	}

	finalTable.Headers("Local", "Remote", "#RX", "RX", "#TX", "TX", "TX(ms) high/low", "TTFB(ms) high/low", "#Err")
	finalTable.StyleFunc(styleFunc)
}

func printTable(list []dataPoint) {
	if len(list) == 0 {
		return
	}

	if globalErr != nil {
		fmt.Println("Last Error: ", globalErr.Error())
		globalErr = nil
	}

	rows := make([][]string, len(list))

	for i := range list {

		local := list[i].Local
		remote := list[i].Remote

		txc := strconv.Itoa(int(list[i].TXCount))
		tx := humanize.Bytes(list[i].TX) + "/s"

		rxc := strconv.Itoa(int(list[i].RXCount))
		rx := humanize.Bytes(list[i].RX) + "/s"

		txms := strconv.Itoa(int(list[i].HighestTransferMS)) + " / " + strconv.Itoa(int(list[i].LowestTransferMS))
		ttfb := strconv.Itoa(int(list[i].TTFBHigh)) + " / " + strconv.Itoa(int(list[i].TTFBLow))
		errCount := strconv.Itoa(int(list[i].Errors))
		if latency {
			rows[i] = append(rows[i], local, remote, txms, errCount)
		} else if stream {
			rows[i] = append(rows[i], local, remote, rx, tx)
		} else {
			rows[i] = append(rows[i], local, remote, rxc, rx, txc, tx, txms, ttfb, errCount)
		}
	}

	finalTable.ClearRows()
	finalTable.Data(table.NewStringData())
	finalTable.Rows(rows...)
	lastRow = len(rows)
	fmt.Println(finalTable)
}

func printDataOut(ctx context.Context, cancel context.CancelFunc) {
	sigc := make(chan os.Signal, 1)
	signal.Notify(sigc,
		syscall.SIGHUP,
		syscall.SIGINT,
		syscall.SIGTERM,
		syscall.SIGQUIT)
	go func() {
		<-sigc
		httpListener.Close()
		cancel()
	}()

	createTables()

	lastPrint := false
	for !lastPrint {
		time.Sleep(1 * time.Second)

		// We wait for requests and clients to exit so that
		// the request counters are as accurate as possible.
		// lastPrint = activeClients.Load() == 0 && activeRequests.Load() == 0 && ctx.Err() != nil
		if activeClients.Load() == 0 && activeRequests.Load() == 0 {
			if ctx.Err() != nil {
				lastPrint = true
			}
		}

		if time.Since(start).Seconds() > float64(seconds) {
			if httpListener != nil {
				httpListener.Close()
			}
			cancel()
		}

		list := generateDataPoint()
		if printJSON {
			for _, v := range list {
				outb, _ := json.Marshal(v)
				fmt.Println(string(outb))
			}
		} else {
			printTable(list)
		}

	}
}

func generateDataPoint() (list []dataPoint) {
	list = make([]dataPoint, 0, len(Readers))
	for ri, rv := range Readers {
		if rv == nil {
			continue
		}
		for wi, wv := range Writers {
			if wv == nil || wv.host != rv.host {
				continue
			}

			w := Writers[wi]
			r := Readers[ri]

			list = append(list, dataPoint{
				Created:           time.Now(),
				RX:                w.rx.Swap(0),
				RXCount:           w.count.Load(),
				TX:                r.tx.Swap(0),
				TXCount:           r.count.Load(),
				Local:             currentHost,
				TTFBLow:           r.ttfbLow,
				TTFBHigh:          r.ttfbHigh,
				LowestTransferMS:  r.lowestTransferMS,
				HighestTransferMS: r.highestTransferMS,
				Errors:            r.errors.Swap(0),
				Remote:            r.host,
			})

			r.m.Lock()
			r.ttfbHigh = 0
			r.ttfbLow = 999
			r.highestTransferMS = 0
			r.lowestTransferMS = 999
			r.m.Unlock()

		}
	}
	return
}

type netperfWriter struct {
	buf   []byte
	rx    atomic.Uint64
	count atomic.Uint64
	host  string
}

func (w *netperfWriter) Write(p []byte) (int, error) {
	w.rx.Add(uint64(len(p)))
	return len(p), nil
}

func runServer(host string) {
	http.HandleFunc("/hello", func(w http.ResponseWriter, r *http.Request) {
		index := createNetWriter(r.Header.Get("X-Host"))
		w.Header().Add("X-Index", index)
		w.WriteHeader(200)
		r.Body.Close()
	})

	http.HandleFunc("/ms", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		r.Body.Close()
	})

	http.HandleFunc("/data", func(w http.ResponseWriter, r *http.Request) {
		activeRequests.Add(1)
		defer activeRequests.Add(-1)
		host := getNetWriter(r.Header.Get("X-Index"))
		if host == nil {
			w.WriteHeader(400)
			return
		}

		host.count.Add(1)

		io.CopyBuffer(host, r.Body, host.buf)
		r.Body.Close()
	})

	var err error
	httpListener, err = net.Listen("tcp", net.JoinHostPort(host, strconv.Itoa(port)))
	if err != nil {
		panic(err)
	}

	s := &http.Server{
		Addr:           net.JoinHostPort(host, strconv.Itoa(port)),
		MaxHeaderBytes: 1 << 20,
	}

	err = s.Serve(httpListener)
	if err != nil {
		globalErr = err
		if printAllErrors {
			fmt.Println("Error serving: ", err)
		}
	}
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
	buf []byte

	host   string
	addr   string
	count  atomic.Uint64
	tx     atomic.Uint64
	errors atomic.Uint64

	ttfbHigh          int64
	ttfbLow           int64
	highestTransferMS int64
	lowestTransferMS  int64

	m sync.Mutex
}

type AsyncReader struct {
	pr             *netperfReader
	i              int64 // current reading index
	prevRune       int   // index of previous rune; or < 0
	ttfbRegistered bool
	start          time.Time
	ctx            context.Context
}

func (a *AsyncReader) Read(b []byte) (n int, err error) {
	if !a.ttfbRegistered {
		a.ttfbRegistered = true
		since := time.Since(a.start).Milliseconds()
		a.pr.m.Lock()
		if since > a.pr.ttfbHigh {
			a.pr.ttfbHigh = since
		}
		if since < a.pr.ttfbLow {
			a.pr.ttfbLow = since
		}
		a.pr.m.Unlock()
	}

	if !stream {
		if a.i >= int64(len(a.pr.buf)) {
			return 0, io.EOF
		}
		a.prevRune = -1
		n = copy(b, a.pr.buf[a.i:])
		a.i += int64(n)
	} else {
		if a.ctx.Err() != nil {
			return 0, io.EOF
		}
		n = copy(b, a.pr.buf)
	}

	a.pr.tx.Add(uint64(n))
	return n, nil
}

func NewNetPerfReader(host string) (r *netperfReader) {
	r = new(netperfReader)
	r.host = host
	r.buf = make([]byte, payloadMB*humanize.MiByte)
	r.ttfbLow = 999
	rand.Read(r.buf)
	return
}

func getWriterIndex(r *netperfReader, c *http.Client) string {
	req, err := http.NewRequestWithContext(
		context.Background(),
		http.MethodGet,
		"http://"+r.addr+"/hello",
		nil,
	)
	if err != nil {
		globalErr = err
		if printAllErrors {
			fmt.Println("Error Creating hello request:", err)
		}
		return ""
	}
	req.Header.Set("X-Host", currentHost)
	resp, err := c.Do(req)
	if err != nil {
		globalErr = err
		if printAllErrors {
			fmt.Println("Error sending hello request:", err)
		}
		return ""
	}
	if resp == nil {
		return ""
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	return resp.Header.Get("X-Index")
}

func runClient(ctx context.Context, r *netperfReader) {
	activeClients.Add(1)
	defer activeClients.Add(-1)

	r.addr = net.JoinHostPort(r.host, strconv.Itoa(port))

	// For more details about various values used here refer
	// https://golang.org/pkg/net/http/#Transport documentation
	tr := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           NewInternodeDialContext(10 * time.Second),
		MaxIdleConnsPerHost:   1024,
		WriteBufferSize:       bufferKB * humanize.KByte,
		ReadBufferSize:        bufferKB * humanize.KByte,
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

	// Each http client receives it's own index in order
	// for us to match it with incomming statistics on
	// the receiver side.
	var clientIndex string
	for {
		if ctx.Err() != nil {
			return
		}

		time.Sleep(1 * time.Second)
		clientIndex = getWriterIndex(r, clnt)
		if clientIndex != "" {
			globalErr = nil
			break
		}
		globalErr = errors.New("Waiting for index ...")
	}

	var wg sync.WaitGroup
	for i := 0; i < proc; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			for {
				if ctx.Err() != nil {
					return
				}
				time.Sleep(time.Duration(sleep) * time.Millisecond)
				var req *http.Request

				AR := new(AsyncReader)
				AR.pr = r
				AR.start = time.Now()
				AR.ctx = ctx

				var resp *http.Response
				var err error

				if stream {
					req, err = http.NewRequestWithContext(
						ctx,
						http.MethodPut,
						"http://"+r.addr+"/data",
						nil,
					)
				} else if latency {
					req, err = http.NewRequestWithContext(
						context.Background(),
						http.MethodGet,
						"http://"+r.addr+"/ms",
						nil,
					)
				} else {
					req, err = http.NewRequestWithContext(
						context.Background(),
						http.MethodPut,
						"http://"+r.addr+"/data",
						AR,
					)
				}

				req.Header.Set("X-Index", clientIndex)

				if err != nil {
					globalErr = err
					if printAllErrors {
						fmt.Println("Error Creating request:", err)
					}
					time.Sleep(dialTimeout)
					continue
				}

				if stream {
					req.Body = io.NopCloser(AR)
					req.ContentLength = -1
				}

				sent := time.Now()
				r.count.Add(1)
				resp, err = clnt.Do(req)
				if err != nil {
					if errors.Is(err, context.Canceled) {
						break
					}
					if printAllErrors {
						fmt.Println("Error Sending request:", err)
					}
					globalErr = err
					r.errors.Add(1)
					time.Sleep(dialTimeout)
					continue
				}

				done := time.Since(sent).Milliseconds()

				r.m.Lock()
				if done > r.highestTransferMS {
					r.highestTransferMS = done
				}

				if done < r.lowestTransferMS {
					r.lowestTransferMS = done
				}
				r.m.Unlock()

				io.Copy(io.Discard, resp.Body)
				resp.Body.Close()

				if resp.StatusCode != http.StatusOK {
					err = errors.New("Status code was not OK: " + resp.Status)
					if printAllErrors {
						fmt.Println(err)
					}

					globalErr = err
					r.errors.Add(1)
					time.Sleep(dialTimeout)
					continue
				}
			}
		}()
	}

	wg.Wait()
}

func getNetWriter(index string) *netperfWriter {
	indexInt, err := strconv.Atoi(index)
	if err != nil {
		globalErr = errors.New("index is not a number")
		return nil
	}
	if indexInt > len(Writers) {
		globalErr = errors.New("writer index is bigger then len")
		return nil
	}

	return Writers[indexInt]
}

func createNetWriter(host string) string {
	defer WriterLock.Unlock()
	WriterLock.Lock()
	for i, v := range Writers {
		if v != nil {
			continue
		}

		Writers[i] = new(netperfWriter)
		Writers[i].buf = make([]byte, payloadMB*humanize.MiByte)
		Writers[i].host = host
		return strconv.Itoa(i)
	}

	return ""
}

var (
	start            = time.Now()
	longLatencyTest  bool
	shortLatencyTest bool

	port           int
	proc           int
	payloadMB      int
	bufferKB       int
	seconds        int
	sleep          int
	startTimeout   int
	stream         bool
	printJSON      bool
	resolveHosts   string
	latency        bool
	clearTable     bool
	serveIP        string
	hosts          string
	activeClients  atomic.Int64
	activeRequests atomic.Int64
	dialTimeout    = 1 * time.Second

	currentHost    string
	httpListener   net.Listener
	Readers        []*netperfReader
	Writers        []*netperfWriter
	WriterLock     = sync.Mutex{}
	globalErr      error
	printAllErrors bool
)

func main() {
	flag.BoolVar(&longLatencyTest, "longLatencyTest", false, "60 second latency test")
	flag.BoolVar(&shortLatencyTest, "shortLatencyTest", false, "10 minute latency test")

	flag.BoolVar(&clearTable, "clearTable", true, "Clear table between updates")
	flag.BoolVar(&latency, "latency", false, "Do latency testing only")
	flag.BoolVar(&stream, "stream", true, "Only use a single client per remote host")
	flag.BoolVar(&printJSON, "json", false, "print as json output")
	flag.IntVar(&port, "port", 9999, "serve port")
	flag.IntVar(&sleep, "sleep", 1, "Timeout between requests in Milliseconds")
	flag.IntVar(&startTimeout, "startTimeout", 10, "Timeout before testing starts")
	flag.IntVar(&bufferKB, "bufferKB", 64, "TX/RX buffer size in Kilobytes")
	flag.IntVar(&payloadMB, "payloadMB", 1, "Payload buffer size in Megabytes")
	flag.StringVar(&serveIP, "serveIP", "", "IP to listen for requests on, only needed if two IPs are on the same host. (mostly used for local testing)")
	flag.StringVar(&hosts, "hosts", "file:./hosts", "Define servers using an ellipses range: '1.1.1.{1...3},2.2.2.{1...3}' \nYou can also use IP's: '1.1.1.1,2.2.2.1'\nOr a host file: 'file:./hosts'\nWhen using a file, hosts can be comma seperate (with no spacing) or host per line. \nYou can even mix hostnames and IP's together in the file.\n")
	flag.StringVar(&resolveHosts, "resolveHosts", "", "Resolve hosts using the given DNS server.")
	flag.IntVar(&proc, "proc", 32, "Concurrent requests per host")
	flag.IntVar(&seconds, "seconds", 15, "how long (Seconds) to run hperf")
	flag.BoolVar(&printAllErrors, "printErrors", false, "Print all errors in real time")
	flag.Parse()

	if latency {
		stream = false
	}

	if shortLatencyTest {
		latency = true
		startTimeout = 30
		stream = false
		sleep = 100
		bufferKB = 16
		payloadMB = 0
		proc = 1
		seconds = 60
	}

	if longLatencyTest {
		latency = true
		startTimeout = 30
		stream = false
		sleep = 100
		bufferKB = 16
		payloadMB = 0
		proc = 1
		seconds = 600
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	addressMap := make(map[string]struct{})
	interfaces, _ := net.Interfaces()
	for _, intf := range interfaces {
		addrs, _ := intf.Addrs()
		for _, addr := range addrs {
			sa := strings.Split(addr.String(), "/")
			addressMap[sa[0]] = struct{}{}
		}
	}

	hostMap := make(map[string]struct{}, 0)

	if strings.Contains(hosts, "file:") {

		fs := strings.Split(hosts, ":")
		if len(fs) < 2 {
			fmt.Println("When using a file for hosts, please use the format( file:path ) example( file:~/hosts.txt )")
			fmt.Println("Hosts within the file should be per line or comma seperated")
			os.Exit(1)
		}
		hb, err := os.ReadFile(fs[1])
		if err != nil {
			fmt.Println("Could not open file:", fs[1])
			os.Exit(1)
		}

		// this is just to trip out carrage return
		hb = bytes.Replace(hb, []byte{13}, []byte{}, -1)

		var splitLines [][]byte
		if bytes.Contains(hb, []byte(",")) {
			splitLines = bytes.Split(hb, []byte(","))
		} else if bytes.Contains(hb, []byte{10}) {
			splitLines = bytes.Split(hb, []byte{10})
		}
		if len(splitLines) < 1 {
			fmt.Println("Hosts within the file (", fs[1], ") should be per line or comma seperated")
			os.Exit(1)
		}

		for _, v := range splitLines {
			// to account to accidental empty lines or commas
			if len(v) == 0 {
				continue
			}
			hostMap[string(v)] = struct{}{}
		}

	} else {

		splitHosts := strings.Split(hosts, ",")
		hostList := make([]ellipses.ArgPattern, 0)
		for _, v := range splitHosts {
			if !ellipses.HasEllipses(v) {
				hostMap[v] = struct{}{}
				continue
			}

			x, e := ellipses.FindEllipsesPatterns(v)
			if e != nil {
				fmt.Println(e)
				os.Exit(1)
			}
			hostList = append(hostList, x)
		}

		for _, host := range hostList {
			for _, pattern := range host {
				for _, seq := range pattern.Seq {
					hostMap[pattern.Prefix+seq] = struct{}{}
				}
			}
		}

	}

	for host := range hostMap {
		if net.ParseIP(host) == nil && resolveHosts != "" {
			ips, err := net.LookupIP(host)
			if err != nil {
				fmt.Println("Could not look up ", host, ", err :", err)
				os.Exit(1)
			}
			if len(ips) == 0 {
				fmt.Println("Could not look up ", host, ", err: did not find any IPs on record")
				os.Exit(1)
			}

			hostMap[ips[0].String()] = struct{}{}
			delete(hostMap, host)
			continue
		}
	}

	var serverRunningOnce sync.Once
	serverStarted := false

	if serveIP != "" {
		serverRunningOnce.Do(func() {
			currentHost = serveIP
			serverStarted = true
			go runServer(serveIP)
		})
	}

	Readers = make([]*netperfReader, len(hostMap)+1)
	Writers = make([]*netperfWriter, len(hostMap)+1)

	time.Sleep(time.Duration(startTimeout) * time.Second)

	for host := range hostMap {
		if !serverStarted {
			_, ok := addressMap[host]
			if ok {
				serverRunningOnce.Do(func() {
					currentHost = host
					serverStarted = true
					go runServer(host)
				})
				continue
			}
		} else if serverStarted && host == serveIP {
			continue
		}

		for i, v := range Readers {
			if v == nil {
				Readers[i] = NewNetPerfReader(host)
				go runClient(ctx, Readers[i])
				break
			}
		}

	}

	if !serverStarted {
		fmt.Println("None of the given IP Addresses can be found on this server")
		os.Exit(1)
	}

	printDataOut(ctx, cancel)
}
