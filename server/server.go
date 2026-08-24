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
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math"
	"net"
	"net/http"
	"net/netip"
	"os"
	"runtime"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	fwebsocket "github.com/fasthttp/websocket"
	"github.com/gofiber/contrib/websocket"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/minio/hperf/shared"
	"github.com/shirou/gopsutil/cpu"
	"github.com/shirou/gopsutil/mem"
)

const (
	// fasthttp allocates a bufio.Reader and bufio.Writer of these sizes for
	// every concurrent connection, so they are a per-connection memory cost,
	// not a one-off. At 1 MB each they cost ~1 MiB of RSS per inbound
	// connection, and a full mesh opens (hosts-1) * concurrency of them:
	// 8064 connections, ~8 GiB, on 64 hosts at the default concurrency.
	//
	// The only hard requirement is that the largest inbound request *header*
	// fits in the read buffer, otherwise fasthttp answers 431. hperf's
	// requests are PUT /stream and PUT /requests plus the websocket upgrade,
	// all of which carry a handful of small headers; --payload-size affects
	// the body, which is streamed and never buffered whole. 64 KiB leaves an
	// order of magnitude of headroom over anything hperf generates.
	serverReadBufferSize  = 64 * 1024
	serverWriteBufferSize = 64 * 1024

	// A client that stops reading must not be able to stall the test it is
	// attached to, so every write to a client socket is bounded.
	wsWriteTimeout = 5 * time.Second

	// minSampleWindow is the shortest interval that produces a meaningful
	// rate. Anything shorter is dropped rather than divided out; see
	// generateDataPoints.
	minSampleWindow = 100 * time.Millisecond
)

var (
	httpServer = fiber.New(fiber.Config{
		Network:               fiber.NetworkTCP,
		StreamRequestBody:     true,
		ServerHeader:          "hperf",
		AppName:               "hperf",
		DisableStartupMessage: true,
		ReadBufferSize:        serverReadBufferSize,
		WriteBufferSize:       serverWriteBufferSize,
	})
	bindAddress      = "0.0.0.0:9000"
	realIP           = ""
	testFolderSuffix = "hperf-tests"
	basePath         = "./"
	tests            = make([]*test, 0)
	testLock         = sync.Mutex{}
)

// wsPeer owns one client websocket, for two reasons.
//
// First, the *websocket.Conn handed to the /ws/:id handler is a pooled
// wrapper: gofiber/contrib/websocket assigns conn.Conn = fconn and, when the
// handler returns, releaseConn nils that field and puts the wrapper back in a
// sync.Pool for the next upgrade to claim. Tests deliberately outlive their
// client, so retaining the wrapper meant a later write either nil-dereferenced
// (silently, into the recover() in sendAndSaveData, once a second forever) or
// landed in an unrelated client's socket. We keep the inner conn, which is
// allocated fresh per upgrade and never pooled.
//
// Second, a test's sampling loop and any number of command handlers can target
// the same socket at once, and fasthttp/websocket panics on concurrent writes
// to the data path. wmu serializes them. It is a leaf lock: never acquire t.M
// or testLock while holding it. Control frames stay off it deliberately --
// WriteControl serializes on its own channel mutex and the library's ping
// handler replies from the read loop, so routing pongs through wmu would queue
// them behind a slow data write and break liveness detection.
type wsPeer struct {
	pid  string
	con  *fwebsocket.Conn
	wmu  sync.Mutex
	dead atomic.Bool
}

func newPeer(c *websocket.Conn) *wsPeer {
	return &wsPeer{pid: uuid.NewString(), con: c.Conn}
}

func (p *wsPeer) writeJSON(v any) error {
	if p == nil || p.con == nil || p.dead.Load() {
		return net.ErrClosed
	}
	p.wmu.Lock()
	defer p.wmu.Unlock()
	if p.dead.Load() {
		return net.ErrClosed
	}
	if err := p.con.SetWriteDeadline(time.Now().Add(wsWriteTimeout)); err != nil {
		return err
	}
	return p.con.WriteJSON(v)
}

// retire marks the peer unusable and waits for any write already in progress.
// It is called when the owning handler returns, which is when fasthttp reclaims
// the hijacked connection underneath us and puts it back in its pool -- so a
// write still inside con.WriteJSON at that moment would either hit a nil'd
// embedded conn or land in whichever connection claims the pooled object next.
// Taking the write mutex guarantees nobody is inside the conn once this returns.
func (p *wsPeer) retire() {
	if p == nil {
		return
	}
	p.wmu.Lock()
	p.dead.Store(true)
	p.wmu.Unlock()
}

func (p *wsPeer) shutdown() {
	if p == nil {
		return
	}
	p.wmu.Lock()
	p.dead.Store(true)
	if p.con != nil {
		_ = p.con.Close()
	}
	p.wmu.Unlock()
}

// Locking contract for test, and the reason it is written down: the fields
// below are touched by the sampling goroutine, by every request goroutine via
// AddError, and by websocket handlers attaching clients.
//
//  1. testLock guards the package-level tests slice, and nothing else.
//  2. t.M guards errors, errMap, DPS and cons.
//  3. endedAt (UnixNano, 0 while running) carries liveness, so code that only
//     needs to know whether a test is finished does not have to take t.M.
//  4. Readers, DataFile, DataFileIndex and netPerfReader.lastDataPointTime are
//     owned by the single sampling goroutine, which is also the goroutine that
//     runs finish(), and need no lock. Request goroutines hold their own
//     *netPerfReader pointers, so those structs must never be mutated from
//     here -- drop the slice reference instead and let GC reclaim them.
//  5. netPerfReader.m guards only TTFBH/TTFBL/RMSH/RMSL. hasStats is atomic.
//  6. No lock nesting: testLock, t.M and wsPeer.wmu are never held together.
//     Iterators snapshot under one lock, release it, then do the work.
//  7. No blocking work -- socket write, disk write, fmt.Println -- while t.M is
//     held. This is a deadlock rule as much as a stall rule: sync.Mutex is not
//     reentrant and AddError takes t.M, so persisting under t.M would deadlock
//     the moment a write failed and reported itself as an error.
type test struct {
	ID      string
	Config  shared.Config
	Started time.Time

	ctx    context.Context
	cancel context.CancelCauseFunc

	Readers []*netPerfReader
	errors  []shared.TError
	errMap  map[string]struct{}
	DPS     []shared.DP
	M       sync.Mutex

	endedAt atomic.Int64

	// dropped holds the interface drop counters sampled when the test began,
	// so data points can report drops accumulated by this test rather than
	// everything since boot.
	dropped dropCounters

	DataFile      *os.File
	DataFileIndex int
	cons          map[string]*wsPeer
}

func (t *test) live() bool { return t.endedAt.Load() == 0 }

func (t *test) finish() {
	if !t.endedAt.CompareAndSwap(0, time.Now().UnixNano()) {
		return
	}

	// Clearing cons stops the test writing to its clients, but the peers are
	// deliberately NOT retired here: retiring is the owning handler's job, and
	// createAndRunTest still has to send Done over the peer that started the
	// test after this returns.
	t.M.Lock()
	t.cons = make(map[string]*wsPeer)
	t.M.Unlock()

	// Reclaim the per-reader payload buffers and transports. A finished test
	// used to keep one PayloadSize buffer and one http.Client per peer alive
	// for the lifetime of the server -- 63 MB per run at 64 hosts with the
	// default 1 MB payload. The test object itself stays in the slice so it
	// remains listable and downloadable.
	//
	// Dropping the reference is what reclaims them. Zeroing fields on the
	// readers instead would race: request goroutines hold their own pointers
	// and may still be inside Read, copying from r.buf, when this runs.
	readers := t.Readers
	t.Readers = nil

	for _, r := range readers {
		if r != nil && r.client != nil {
			r.client.CloseIdleConnections()
		}
	}
}

// attach registers a client socket with a running test. It reports false if the
// test has already finished, so callers can avoid handing a socket to a test
// that will never write to it again.
func (t *test) attach(p *wsPeer) bool {
	if !t.live() {
		return false
	}
	t.M.Lock()
	defer t.M.Unlock()
	if t.cons == nil {
		return false
	}
	t.cons[p.pid] = p
	return true
}

func (t *test) detach(ids []string) {
	if len(ids) == 0 {
		return
	}
	t.M.Lock()
	defer t.M.Unlock()
	for _, id := range ids {
		delete(t.cons, id)
	}
}

func (t *test) AddError(err error, id string) {
	if err == nil {
		return
	}
	debugOn := t.Config.Debug

	t.M.Lock()
	if _, ok := t.errMap[id]; ok {
		t.M.Unlock()
		return
	}
	t.errors = append(t.errors, shared.TError{
		Error:   shared.TruncateError(err.Error()),
		Created: time.Now(),
	})
	t.errMap[id] = struct{}{}
	t.M.Unlock()

	// Printing is I/O and must not happen under t.M.
	if debugOn {
		fmt.Println("ERR:", err)
	}
}

func RunServer(ctx context.Context, address string, rIP string, storagePath string) (err error) {
	cancelContext, cancel := context.WithCancel(ctx)
	defer cancel()

	if storagePath == "" {
		basePath, err = os.Getwd()
		if err != nil {
			return err
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
	statsInterface = resolveStatsInterface()
	if statsInterface != "" {
		shared.DEBUG("Reporting drop counters for interface:", statsInterface)
	} else {
		shared.DEBUG("Could not resolve an interface for drop counters, summing all non-loopback interfaces")
	}
	shared.INFO("starting 'hperf' server on:", bindAddress)
	err = startAPIandWS(cancelContext)
	if err != nil {
		return err
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

		// The wrapper con is pooled and reused by the next upgrade once this
		// handler returns, so anything outliving the handler must hold the peer
		// instead, and the peer must be retired here.
		peer := newPeer(con)
		defer peer.retire()

		err = SendPing(peer)
		if err != nil {
			shared.DEBUG("Error accepting client socket:", err)
			peer.shutdown()
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
				log.Println("Unable to parse signal:", err)
				continue
			}
			if signal.Config.Debug {
				fmt.Printf("WebsocketSignal: %+v\n", signal)
			}

			// These run inline rather than in their own goroutine. Spawning
			// meant several handlers could target one socket concurrently and
			// that a client could not tell when its command had been accepted;
			// the read loop is per-connection already, so serializing here
			// costs nothing but removes a whole class of interleaving.
			switch signal.SType {
			case shared.RunTest:
				createAndRunTest(peer, *signal)
			case shared.ListenTest:
				listenToLiveTests(peer, *signal)
			case shared.ListTests:
				listAllTests(peer, *signal)
			case shared.GetTest:
				getTestOnServer(peer, *signal)
			case shared.Ping:
				replyToPing(peer)
			case shared.DeleteTests:
				deleteTestsFromDisk(peer, *signal)
			case shared.StopAllTests:
				stopAllTests(peer, *signal)
			case shared.Exit:
				os.Exit(1)
			default:
				shared.DEBUG("unrecognized command:", signal.SType)
			}

		}
	}))

	// Both bodies are drained straight to Discard without being materialized.
	// /requests used to call c.Body(), which buffers the whole payload in
	// memory for every in-flight request: at the default 1 MB payload that is
	// (hosts-1) * concurrency megabytes of garbage on the receive path.
	httpServer.Put("/requests", func(c *fiber.Ctx) error {
		_, _ = io.Copy(io.Discard, c.Request().BodyStream())
		return c.SendStatus(200)
	})

	httpServer.Put("/stream", func(c *fiber.Ctx) error {
		_, _ = io.Copy(io.Discard, c.Request().BodyStream())
		return c.SendStatus(200)
	})

	// A failed bind has to end the process, otherwise the server keeps running
	// without a listener and looks healthy while refusing every connection.
	listenErr := make(chan error, 1)
	go func() {
		listenErr <- httpServer.Listen(bindAddress)
	}()

	go pollHostStats(ctx)

	for {
		select {
		case lerr := <-listenErr:
			if lerr != nil {
				return fmt.Errorf("unable to listen on %s: %w", bindAddress, lerr)
			}
			return nil
		case <-ctx.Done():
			return httpServer.Shutdown()
		case <-time.After(time.Second):
		}
	}
}

// dropCounters holds cumulative interface drop counters. Both directions are
// tracked: the transmit column is the one that matters for a saturating sender
// and used to be ignored entirely.
type dropCounters struct {
	rx uint64
	tx uint64
	ok bool
}

func (d dropCounters) total() uint64 { return d.rx + d.tx }

// hostStats is an immutable snapshot published as a whole, so readers never see
// a half-updated set of values and never race with the poller.
type hostStats struct {
	memUsedPercent int
	cpuUsedPercent int
	drops          dropCounters
}

var currentStats atomic.Pointer[hostStats]

// statsInterface is the interface whose drop counters we report, resolved once
// from the address this server serves on. Empty means "sum every non-loopback
// interface", which is the best available answer on a wildcard bind with no
// --real-ip.
var statsInterface string

func loadStats() hostStats {
	if s := currentStats.Load(); s != nil {
		return *s
	}
	// Nothing polled yet. Reporting a zeroed snapshot with an unknown drop
	// count is correct; dereferencing a nil pointer here used to panic the
	// sampling goroutine, which has no recover, and take the server with it.
	return hostStats{}
}

func collectHostStats() {
	s := hostStats{}

	if vm, err := mem.VirtualMemory(); err == nil && vm != nil {
		s.memUsedPercent = int(math.Round(vm.UsedPercent))
	} else if err != nil {
		shared.DEBUG("unable to read memory stats:", err)
	}

	// cpu.Percent blocks for the duration it is given, which is why this runs
	// on its own ticker rather than inline with anything that matters.
	if percent, err := cpu.Percent(time.Second, false); err == nil && len(percent) > 0 {
		s.cpuUsedPercent = int(math.Round(percent[0]))
	} else if err != nil {
		shared.DEBUG("unable to read cpu stats:", err)
	}

	s.drops = readDropCounters(statsInterface)
	currentStats.Store(&s)
}

func pollHostStats(ctx context.Context) {
	// The interval is enforced here rather than relying on cpu.Percent to block
	// for a second: gopsutil returns from it immediately if it cannot read the
	// CPU counters, which would turn this into a tight loop re-reading
	// /proc/meminfo and /proc/net/dev -- burning a core on the very host whose
	// throughput is being measured. In the normal case the blocking CPU sample
	// paces us and the ticker is already ready, so the period stays ~1s.
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	// The recover is per sample, so one panic costs a single reading rather
	// than freezing host stats for the life of the process.
	sample := func() {
		defer func() {
			if r := recover(); r != nil {
				log.Println(r, string(debug.Stack()))
			}
		}()
		collectHostStats()
	}

	sample()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		sample()
	}
}

// readDropCounters sums the receive and transmit drop columns from
// /proc/net/dev. When iface is set only that interface is counted; otherwise
// every non-loopback, non-virtual interface is. The previous implementation
// summed the receive column across every interface including lo, wg0 and
// tunnels, and reported it as an absolute since-boot figure.
func readDropCounters(iface string) (d dropCounters) {
	if runtime.GOOS != "linux" {
		return
	}
	data, err := os.ReadFile("/proc/net/dev")
	if err != nil {
		shared.DEBUG("unable to read /proc/net/dev:", err)
		return
	}

	for _, line := range strings.Split(string(data), "\n") {
		// The name is separated by a colon and can abut it when counters are
		// wide, so split on the colon rather than on whitespace.
		colon := strings.IndexByte(line, ':')
		if colon < 0 {
			continue
		}
		name := strings.TrimSpace(line[:colon])
		if name == "" || strings.Contains(name, "|") {
			continue
		}
		if iface != "" {
			if name != iface {
				continue
			}
		} else if name == "lo" {
			continue
		}

		// Receive: bytes packets errs drop fifo frame compressed multicast
		// Transmit: bytes packets errs drop fifo colls carrier compressed
		fields := strings.Fields(line[colon+1:])
		if len(fields) < 12 {
			continue
		}
		rx, errRX := strconv.ParseUint(fields[3], 10, 64)
		tx, errTX := strconv.ParseUint(fields[11], 10, 64)
		if errRX != nil || errTX != nil {
			continue
		}
		d.rx += rx
		d.tx += tx
		d.ok = true
	}
	return
}

// resolveStatsInterface maps the address this server serves on to an interface
// name, so drop counters describe the link actually carrying the test.
func resolveStatsInterface() string {
	candidate := realIP
	if candidate == "" {
		candidate = shared.HostOnly(bindAddress)
	}
	if candidate == "" {
		return ""
	}
	addr, err := netip.ParseAddr(shared.NormalizeHost(candidate))
	if err != nil || addr.IsUnspecified() {
		return ""
	}

	interfaces, err := net.Interfaces()
	if err != nil {
		return ""
	}
	for _, intf := range interfaces {
		addrs, err := intf.Addrs()
		if err != nil {
			continue
		}
		for _, a := range addrs {
			prefix, err := netip.ParsePrefix(a.String())
			if err != nil {
				continue
			}
			if prefix.Addr().Unmap() == addr.Unmap() {
				return intf.Name
			}
		}
	}
	return ""
}

func replyToPing(p *wsPeer) {
	msg := new(shared.WebsocketSignal)
	msg.SType = shared.Pong
	_ = p.writeJSON(msg)
}

func SendError(p *wsPeer, e error) error {
	if e == nil {
		return nil
	}
	msg := new(shared.WebsocketSignal)
	msg.SType = shared.Err
	msg.Error = e.Error()
	return p.writeJSON(msg)
}

// snapshotTests copies the test pointers under testLock. The slice is appended
// to by newTest, so ranging over it without the lock was a torn read.
func snapshotTests() []*test {
	testLock.Lock()
	defer testLock.Unlock()
	out := make([]*test, len(tests))
	copy(out, tests)
	return out
}

func stopAllTests(p *wsPeer, s shared.WebsocketSignal) {
	defer SendDone(p)
	for _, t := range snapshotTests() {
		if s.Config.TestID != "" && s.Config.TestID != t.ID {
			continue
		}
		shared.DEBUG("Stopping:", t.ID)
		t.cancel(errors.New("Client called StopAllTests"))
	}
}

func SendPing(p *wsPeer) error {
	msg := new(shared.WebsocketSignal)
	msg.SType = shared.Ping
	msg.Code = shared.OK
	return p.writeJSON(msg)
}

func SendDone(p *wsPeer) error {
	msg := new(shared.WebsocketSignal)
	msg.SType = shared.Done
	msg.Code = shared.OK
	return p.writeJSON(msg)
}

// isSelfHost reports whether host points back at this server. Addresses are
// compared as addresses and not as substrings, so --real-ip 10.0.0.1 no longer
// swallows the peer 10.0.0.10 and --real-ip fd00::1 no longer swallows
// fd00::10.
func isSelfHost(host string) bool {
	if realIP != "" && shared.SameHost(host, realIP) {
		return true
	}

	bindHost := shared.HostOnly(bindAddress)
	if bindHost == "" {
		return false
	}
	// A wildcard bind says nothing about our own identity, that is what
	// --real-ip is for.
	if addr, err := netip.ParseAddr(bindHost); err == nil && addr.IsUnspecified() {
		return false
	}
	return shared.SameHost(host, bindHost)
}

func newTest(c shared.Config) (t *test, err error) {
	// The ID becomes a filename component under --storage-path and it comes
	// from the client, so it is validated before it can touch the filesystem.
	if err = shared.ValidateTestID(c.TestID); err != nil {
		return nil, err
	}

	testLock.Lock()
	defer testLock.Unlock()

	for _, existing := range tests {
		if existing.ID == c.TestID && existing.live() {
			return nil, fmt.Errorf("A test with id (%s) is already running", c.TestID)
		}
	}

	t = new(test)
	t.errMap = make(map[string]struct{})
	t.cons = make(map[string]*wsPeer)
	t.Started = time.Now()
	t.Config = c
	t.DPS = make([]shared.DP, 0)
	t.ID = c.TestID
	t.ctx, t.cancel = context.WithCancelCause(context.Background())

	// Baseline the drop counters for this test. Prefer the poller's snapshot,
	// but read directly if it has not published yet, otherwise a test started
	// in the first second of the server's life reports "unknown" for its whole
	// run.
	t.dropped = loadStats().drops
	if !t.dropped.ok {
		t.dropped = readDropCounters(statsInterface)
	}

	if c.Save {
		if err = resetTestFiles(t); err != nil {
			return nil, err
		}
		// A test that cannot write its results should say so now rather than
		// run for the full duration and silently save nothing.
		if _, err = newTestFile(t); err != nil {
			return nil, err
		}
	}

	t.Readers = make([]*netPerfReader, 0)
	readersCreated := 0

	for i := range c.Hosts {

		if isSelfHost(c.Hosts[i]) {
			shared.DEBUG("Skipping self:", c.Hosts[i])
			continue
		}
		t.Readers = append(t.Readers,
			newPerformanceReaderForASingleHost(c, c.Hosts[i], c.Port),
		)
		readersCreated++

	}

	if readersCreated == 0 {
		return nil, errors.New("No performance readers were created, please revise your config")
	}

	tests = append(tests, t)
	return t, nil
}

type netPerfReader struct {
	// hasStats is atomic because it is set on every chunk written and read by
	// the sampling goroutine once a second.
	hasStats atomic.Bool
	m        sync.Mutex

	buf []byte

	addr   string
	url    string
	ip     string
	client *http.Client

	TXCount atomic.Uint64
	TX      atomic.Uint64

	concurrency chan int

	// Guarded by m.
	TTFBH int64
	TTFBL int64
	RMSH  int64
	RMSL  int64

	// Owned by the sampling goroutine.
	lastDataPointTime time.Time
}

type asyncReader struct {
	pr             *netPerfReader
	i              int64 // current reading index
	ttfbRegistered bool
	start          time.Time
	ctx            context.Context
	c              *shared.Config
}

// Read feeds the request body. It runs once per chunk on every in-flight
// request, so it takes the shared reader lock only on the first chunk, to
// record TTFB. ttfbRegistered is per-asyncReader state, so testing it needs no
// lock; hasStats is atomic. Previously every chunk of every request contended
// on one mutex per peer purely to re-set a bool that was already true.
func (a *asyncReader) Read(b []byte) (n int, err error) {
	if !a.ttfbRegistered {
		a.ttfbRegistered = true
		since := time.Since(a.start).Microseconds()
		a.pr.m.Lock()
		if since > a.pr.TTFBH {
			a.pr.TTFBH = since
		}
		if since < a.pr.TTFBL {
			a.pr.TTFBL = since
		}
		a.pr.m.Unlock()
	}

	// Must stay outside the branch above: a stream request issues one Read per
	// chunk for the whole test and never starts a second request, so gating
	// this on the first chunk would stop the reader reporting any data point
	// after its first interval.
	a.pr.hasStats.Store(true)

	if a.ctx.Err() != nil {
		return 0, io.EOF
	}

	if a.c.TestType == shared.StreamTest {
		n = copy(b, a.pr.buf)
		if n == 0 {
			// The buffer is released when a test finishes; returning 0, nil
			// forever would spin net/http.
			return 0, io.EOF
		}
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

func createAndRunTest(p *wsPeer, signal shared.WebsocketSignal) {
	defer SendDone(p)

	test, err := newTest(signal.Config)
	if err != nil {
		SendError(p, err)
		return
	}
	defer func() {
		shared.DEBUG("Test exiting:", test.ID)
	}()
	// Defers run last-registered-first, so this is cancel() then finish() then
	// the log line, and SendDone(p) last of all. Stopping the readers before
	// marking the test finished means it never keeps generating traffic that
	// nothing will sample, and finish() must not retire p -- SendDone still
	// has to go out over it.
	defer test.finish()
	defer test.cancel(errors.New("testing finished"))

	for i := range test.Readers {
		go startPerformanceReader(test, test.Readers[i])
	}

	test.attach(p)

	// A counted loop rather than a wall-clock comparison: the old form
	// re-checked elapsed time after a sleep that had already drifted by the
	// generate+send work, so a run emitted a duration-dependent, nondeterministic
	// number of samples.
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	for i := 0; i < test.Config.Duration; i++ {
		select {
		case <-test.ctx.Done():
			// Flush whatever the canceled interval accumulated instead of
			// dropping it.
			generateDataPoints(test)
			sendAndSaveData(test)
			return
		case <-ticker.C:
		}

		generateDataPoints(test)
		sendAndSaveData(test)
	}

	// The loop used to break before sampling, discarding the final interval's
	// bytes and any data points still queued.
	generateDataPoints(test)
	sendAndSaveData(test)
}

func listenToLiveTests(p *wsPeer, s shared.WebsocketSignal) {
	attached := 0
	for _, t := range snapshotTests() {
		if s.Config.TestID != "" && t.ID != s.Config.TestID {
			continue
		}
		if t.attach(p) {
			attached++
		}
	}
	if attached == 0 {
		SendError(p, fmt.Errorf("No live test matching id (%s) on this host", s.Config.TestID))
		SendDone(p)
	}
}

// sendAndSaveData drains the pending data points and errors under the lock,
// then persists and ships them with no lock held. Holding t.M across the disk
// write or the socket writes would both stall the sampling loop and deadlock
// against AddError.
func sendAndSaveData(t *test) {
	defer func() {
		if r := recover(); r != nil {
			log.Println(r, string(debug.Stack()))
		}
	}()

	t.M.Lock()
	dps := t.DPS
	t.DPS = make([]shared.DP, 0, len(dps))
	errs := t.errors
	t.errors = make([]shared.TError, 0)
	t.errMap = make(map[string]struct{})
	peers := make([]*wsPeer, 0, len(t.cons))
	for _, p := range t.cons {
		peers = append(peers, p)
	}
	t.M.Unlock()

	if len(dps) == 0 && len(errs) == 0 {
		return
	}

	if t.Config.Save {
		persist(t, dps, errs)
	}

	wss := &shared.WebsocketSignal{
		SType:     shared.Stats,
		DataPoint: &shared.DataReponseToClient{DPS: dps, Errors: errs},
	}

	var dead []string
	for _, p := range peers {
		if err := p.writeJSON(wss); err != nil {
			shared.DEBUG("Unable to send data point:", err)
			p.shutdown()
			dead = append(dead, p.pid)
		}
	}
	t.detach(dead)
}

// persist writes the batch to the test's data file. Errors here are logged
// rather than routed through AddError, so a failing disk cannot generate one
// error per interval forever.
func persist(t *test, dps []shared.DP, errs []shared.TError) {
	if t.DataFile == nil {
		if _, err := newTestFile(t); err != nil {
			shared.DEBUG("Unable to open a test file:", err)
			return
		}
	}

	w := bufio.NewWriter(t.DataFile)
	for i := range dps {
		if _, err := shared.WriteStructAndNewLine(w, shared.DataPoint, dps[i]); err != nil {
			shared.DEBUG("Unable to persist data point:", err)
			return
		}
	}
	for i := range errs {
		if _, err := shared.WriteStructAndNewLine(w, shared.ErrorPoint, errs[i]); err != nil {
			shared.DEBUG("Unable to persist error point:", err)
			return
		}
	}
	if err := w.Flush(); err != nil {
		shared.DEBUG("Unable to flush test file:", err)
	}
}

func generateDataPoints(t *test) {
	defer func() {
		if r := recover(); r != nil {
			log.Println(r, string(debug.Stack()))
		}
	}()

	stats := loadStats()

	local := realIP
	if local == "" {
		local = bindAddress
	}

	// Drops are reported as the delta since this test started. The counter
	// used to be an absolute since-boot total summed over every interface,
	// which made it a large constant that said nothing about the test.
	dropDelta := -1
	if stats.drops.ok && t.dropped.ok && stats.drops.total() >= t.dropped.total() {
		dropDelta = int(stats.drops.total() - t.dropped.total())
	}

	t.M.Lock()
	errCount := len(t.errors)
	t.M.Unlock()

	batch := make([]shared.DP, 0, len(t.Readers))
	now := time.Now()

	for _, r := range t.Readers {
		if r == nil || !r.hasStats.Load() {
			continue
		}

		// A rate is bytes divided by the measured window, so a very short
		// window yields a number orders of magnitude above anything real --
		// and that number then becomes TX(max). The final flush lands
		// microseconds after the last scheduled sample, which is exactly that
		// case. Skip the reader instead, leaving its counter and timestamp
		// alone so the bytes land in the next sample rather than being lost.
		elapsed := now.Sub(r.lastDataPointTime)
		if elapsed < minSampleWindow {
			continue
		}

		r.hasStats.Store(false)
		tx := r.TX.Swap(0)
		r.lastDataPointTime = now
		rate := uint64(float64(tx) / elapsed.Seconds())

		r.m.Lock()
		ttfbL, ttfbH, rmsL, rmsH := r.TTFBL, r.TTFBH, r.RMSL, r.RMSH
		r.TTFBH = 0
		r.TTFBL = math.MaxInt64
		r.RMSH = 0
		r.RMSL = math.MaxInt64
		r.m.Unlock()

		batch = append(batch, shared.DP{
			Type:              t.Config.TestType,
			TestID:            t.ID,
			Created:           now,
			Local:             local,
			Remote:            r.addr,
			TX:                rate,
			TXTotal:           tx,
			TXCount:           r.TXCount.Load(),
			TTFBL:             ttfbL,
			TTFBH:             ttfbH,
			RMSL:              rmsL,
			RMSH:              rmsH,
			ErrCount:          errCount,
			DroppedPackets:    dropDelta,
			MemoryUsedPercent: stats.memUsedPercent,
			CPUUsedPercent:    stats.cpuUsedPercent,
		})
	}

	if len(batch) == 0 {
		return
	}

	t.M.Lock()
	t.DPS = append(t.DPS, batch...)
	t.M.Unlock()
}

func newTransport(c *shared.Config) *http.Transport {
	return &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           newDialContext(10 * time.Second),
		MaxIdleConnsPerHost:   1024,
		WriteBufferSize:       c.BufferSize,
		ReadBufferSize:        c.BufferSize,
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

func newPerformanceReaderForASingleHost(c shared.Config, host string, port string) (r *netPerfReader) {
	r = new(netPerfReader)
	r.lastDataPointTime = time.Now()
	r.addr = net.JoinHostPort(host, port)
	r.url = shared.URLHostPort(host, port)
	r.ip = host
	r.buf = make([]byte, c.PayloadSize)
	r.TTFBL = math.MaxInt64
	r.RMSL = math.MaxInt64
	r.client = &http.Client{
		Transport: newTransport(&c),
	}
	r.concurrency = make(chan int, c.Concurrency)
	for i := 1; i <= c.Concurrency; i++ {
		r.concurrency <- i
	}
	return
}

func startPerformanceReader(t *test, r *netPerfReader) {
	defer func() {
		r := recover()
		if r != nil {
			log.Println(r, string(debug.Stack()))
		}
	}()
	for {
		var cid int
		select {
		case cid = <-r.concurrency:
			go sendRequestToHost(t, r, cid)
		case <-t.ctx.Done():
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
	method := http.MethodPut
	switch t.Config.TestType {
	case shared.StreamTest:
		route = "/stream"
		body = io.NopCloser(AR)
	case shared.RequestTest:
		route = "/requests"
		body = AR
	default:
		t.AddError(fmt.Errorf("Unknown test type: %d", t.Config.TestType), "unknown-signal")
	}

	req, err = http.NewRequestWithContext(
		t.ctx,
		method,
		proto+r.url+route,
		body,
	)
	if err != nil {
		t.AddError(err, "network-new-request")
		return
	}

	if t.Config.TestType == shared.StreamTest {
		req.ContentLength = -1
	}

	// Counted when the request is issued, not when it completes: a stream
	// request only ends when the test is canceled, so counting completions
	// would leave #TX permanently zero for every bandwidth run.
	r.TXCount.Add(1)

	sent := time.Now()
	resp, err = r.client.Do(req)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return
		}
		t.AddError(err, "network-error")
		return
	}

	// Drain and close on every path. The non-200 branch used to return without
	// closing, and net/http cannot reuse or release a connection whose body is
	// still open: each failed request permanently burned a connection, an fd
	// and its two net/http goroutines. A peer returning errors could leak
	// (hosts-1) * concurrency of them in a single test.
	defer func() {
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		t.AddError(
			fmt.Errorf("Status code was %d, expected 200 from host %s", resp.StatusCode, r.addr),
			"invalid-status-code-"+r.addr,
		)
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
	r.m.Unlock()
	r.hasStats.Store(true)
}

func listAllTests(p *wsPeer, s shared.WebsocketSignal) {
	defer SendDone(p)

	var err error
	s.TestList, err = listTestsFromDisk()
	if err != nil {
		SendError(p, err)
		return
	}

	s.Code = 200
	s.SType = shared.ListTests
	if err = p.writeJSON(s); err != nil {
		shared.DEBUG("Unable to send test list:", err)
	}
}

func getTestOnServer(p *wsPeer, s shared.WebsocketSignal) {
	defer SendDone(p)
	err := streamTestFilesToWebsocket(p, s.Config.TestID)
	if err != nil {
		SendError(p, err)
	}
}
