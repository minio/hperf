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

package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math"
	"net"
	"net/http"
	"os"
	"runtime"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gofiber/contrib/websocket"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/minio/hperf/shared"
	"github.com/shirou/gopsutil/cpu"
	"github.com/shirou/gopsutil/mem"
)

var (
	httpServer = fiber.New(fiber.Config{
		StreamRequestBody:     true,
		ServerHeader:          "hperf",
		AppName:               "hperf",
		DisableStartupMessage: true,
		ReadBufferSize:        1000000,
		WriteBufferSize:       1000000,
	})
	bindAddress      = "0.0.0.0:9000"
	realIP           = ""
	testFolderSuffix = "hperf-tests"
	basePath         = "./"
	tests            = make([]*test, 0)
	testLock         = sync.Mutex{}
)

type test struct {
	ID      string
	Config  shared.Config
	Started time.Time

	ctx    context.Context
	cancel context.CancelCauseFunc

	Readers  []*netPerfReader
	errors   []shared.TError
	errMap   map[string]struct{}
	errIndex atomic.Int32
	DPS      []shared.DP
	M        sync.Mutex

	DataFile      *os.File
	DataFileIndex int
	cons          map[string]*websocket.Conn
}

func (t *test) AddError(err error, id string) {
	t.M.Lock()
	defer t.M.Unlock()
	if err == nil {
		return
	}
	_, ok := t.errMap[id]
	if ok {
		return
	}
	if t.Config.Debug {
		fmt.Println("ERR:", err)
	}
	t.errors = append(t.errors, shared.TError{Error: err.Error(), Created: time.Now()})
	t.errMap[id] = struct{}{}
}

func RunServer(ctx context.Context, address string, rIP string, storagePath string) (err error) {
	cancelContext, cancel := context.WithCancel(ctx)
	defer cancel()

	if storagePath == "" {
		basePath, err = os.Getwd()
		if err != nil {
			return
		}
	} else {
		basePath = storagePath
		err = os.MkdirAll(storagePath, 0o777)
		if err != nil {
			return err
		}
	}
	shared.DEBUG("Storage path:", storagePath)

	if basePath[len(basePath)-1] != byte(os.PathSeparator) {
		basePath += string(os.PathSeparator) + testFolderSuffix + string(os.PathSeparator)
	} else {
		basePath += testFolderSuffix + string(os.PathSeparator)
	}
	shared.DEBUG("Base path:", basePath)

	err = os.MkdirAll(basePath, 0o777)
	if err != nil {
		return err
	}

	bindAddress = address
	realIP = rIP
	shared.INFO("starting 'hperf' server on:", bindAddress)
	err = startAPIandWS(cancelContext)
	if err != nil {
		return
	}

	return nil
}

func startAPIandWS(ctx context.Context) (err error) {
	httpServer.Get("/", func(c *fiber.Ctx) error {
		return c.SendString("Hello, World!")
	})

	httpServer.Use("/ws", func(c *fiber.Ctx) error {
		if websocket.IsWebSocketUpgrade(c) {
			c.Locals("allowed", true)
			return c.Next()
		}
		return fiber.ErrUpgradeRequired
	})

	httpServer.Get("/ws/:id", websocket.New(func(con *websocket.Conn) {
		var (
			msg []byte
			err error
		)

		err = SendPing(con)
		if err != nil {
			shared.DEBUG("Error accepting client socket:", err)
			if con != nil {
				con.Close()
			}
			return
		}

		for {
			if ctx.Err() != nil {
				shared.DEBUG("Ctx done, closing websocket read loop:", err)
				return
			}
			if _, msg, err = con.ReadMessage(); err != nil {
				shared.DEBUG("Error reading websocket message:", err)
				break
			}

			signal := new(shared.WebsocketSignal)
			err := json.Unmarshal(msg, signal)
			if err != nil {
				if signal.Config.Debug {
					log.Println("Unable to parse signal:", err)
				}
				continue
			}
			if signal.Config.Debug {
				fmt.Printf("WebsocketSignal: %+v\n", signal)
			}

			switch signal.SType {
			case shared.RunTest:
				go createAndRunTest(con, *signal)
			case shared.ListenTest:
				go listenToLiveTests(con, *signal)
			case shared.ListTests:
				go listAllTests(con, *signal)
			case shared.GetTest:
				go getTestOnServer(con, *signal)
			case shared.Ping:
				go replyToPing(con)
			case shared.DeleteTests:
				go deleteTestsFromDisk(con, *signal)
			case shared.StopAllTests:
				go stopAllTests(con, *signal)
			case shared.Exit:
				os.Exit(1)
			default:
				if signal.Config.Debug {
					fmt.Println("unrecognized command")
				}
			}

		}
	}))

	httpServer.Put("/latency", func(c *fiber.Ctx) error {
		io.Copy(io.Discard, bytes.NewBuffer(c.Body()))
		return c.SendStatus(200)
	})

	httpServer.Put("/bandwidth", func(c *fiber.Ctx) error {
		io.Copy(io.Discard, c.Request().BodyStream())
		return c.SendStatus(200)
	})

	go func() {
		err = httpServer.Listen(bindAddress)
		if err != nil {
			fmt.Println(err)
		}
	}()

	routineMonitor <- 1

	for {
		select {
		case id := <-routineMonitor:
			if id == 1 {
				go getServerStats(id)
			}
		default:
		}
		if ctx.Err() != nil {
			httpServer.Shutdown()
			return
		}
		time.Sleep(1 * time.Second)
	}
}

var (
	currentMemoryStat *mem.VirtualMemoryStat
	droppedPackets    int
	cpuPercent        float64
)

func getServerStats(id byte) {
	defer func() {
		r := recover()
		if r != nil {
			log.Println(r, string(debug.Stack()))
		}
		time.Sleep(1 * time.Second)
		routineMonitor <- id
	}()

	var err error
	currentMemoryStat, err = mem.VirtualMemory()
	if err != nil {
		fmt.Println(err)
	}

	droppedPackets, err = GetDroppedPackets()
	if err != nil {
		fmt.Println(err)
	}
	percent, err := cpu.Percent(time.Second, false)
	if err != nil {
		fmt.Println(err)
	}
	if len(percent) > 0 {
		cpuPercent = percent[0]
	}
}

func GetDroppedPackets() (total int, err error) {
	if runtime.GOOS != "linux" {
		return 0, nil
	}
	data, err := os.ReadFile("/proc/net/dev")
	if err != nil {
		return 0, err
	}
	lines := strings.Split(string(data), "\n")
	for _, line := range lines[2:] {
		fields := strings.Fields(line)
		if len(fields) < 5 {
			continue
		}
		dropped, err := strconv.Atoi(fields[4]) // Field 4 is for dropped packets
		if err != nil {
			return 0, err
		}
		total += dropped
	}
	return
}

var routineMonitor = make(chan byte, 100)

func replyToPing(c *websocket.Conn) {
	msg := new(shared.WebsocketSignal)
	msg.SType = shared.Pong
	_ = c.WriteJSON(msg)
}

func SendError(c *websocket.Conn, e error) error {
	if e == nil {
		return nil
	}
	msg := new(shared.WebsocketSignal)
	msg.SType = shared.Err
	msg.Error = e.Error()
	return c.WriteJSON(msg)
}

func stopAllTests(con *websocket.Conn, s shared.WebsocketSignal) {
	defer SendDone(con)
	for i := range tests {
		if s.Config.TestID != "" && s.Config.TestID != tests[i].ID {
			continue
		}
		if s.Config.Debug {
			fmt.Println("Stopping:", tests[i].ID)
		}
		tests[i].cancel(fmt.Errorf("Client called StopAllTests"))
	}
}

func SendPing(c *websocket.Conn) error {
	msg := new(shared.WebsocketSignal)
	msg.SType = shared.Ping
	msg.Code = shared.OK
	return c.WriteJSON(msg)
}

func SendOK(c *websocket.Conn, t shared.SignalType) error {
	msg := new(shared.WebsocketSignal)
	msg.SType = t
	msg.Code = shared.OK
	return c.WriteJSON(msg)
}

func SendDone(c *websocket.Conn) error {
	msg := new(shared.WebsocketSignal)
	msg.SType = shared.Done
	msg.Code = shared.OK
	return c.WriteJSON(msg)
}

func newTest(c *shared.Config) (t *test, err error) {
	testLock.Lock()
	defer testLock.Unlock()

	t = new(test)
	t.errMap = make(map[string]struct{})
	t.cons = make(map[string]*websocket.Conn)
	t.Started = time.Now()
	t.Config = *c
	t.DPS = make([]shared.DP, 0)
	t.ID = c.TestID
	t.ctx, t.cancel = context.WithCancelCause(context.Background())

	if c.Save {
		resetTestFiles(t)
		newTestFile(t)
	}

	t.Readers = make([]*netPerfReader, 0)
	readersCreated := 0

	for i := range c.Hosts {

		joinedHostPort := net.JoinHostPort(c.Hosts[i], c.Port)
		if realIP != "" && strings.Contains(joinedHostPort, realIP) {
			continue
		}
		if joinedHostPort == bindAddress {
			continue
		}
		t.Readers = append(t.Readers,
			newPerformanceReaderForASingleHost(c, c.Hosts[i], c.Port),
		)
		readersCreated++

	}

	if readersCreated == 0 {
		return nil, fmt.Errorf("No performance readers were created, please revise your config")
	}

	tests = append(tests, t)
	return t, nil
}

type netPerfReader struct {
	hasStats bool
	m        sync.Mutex

	buf []byte

	addr   string
	ip     string
	client *http.Client

	TXCount atomic.Uint64
	TX      atomic.Uint64

	concurrency chan int

	TTFBH int64
	TTFBL int64
	RMSH  int64
	RMSL  int64

	lastDataPointTime time.Time
}

type asyncReader struct {
	pr             *netPerfReader
	i              int64 // current reading index
	prevRune       int   // index of previous rune; or < 0
	ttfbRegistered bool
	start          time.Time
	ctx            context.Context
	c              *shared.Config
}

func (a *asyncReader) Read(b []byte) (n int, err error) {
	a.pr.m.Lock()
	if !a.ttfbRegistered {
		since := time.Since(a.start).Microseconds()
		a.ttfbRegistered = true
		if since > a.pr.TTFBH {
			a.pr.TTFBH = since
		}
		if since < a.pr.TTFBL {
			a.pr.TTFBL = since
		}
	}
	a.pr.hasStats = true
	a.pr.m.Unlock()

	if a.ctx.Err() != nil {
		return 0, io.EOF
	}

	if a.c.TestType == shared.BandwidthTest {
		n = copy(b, a.pr.buf)
		a.pr.TX.Add(uint64(n))
		return n, nil
	}

	if a.i >= int64(len(a.pr.buf)) {
		return 0, io.EOF
	}
	n = copy(b, a.pr.buf[a.i:])
	a.i += int64(n)
	a.pr.TX.Add(uint64(n))
	return n, nil
}

func createAndRunTest(con *websocket.Conn, signal shared.WebsocketSignal) {
	defer SendDone(con)

	test, err := newTest(signal.Config)
	if err != nil {
		SendError(con, err)
		return
	}
	if signal.Config.Debug {
		defer func() {
			fmt.Println("Test exiting:", test.ID)
		}()
	}
	defer test.cancel(fmt.Errorf("testing finished"))

	start := time.Now()
	for i := range test.Readers {
		go startPerformanceReader(test, test.Readers[i])
	}

	conUID := uuid.NewString()
	test.cons[conUID] = con

	for {
		if test.ctx.Err() != nil {
			return
		}

		if time.Since(start).Seconds() > float64(test.Config.Duration) {
			break
		}
		time.Sleep(1 * time.Second)
		if signal.Config.Debug {
			fmt.Println("Duration: ", signal.Config.TestID, time.Since(start).Seconds())
		}

		generateDataPoints(test)
		_ = sendAndSaveData(test)
	}
}

func listenToLiveTests(con *websocket.Conn, s shared.WebsocketSignal) {
	uid := uuid.NewString()

	for i := range tests {
		if s.Config.TestID != "" && tests[i].ID != s.Config.TestID {
			continue
		}
		if s.Config.Debug {
			fmt.Println("Listen:", tests[i].ID, "DPS:", len(tests[i].DPS), "ERR:", len(tests[i].errors))
		}

		tests[i].cons[uid] = con
	}
}

type DataPointPaginator struct {
	DPIndex  int
	ErrIndex int
	After    time.Time
}

func sendAndSaveData(t *test) (err error) {
	defer func() {
		r := recover()
		if r != nil {
			log.Println(r, string(debug.Stack()))
		}
	}()

	wss := new(shared.WebsocketSignal)
	wss.SType = shared.Stats
	wss.DataPoint = new(shared.DataReponseToClient)

	if t.DataFile == nil && t.Config.Save {
		newTestFile(t)
	}

	for i := range t.DPS {
		wss.DataPoint.DPS = append(wss.DataPoint.DPS, t.DPS[i])
		if t.Config.Save {
			fileb, err := json.Marshal(t.DPS[i])
			if err != nil {
				t.AddError(err, "datapoint-marshaling")
			}
			t.DataFile.Write(shared.DataPoint.String())
			t.DataFile.Write(fileb)
			t.DataFile.Write([]byte{10})
		}
	}
	t.DPS = make([]shared.DP, 0)

	t.M.Lock()
	errorsClone := make([]shared.TError, 0)
	for _, v := range t.errors {
		errorsClone = append(errorsClone, v)
	}
	t.errors = make([]shared.TError, 0)
	t.errMap = make(map[string]struct{})
	t.M.Unlock()

	for i := range errorsClone {
		wss.DataPoint.Errors = append(wss.DataPoint.Errors, errorsClone[i])
		if t.Config.Save {
			fileb, err := json.Marshal(errorsClone[i])
			if err != nil {
				t.AddError(err, "error-marshaling")
			}
			t.DataFile.Write(shared.ErrorPoint.String())
			t.DataFile.Write(fileb)
			t.DataFile.Write([]byte{10})
		}
	}

	for i := range t.cons {
		if t.cons[i] == nil {
			continue
		}

		err = t.cons[i].WriteJSON(wss)
		if err != nil {
			if t.Config.Debug {
				fmt.Println("Unable to send data point:", err)
			}
			t.cons[i].Close()
			delete(t.cons, i)
			continue
		}
	}
	return
}

func generateDataPoints(t *test) {
	for ri, rv := range t.Readers {
		if rv == nil {
			continue
		}

		if !rv.hasStats {
			continue
		}

		r := t.Readers[ri]

		tx := r.TX.Swap(0)
		totalSecs := time.Since(r.lastDataPointTime).Seconds()
		r.lastDataPointTime = time.Now()
		txtotal := float64(tx) / totalSecs

		d := shared.DP{
			Type:              t.Config.TestType,
			TestID:            t.ID,
			Created:           time.Now(),
			TX:                uint64(txtotal),
			TXTotal:           tx,
			TXCount:           r.TXCount.Load(),
			Remote:            r.addr,
			TTFBL:             r.TTFBL,
			TTFBH:             r.TTFBH,
			RMSL:              r.RMSL,
			RMSH:              r.RMSH,
			ErrCount:          len(t.errors),
			DroppedPackets:    droppedPackets,
			MemoryUsedPercent: int(currentMemoryStat.UsedPercent),
			CPUUsedPercent:    int(cpuPercent),
		}

		if realIP != "" {
			d.Local = realIP
		} else {
			d.Local = bindAddress
		}

		r.m.Lock()
		r.hasStats = false
		r.TTFBH = 0
		r.TTFBL = math.MaxInt64
		r.RMSH = 0
		r.RMSL = math.MaxInt64
		r.m.Unlock()

		t.DPS = append(t.DPS, d)
	}
	return
}

func newTransport(c *shared.Config) *http.Transport {
	return &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           newDialContext(10 * time.Second),
		MaxIdleConnsPerHost:   1024,
		WriteBufferSize:       c.BufferKB,
		ReadBufferSize:        c.BufferKB,
		IdleConnTimeout:       15 * time.Second,
		ResponseHeaderTimeout: 15 * time.Minute,
		TLSHandshakeTimeout:   10 * time.Second,
		DisableCompression:    true,
	}
}

func newDialContext(dialTimeout time.Duration) dialContext {
	d := &net.Dialer{
		Timeout: dialTimeout,
		Control: setTCPParametersFn(),
	}
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		return d.DialContext(ctx, network, addr)
	}
}

// DialContext is a function to make custom Dial for internode communications
type dialContext func(ctx context.Context, network, address string) (net.Conn, error)

func newPerformanceReaderForASingleHost(c *shared.Config, host string, port string) (r *netPerfReader) {
	r = new(netPerfReader)
	r.lastDataPointTime = time.Now()
	r.addr = net.JoinHostPort(host, port)
	r.ip = host
	r.buf = make([]byte, c.PayloadSize)
	r.TTFBL = math.MaxInt64
	r.RMSL = math.MaxInt64
	r.client = &http.Client{
		Transport: newTransport(c),
	}
	r.concurrency = make(chan int, c.Concurrency)
	for i := 1; i <= c.Concurrency; i++ {
		r.concurrency <- i
	}
	return
}

func startPerformanceReader(t *test, r *netPerfReader) {
	for {
		var cid int
		select {
		case cid = <-r.concurrency:
			go sendRequestToHost(t, r, cid)
		case _ = <-t.ctx.Done():
			return
		}
	}
}

func sendRequestToHost(t *test, r *netPerfReader, cid int) {
	defer func() {
		rec := recover()
		if rec != nil {
			log.Println(rec, string(debug.Stack()))
		}
		r.concurrency <- cid
	}()

	if t.Config.RequestDelay > 0 {
		time.Sleep(time.Duration(t.Config.RequestDelay) * time.Millisecond)
	}

	if t.ctx.Err() != nil {
		return
	}

	AR := new(asyncReader)
	AR.ctx = t.ctx
	AR.pr = r
	AR.c = &t.Config
	AR.start = time.Now()

	var req *http.Request
	var resp *http.Response
	var err error

	proto := "https://"
	if t.Config.Insecure {
		proto = "http://"
	}

	route := "/404"
	var body io.Reader
	method := http.MethodGet
	switch t.Config.TestType {
	case shared.BandwidthTest:
		route = "/bandwidth"
		body = io.NopCloser(AR)
		method = http.MethodPut
	case shared.LatencyTest:
		method = http.MethodPut
		route = "/latency"
		body = AR
	default:
		t.AddError(fmt.Errorf("Unknown test type: %d", t.Config.TestType), "unknown-signal")
	}

	req, err = http.NewRequestWithContext(
		t.ctx,
		method,
		proto+r.addr+route,
		body,
	)
	if err != nil {
		t.AddError(err, "network-new-request")
		return
	}

	if t.Config.TestType == shared.BandwidthTest {
		req.ContentLength = -1
	}

	sent := time.Now()
	r.TXCount.Add(1)
	resp, err = r.client.Do(req)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return
		}
		t.AddError(err, "network-error")
		return
	}

	if resp.StatusCode != http.StatusOK {
		t.AddError(fmt.Errorf("Status code was %d, expected 200 from host %s", resp.StatusCode, r.addr), "invalid-status-code")
		return
	}

	done := time.Since(sent).Microseconds()

	r.m.Lock()
	if done > r.RMSH {
		r.RMSH = done
	}

	if done < r.RMSL {
		r.RMSL = done
	}
	r.hasStats = true
	r.m.Unlock()

	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()

	return
}

func listAllTests(con *websocket.Conn, s shared.WebsocketSignal) {
	defer SendDone(con)

	var err error
	s.TestList, err = listTestsFromDisk()
	if err != nil {
		SendError(con, err)
		return
	}

	s.Code = 200
	s.SType = shared.ListTests
	err = con.WriteJSON(s)
	if err != nil {
		fmt.Println(err)
	}
}

func getTestOnServer(con *websocket.Conn, s shared.WebsocketSignal) {
	defer SendDone(con)
	err := streamTestFilesToWebsocket(con, s.Config.TestID)
	if err != nil {
		SendError(con, err)
	}
}

func sendAllDataPoints(con *websocket.Conn, t *test) error {
	wss := new(shared.WebsocketSignal)
	wss.SType = shared.Stats
	dataResponse := new(shared.DataReponseToClient)

	for i := range t.DPS {
		dataResponse.DPS = append(dataResponse.DPS, t.DPS[i])
	}

	for i := range t.errors {
		dataResponse.Errors = append(dataResponse.Errors, t.errors[i])
	}

	wss.DataPoint = dataResponse
	err := con.WriteJSON(wss)
	if err != nil {
		return err
	}
	return nil
}
