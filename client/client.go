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

package client

import (
	"bufio"
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net"
	"net/http"
	"net/url"
	"os"
	"reflect"
	"runtime/debug"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/fasthttp/websocket"
	"github.com/minio/hperf/shared"
)

var (
	responseDPS  = make([]shared.DP, 0)
	responseERR  = make([]shared.TError, 0)
	responseLock = sync.Mutex{}

	// liveAggregate is the incremental live summary. Guarded by responseLock.
	liveAggregate = newAggregate()

	// retainDPS controls whether every data point is kept. Latency runs need
	// them all for the percentile analysis; a bandwidth run only needs them
	// for --print-all, and at 256 hosts they arrive at 65k/s.
	retainDPS = true

	websockets     []*wsClient
	hostsDoingWork atomic.Int32

	// reconnectDeadline (UnixNano, 0 = unset) bounds the reconnect window. Once
	// the measurement window has passed there is nothing left to collect, so a
	// host that is still down should stop retrying rather than hold the command
	// open for its whole reconnect budget -- which cost ~17s of dead air after
	// the data was already complete.
	reconnectDeadline atomic.Int64
)

// connectTimeout bounds how long we wait for the initial connection to every
// host before proceeding with whichever ones answered.
const connectTimeout = 10 * time.Second

// maxReconnects bounds the reconnect loop. It used to respawn forever, which
// churned goroutines for the whole run against a host that was simply down.
const maxReconnects = 10

// noReattach means a reconnecting socket should not re-announce itself. Note
// shared.Err is 0, so the sentinel cannot be.
const noReattach = shared.SignalType(-1)

// defaultDialTimeout bounds the websocket handshake when the config does not
// say. Config.DialTimeout is hardcoded to zero, which websocket.Dialer reads as
// "no timeout": a reconnect to an address that black-holes packets -- a killed
// container, a host that fell off the network -- then hangs until the kernel
// gives up minutes later, holding the whole run open past its grace period. A
// host that refuses or resets fails fast either way; this is only about the
// silent case.
const defaultDialTimeout = 10 * time.Second

// handshakeTimeout is DialTimeout in seconds, floored so it is never unbounded.
func handshakeTimeout(c *shared.Config) time.Duration {
	if d := time.Second * c.DialTimeout; d > 0 {
		return d
	}
	return defaultDialTimeout
}

// wsClient tracks one host's connection. The reader goroutine reconnects and
// replaces the connection while the main goroutine is iterating hosts to send
// signals, so the shared fields are atomics rather than plain values.
type wsClient struct {
	ID   int
	Host string

	con      atomic.Pointer[websocket.Conn]
	counted  atomic.Bool
	excluded atomic.Bool

	// Owned by the reader goroutine chain, which is sequential per host: at
	// most one handleWSConnection invocation is live for a given socket.
	retries int
}

func (c *wsClient) Conn() *websocket.Conn { return c.con.Load() }

// withinReconnectWindow reports whether reconnecting could still yield data.
func withinReconnectWindow() bool {
	deadline := reconnectDeadline.Load()
	return deadline == 0 || time.Now().UnixNano() < deadline
}

// hold makes this host count towards hostsDoingWork, exactly once, and reports
// whether it was this call that did so.
func (c *wsClient) hold() bool {
	if c.counted.CompareAndSwap(false, true) {
		hostsDoingWork.Add(1)
		return true
	}
	return false
}

// release undoes hold, exactly once. Increment and decrement have to be
// symmetric: hostsDoingWork reaching zero is what tells keepAliveLoop every host
// has finished, so an unmatched decrement ends the run early. A host that never
// connected must therefore not decrement -- it never incremented.
func (c *wsClient) release() {
	if c.counted.CompareAndSwap(true, false) {
		hostsDoingWork.Add(-1)
	}
}

// reportReady announces this host's connect outcome to initializeClient.
//
// The send must not block. initializeClient drains this channel exactly
// len(hosts) times and then abandons it, while the reconnect path re-enters
// handleWSConnection with fresh locals -- so a blocking send would eventually
// fill the buffer and park the reader goroutine here, before its read loop,
// silently dropping that host from the results with nothing left to notice.
// Duplicate reports are harmless: initializeClient ignores a host that has
// already reported.
func (c *wsClient) reportReady(ready chan connectResult, err error) {
	select {
	case ready <- connectResult{id: c.ID, err: err}:
	default:
	}
}

// filterSelf removes every entry matching self, not just the first. A host
// listed twice used to leave one copy behind, so a server would test against
// itself through the local network stack.
func filterSelf(hosts []string, self string) []string {
	return slices.DeleteFunc(hosts, func(h string) bool {
		return shared.SameHost(h, self)
	})
}

// itterateWebsockets runs action for every host that is currently connected.
// The connection is loaded once and handed over, so it cannot be swapped out
// from under the action by a reconnect.
func itterateWebsockets(action func(c *wsClient, con *websocket.Conn)) {
	for i := range websockets {
		if websockets[i] == nil {
			continue
		}
		if con := websockets[i].Conn(); con != nil {
			action(websockets[i], con)
		}
	}
}

func (c *wsClient) NewSignal(signal shared.SignalType, conf shared.Config) *shared.WebsocketSignal {
	msg := new(shared.WebsocketSignal)
	msg.SType = signal
	msg.Config = conf
	return msg
}

var (
	testList = make(map[string]shared.TestInfo)
	testLock = sync.Mutex{}
)

type connectResult struct {
	id  int
	err error
}

// initializeClient dials every host and returns the ones that answered. A
// single unreachable host used to abort the whole run; now the run proceeds
// with the reachable subset and says loudly which hosts it dropped, because a
// silently smaller mesh is exactly the failure this tool exists to detect.
// reattach is the signal a reconnecting socket re-sends so the server hands it
// back the test already in flight; pass noReattach for commands where that makes
// no sense.
func initializeClient(ctx context.Context, c *shared.Config, reattach shared.SignalType) (reachable []string, err error) {
	websockets = make([]*wsClient, len(c.Hosts))
	for i := range c.Hosts {
		websockets[i] = &wsClient{ID: i, Host: c.Hosts[i]}
	}
	hostsDoingWork.Store(0)

	// A duration of zero means the caller is not running a bounded test, so
	// leave the reconnect budget alone.
	if c.Duration > 0 {
		reconnectDeadline.Store(time.Now().Add(time.Duration(c.Duration) * time.Second).UnixNano())
	} else {
		reconnectDeadline.Store(0)
	}

	responseLock.Lock()
	liveAggregate = newAggregate()
	responseLock.Unlock()

	// Reports are best-effort sends into a buffer nobody drains once this
	// function returns, so the reconnect path can never block on it.
	ready := make(chan connectResult, len(c.Hosts))
	for i := range websockets {
		go handleWSConnection(ctx, c, websockets[i], ready, reattach)
	}

	timeout := time.NewTimer(connectTimeout)
	defer timeout.Stop()

	reported := make([]bool, len(c.Hosts))
	remaining := len(c.Hosts)

waiting:
	for remaining > 0 {
		select {
		case r := <-ready:
			if reported[r.id] {
				continue
			}
			reported[r.id] = true
			remaining--
		case <-ctx.Done():
			return nil, errors.New("Context canceled")
		case <-timeout.C:
			break waiting
		}
	}

	excluded := make([]string, 0)
	reachable = make([]string, 0, len(c.Hosts))
	for i := range websockets {
		if websockets[i].Conn() != nil {
			reachable = append(reachable, websockets[i].Host)
			continue
		}
		// Stop the reconnect chain for a host that is not part of the mesh.
		websockets[i].excluded.Store(true)
		excluded = append(excluded, websockets[i].Host)
	}

	if len(reachable) == 0 {
		return nil, fmt.Errorf("Unable to connect to any of the %d configured hosts", len(c.Hosts))
	}
	if len(excluded) > 0 {
		PrintErrorString(fmt.Sprintf(
			"WARNING: %d of %d hosts did not answer and were excluded from the test: %s",
			len(excluded), len(c.Hosts), strings.Join(excluded, ", "),
		))
	}
	return reachable, nil
}

func handleWSConnection(ctx context.Context, c *shared.Config, socket *wsClient, ready chan connectResult, reattach shared.SignalType) {
	var err error
	host := socket.Host

	defer func() {
		if r := recover(); r != nil {
			fmt.Println(r, string(debug.Stack()))
		}
		socket.reportReady(ready, err)

		if ctx.Err() != nil {
			socket.release()
			return
		}
		// Only retry a host that was part of the mesh, only a bounded number of
		// times, and only while the measurement is still running. The retry
		// keeps the host counted: releasing here and re-holding on reconnect
		// would let the count dip to zero and end the whole run.
		if c.RestartOnError && err != nil && !socket.excluded.Load() &&
			socket.retries < maxReconnects && withinReconnectWindow() {
			socket.retries++
			time.Sleep(500 * time.Millisecond)
			go handleWSConnection(ctx, c, socket, ready, reattach)
			return
		}
		socket.release()
	}()

	dialer := websocket.Dialer{
		Proxy:            http.ProxyFromEnvironment,
		HandshakeTimeout: handshakeTimeout(c),
		// These are per-connection buffers. 1 MB each cost ~2 MB per host on
		// the client for no benefit: the signals are small and the data-point
		// batches are tens of kilobytes.
		ReadBufferSize:  64 * 1024,
		WriteBufferSize: 64 * 1024,
	}

	shared.DEBUG(WarningStyle.Render("Connecting to ", net.JoinHostPort(host, c.Port)))

	scheme := "wss"
	if c.Insecure {
		scheme = "ws"
	}
	connectString := scheme + "://" + shared.URLHostPort(host, c.Port) + "/ws/" + url.PathEscape(host)

	con, resp, dialErr := dialer.DialContext(
		ctx,
		connectString,
		nil)
	// A failed handshake still returns a response whose body has to be closed,
	// otherwise every reconnect against a host that answers but will not
	// upgrade leaks a connection.
	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}
	if dialErr != nil {
		err = fmt.Errorf("%s: %w", host, dialErr)
		PrintError(err)
		return
	}
	socket.con.Store(con)
	defer func() {
		socket.con.CompareAndSwap(con, nil)
		_ = con.Close()
	}()

	msg := new(shared.WebsocketSignal)
	err = con.ReadJSON(&msg)
	if err != nil {
		err = fmt.Errorf("Unable to read message from server on first connect: %w", err)
		PrintError(err)
		return
	}
	if msg.Code != shared.OK {
		err = fmt.Errorf("Received %d from server on connect", msg.Code)
		PrintError(err)
		return
	}
	shared.DEBUG(SuccessStyle.Render("Connected to ", net.JoinHostPort(host, c.Port)))

	// Count the host only once it is actually up, so the counter is only ever
	// decremented by a host that contributed to it.
	socket.hold()
	socket.reportReady(ready, nil)

	// A reconnected socket is unknown to the test already running on the
	// server, so it would receive neither data points nor -- the part that
	// matters -- a Done. The command would then wait out its entire grace
	// period and report failure on an otherwise complete run. Re-announce it:
	// the server either attaches it to the live test, or answers Done because
	// no matching test exists, which ends this reader cleanly.
	if socket.retries > 0 && reattach != noReattach {
		if werr := con.WriteJSON(socket.NewSignal(reattach, *c)); werr != nil {
			err = fmt.Errorf("%s: unable to re-attach after reconnect: %w", host, werr)
			PrintError(err)
			return
		}
		shared.DEBUG(WarningStyle.Render("Re-attached to ", host, " after reconnect"))
	}

	for {
		signal := new(shared.WebsocketSignal)
		err = con.ReadJSON(&signal)
		if err != nil {
			PrintError(err)
			return
		}
		if shared.DebugEnabled {
			fmt.Printf("WebsocketSignal: %+v\n", signal)
		}
		// Handled inline rather than in a goroutine per message: spawning gave
		// no ordering guarantee, piled up goroutines contending on
		// responseLock, and at 256 hosts meant tens of thousands of goroutines
		// a second. The read loop is per-host already.
		switch signal.SType {
		case shared.Stats:
			// Live tests print an aggregate table of their own, attached
			// clients print the incoming data points as they arrive.
			if c.PrintLive {
				printAndCollectDataPoints(host, signal.DataPoint, c)
			} else {
				collectDataPointv2(host, signal.DataPoint)
			}
		case shared.ListTests:
			parseTestList(signal.TestList)
		case shared.GetTest:
			receiveJSONDataPoint(signal.Data, c)
		case shared.Err:
			PrintErrorString(signal.Error)
		case shared.Done:
			shared.DEBUG(SuccessStyle.Render("Host Finished: ", host))
			return
		}
	}
}

func PrintTError(err shared.TError) {
	fmt.Println(ErrorStyle.Render(err.Created.Format(time.RFC3339), " - ", err.Error))
}

func PrintErrorString(err string) {
	fmt.Println(ErrorStyle.Render(err))
}

func PrintError(err error) {
	if err == nil {
		return
	}
	fmt.Println(ErrorStyle.Render("ERROR: ", err.Error()))
}

func receiveJSONDataPoint(data []byte, _ *shared.Config) {
	responseLock.Lock()
	defer responseLock.Unlock()

	if bytes.HasPrefix(data, shared.ErrorPoint.String()) {
		dp := new(shared.TError)
		err := json.Unmarshal(data[1:], &dp)
		if err != nil {
			PrintError(err)
			return
		}
		responseERR = append(responseERR, *dp)
	} else if bytes.HasPrefix(data, shared.DataPoint.String()) {
		dp := new(shared.DP)
		err := json.Unmarshal(data[1:], &dp)
		if err != nil {
			PrintError(err)
			return
		}
		responseDPS = append(responseDPS, *dp)
	} else {
		PrintError(fmt.Errorf("Uknown data point: %s", data))
	}
}

func keepAliveLoop(ctx context.Context, c *shared.Config, tickerfunc func() (shouldExit bool)) error {
	start := time.Now()

	// The normal exit is every host reporting Done, which drives
	// hostsDoingWork to zero. This is only a backstop, and it has to outlast
	// the servers: each runs Duration sampling intervals plus a final flush,
	// and shipping stats to attached clients adds to every interval. The grace
	// period was a flat 20 seconds, which a long run could exceed -- getting
	// cut off and silently reported as finished. Scaling it fixes that without
	// making short runs wait longer than they used to when a host dies.
	grace := time.Duration(max(20, c.Duration/2)) * time.Second
	limit := time.Duration(c.Duration)*time.Second + grace

	for ctx.Err() == nil {
		time.Sleep(1 * time.Second)
		if time.Since(start) > limit {
			return fmt.Errorf(
				"Hosts did not finish within %s of the configured %ds duration",
				grace, c.Duration,
			)
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		if tickerfunc != nil && tickerfunc() {
			break
		}

		if hostsDoingWork.Load() <= 0 {
			return ctx.Err()
		}

	}
	return ctx.Err()
}

func Listen(ctx context.Context, c shared.Config) (err error) {
	cancelContext, cancel := context.WithCancel(ctx)
	defer cancel()
	c.PrintLive = true
	_, err = initializeClient(cancelContext, &c, shared.ListenTest)
	if err != nil {
		return
	}

	itterateWebsockets(func(ws *wsClient, con *websocket.Conn) {
		err = con.WriteJSON(ws.NewSignal(shared.ListenTest, c))
		if err != nil {
			return
		}
	})

	return keepAliveLoop(ctx, &c, nil)
}

func Stop(ctx context.Context, c shared.Config) (err error) {
	cancelContext, cancel := context.WithCancel(ctx)
	defer cancel()
	_, err = initializeClient(cancelContext, &c, noReattach)
	if err != nil {
		return
	}

	itterateWebsockets(func(ws *wsClient, con *websocket.Conn) {
		err = con.WriteJSON(ws.NewSignal(shared.StopAllTests, c))
		if err != nil {
			return
		}
	})

	return keepAliveLoop(ctx, &c, nil)
}

func RunTest(ctx context.Context, c shared.Config) (err error) {
	cancelContext, cancel := context.WithCancel(ctx)
	defer cancel()

	// A bandwidth run only needs every data point for --print-all; a latency
	// run always needs them for the percentile analysis. At 256 hosts they
	// arrive at 65k/s, so keeping them when nothing will read them is a
	// gigabyte of garbage for nothing.
	retainDPS = c.PrintAll || c.PrintStats || c.TestType == shared.RequestTest

	reachable, err := initializeClient(cancelContext, &c, shared.ListenTest)
	if err != nil {
		return
	}

	// Only reachable hosts go into the mesh. Handing servers a peer that is
	// known to be down just makes every one of them spend the test erroring
	// against it.
	ogh := slices.Clone(c.Hosts)
	itterateWebsockets(func(ws *wsClient, con *websocket.Conn) {
		c.Hosts = filterSelf(slices.Clone(reachable), ws.Host)
		if werr := con.WriteJSON(ws.NewSignal(shared.RunTest, c)); werr != nil {
			PrintError(fmt.Errorf("%s: unable to start test: %w", ws.Host, werr))
		}
	})
	c.Hosts = ogh

	printCount := 0
	errorsPrinted := 0
	renderedSamples := uint64(0)

	printOnTick := func() bool {
		responseLock.Lock()
		to := liveAggregate.output(c.Micro)
		tt := liveAggregate.testType
		newErrors := make([]shared.TError, 0)
		if len(responseERR) > errorsPrinted {
			newErrors = append(newErrors, responseERR[errorsPrinted:]...)
			errorsPrinted = len(responseERR)
		}
		responseLock.Unlock()

		// Nothing new to say. This also makes the final render after the loop
		// a no-op when the last tick already covered everything, rather than
		// repeating an identical row.
		if to.Samples == 0 || to.Samples == renderedSamples {
			return false
		}
		renderedSamples = to.Samples
		printCount++

		// Only errors not already shown. The old loop reprinted every error
		// received so far on every tick.
		for i := range newErrors {
			PrintErrorString(newErrors[i].Error)
		}

		if printCount%10 == 1 {
			printRealTimeHeaders(tt)
		}
		printRealTimeRow(BaseStyle, to, tt)

		return false
	}

	err = keepAliveLoop(ctx, &c, printOnTick)

	// One last render. Servers flush their final interval when the run ends,
	// which lands after the loop's last tick, so without this the live
	// TX(total) stops short of what was actually saved.
	printOnTick()

	responseLock.Lock()
	hosts := liveAggregate.hosts()
	fleet := liveAggregate.fleetAverage()
	samples := liveAggregate.samples
	responseLock.Unlock()
	printHostAverages(hosts, fleet)

	// A test that produced nothing has to fail the command. Every server
	// rejecting the config -- an invalid --id, for instance -- used to leave
	// the client printing "Testing finished" and exiting successfully.
	if err == nil && samples == 0 {
		return errors.New("No data points were received from any host, the test did not run")
	}
	return err
}

func ListTests(ctx context.Context, c shared.Config) (err error) {
	cancelContext, cancel := context.WithCancel(ctx)
	defer cancel()
	_, err = initializeClient(cancelContext, &c, noReattach)
	if err != nil {
		return
	}

	itterateWebsockets(func(ws *wsClient, con *websocket.Conn) {
		err = con.WriteJSON(ws.NewSignal(shared.ListTests, c))
		if err != nil {
			return
		}
	})

	err = keepAliveLoop(ctx, &c, nil)
	if err != nil {
		return
	}

	printHeader(ListHeaders)
	tableStyle := lipgloss.NewStyle()

	keys := []string{}
	for id := range testList {
		keys = append(keys, id)
	}

	slices.SortFunc(keys, func(a string, b string) int {
		if testList[a].Time.Before(testList[b].Time) {
			return 1
		} else {
			return -1
		}
	})

	for i := range keys {
		PrintColumns(
			tableStyle,
			column{strconv.Itoa(i), colWidth(IntNumber)},
			column{keys[i], colWidth(ID)},
			column{testList[keys[i]].Time.Format("02/01/2006 3:04 PM"), colWidth(ID)},
		)
	}

	return err
}

func DeleteTests(ctx context.Context, c shared.Config) (err error) {
	cancelContext, cancel := context.WithCancel(ctx)
	defer cancel()
	_, err = initializeClient(cancelContext, &c, noReattach)
	if err != nil {
		return
	}

	itterateWebsockets(func(ws *wsClient, con *websocket.Conn) {
		err = con.WriteJSON(ws.NewSignal(shared.DeleteTests, c))
		if err != nil {
			return
		}
	})

	return keepAliveLoop(ctx, &c, nil)
}

func parseTestList(list []shared.TestInfo) {
	testLock.Lock()
	defer testLock.Unlock()

	for i := range list {
		_, ok := testList[list[i].ID]
		if !ok {
			testList[list[i].ID] = list[i]
		}
	}
}

func DownloadTest(ctx context.Context, c shared.Config) (err error) {
	cancelContext, cancel := context.WithCancel(ctx)
	defer cancel()
	_, err = initializeClient(cancelContext, &c, noReattach)
	if err != nil {
		return
	}

	itterateWebsockets(func(ws *wsClient, con *websocket.Conn) {
		err = con.WriteJSON(ws.NewSignal(shared.GetTest, c))
		if err != nil {
			fmt.Println(err)
			return
		}
	})

	_ = keepAliveLoop(ctx, &c, nil)

	// Snapshot under the lock: a straggling reader goroutine can still be
	// appending, and these used to be read with no lock at all.
	responseLock.Lock()
	dps := slices.Clone(responseDPS)
	errs := slices.Clone(responseERR)
	responseLock.Unlock()

	// Refuse to present an empty file as a successful download. Every host
	// rejecting the id -- or simply not having the test -- used to create or
	// truncate the target, write nothing, and exit 0.
	if len(dps) == 0 && len(errs) == 0 {
		return fmt.Errorf("No records for test (%s) were returned by any host", c.TestID)
	}

	// Compare returns 0 for equal timestamps. Returning 1 broke
	// slices.SortFunc's ordering contract, which left ties in an undefined
	// order and made two downloads of the same test produce different files.
	slices.SortFunc(errs, func(a shared.TError, b shared.TError) int {
		return a.Created.Compare(b.Created)
	})
	slices.SortFunc(dps, func(a shared.DP, b shared.DP) int {
		return a.Created.Compare(b.Created)
	})

	f, err := os.Create(c.File)
	if err != nil {
		return err
	}
	// Close is reported rather than discarded: this function's whole purpose is
	// to leave a correct file on disk, and a deferred write can fail at Close
	// even after Flush succeeded.
	defer func() {
		if cerr := f.Close(); cerr != nil && err == nil {
			err = cerr
		}
	}()

	w := bufio.NewWriter(f)
	for i := range dps {
		if _, err = shared.WriteStructAndNewLine(w, shared.DataPoint, dps[i]); err != nil {
			return err
		}
	}
	for i := range errs {
		if _, err = shared.WriteStructAndNewLine(w, shared.ErrorPoint, errs[i]); err != nil {
			return err
		}
	}

	err = w.Flush()
	return err
}

// snapshotResponses copies the collected data under the lock. Reading these
// globals directly raced with the reader goroutines still appending to them.
func snapshotResponses() (dps []shared.DP, errs []shared.TError) {
	responseLock.Lock()
	defer responseLock.Unlock()
	return slices.Clone(responseDPS), slices.Clone(responseERR)
}

func AnalyzeBandwidthTest(ctx context.Context, c shared.Config) (err error) {
	_, cancel := context.WithCancel(ctx)
	defer cancel()

	dps, errs := snapshotResponses()

	if c.PrintAll {
		shared.INFO(" Printing all data points ..")
		fmt.Println("")

		printSliceOfDataPoints(dps, c)

		if len(errs) > 0 {
			fmt.Println(" ____ ERRORS ____")
		}
		for i := range errs {
			PrintTError(errs[i])
		}
		if len(errs) > 0 {
			fmt.Println("")
		}
	}

	// Retention is off for a plain bandwidth run, so an empty slice does not
	// mean an empty run -- ask the aggregate, which counts every data point
	// regardless of whether it was kept.
	responseLock.Lock()
	samples := liveAggregate.samples
	responseLock.Unlock()
	if samples == 0 {
		fmt.Println("No datapoints found")
	}

	return nil
}

func AnalyzeLatencyTest(ctx context.Context, c shared.Config) (err error) {
	_, cancel := context.WithCancel(ctx)
	defer cancel()

	dps, errs := snapshotResponses()

	if c.PrintAll {
		shared.INFO(" Printing all data points ..")

		printSliceOfDataPoints(dps, c)

		if len(errs) > 0 {
			fmt.Println(" ____ ERRORS ____")
		}
		for i := range errs {
			PrintTError(errs[i])
		}
		if len(errs) > 0 {
			fmt.Println("")
		}
	}
	if len(dps) == 0 {
		fmt.Println("No datapoints found")
		return
	}

	shared.INFO(" Analyzing data ..")
	fmt.Println("")
	analyzeLatencyTest(dps, c)

	return nil
}

// parseTestFile reads a saved or downloaded test file. Each line is one record:
// a prefix byte identifying the type, then the JSON.
//
// Both branches test the prefix on b and unmarshal b[1:]. The error branch used
// to test the prefix on b[1:] -- which is always '{', so it never matched -- and
// then unmarshal b including the prefix byte, which could not have parsed
// either. Every error point in a file was silently discarded, so `analyze
// --print-errors` reported none on a file full of them.
func parseTestFile(path string) (dps []shared.DP, errs []shared.TError, err error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}
	defer f.Close()

	dps = make([]shared.DP, 0)
	errs = make([]shared.TError, 0)

	s := bufio.NewScanner(f)
	for s.Scan() {
		b := s.Bytes()
		if len(b) < 2 {
			continue
		}
		switch {
		case bytes.HasPrefix(b, shared.ErrorPoint.String()):
			dperr := new(shared.TError)
			if err := json.Unmarshal(b[1:], dperr); err != nil {
				return nil, nil, err
			}
			errs = append(errs, *dperr)
		case bytes.HasPrefix(b, shared.DataPoint.String()):
			dp := new(shared.DP)
			if err := json.Unmarshal(b[1:], dp); err != nil {
				return nil, nil, err
			}
			dps = append(dps, *dp)
		default:
			shared.DEBUG(ErrorStyle.Render("Unknown data point encountered: ", string(b)))
		}
	}
	return dps, errs, s.Err()
}

func AnalyzeTest(ctx context.Context, c shared.Config) (err error) {
	_, cancel := context.WithCancel(ctx)
	defer cancel()

	dps, errors, err := parseTestFile(c.File)
	if err != nil {
		return err
	}

	if c.HostFilter != "" {
		dps = shared.HostFilter(c.HostFilter, dps)
	}

	if c.PrintStats {
		printSliceOfDataPoints(dps, c)
	}

	if c.PrintErrors {
		if len(errors) > 0 {
			fmt.Println(" ____ ERRORS ____")
		}
		for i := range errors {
			PrintTError(errors[i])
		}
		if len(errors) > 0 {
			fmt.Println("")
		}
	}

	if len(dps) == 0 {
		fmt.Println("No datapoints found")
		return
	}

	switch dps[0].Type {
	case shared.RequestTest:
		analyzeLatencyTest(dps, c)
	case shared.StreamTest:
		fmt.Println("")
		fmt.Println("Detailed analysis for bandwidth testing is in development")
	}

	return nil
}

func analyzeLatencyTest(dps []shared.DP, c shared.Config) {
	shared.SortDataPoints(dps, c)

	dps10 := math.Ceil((float64(len(dps)) / 100) * 10)
	dps50 := math.Floor((float64(len(dps)) / 100) * 50)
	dps90 := math.Floor((float64(len(dps)) / 100) * 90)
	dps99 := math.Floor((float64(len(dps)) / 100) * 99)

	// Only the P99 slice is rendered. The P10/P50/P90 slices were built and
	// then never read -- three near-full copies of the data set per analysis.
	dps99s := make([]shared.DP, 0)

	// count, sum, low, avg, high
	dps10stats := []int64{0, 0, math.MaxInt64, 0, 0}
	dps50stats := []int64{0, 0, math.MaxInt64, 0, 0}
	dps90stats := []int64{0, 0, math.MaxInt64, 0, 0}
	dps99stats := []int64{0, 0, math.MaxInt64, 0, 0}

	for i := range dps {
		if i >= int(dps10) {
			shared.UpdatePSStats(dps10stats, dps[i], c)
		}
		if i >= int(dps50) {
			shared.UpdatePSStats(dps50stats, dps[i], c)
		}
		if i >= int(dps90) {
			shared.UpdatePSStats(dps90stats, dps[i], c)
		}
		if i >= int(dps99) {
			dps99s = append(dps99s, dps[i])
			shared.UpdatePSStats(dps99stats, dps[i], c)
		}
	}

	fmt.Println("")
	fmt.Println(" _____ P99 data points _____ ")
	fmt.Println("")
	printSliceOfDataPoints(dps99s, c)

	fmt.Println("")
	if c.Sort == "" {
		fmt.Println(" Sorting:", shared.SortDefault)
	} else {
		fmt.Println(" Sorting:", c.Sort)
	}
	if c.Micro {
		fmt.Println(" Time: Microseconds")
	} else {
		fmt.Println(" Time: Milliseconds")
	}
	fmt.Println("")
	PrintPercentiles(SuccessStyle, "P10", dps10stats, c)
	PrintPercentiles(WarningStyle, "P50", dps50stats, c)
	PrintPercentiles(ErrorStyle, "P90", dps90stats, c)
	PrintPercentiles(ErrorStyle, "P99", dps99stats, c)
}

func MakeCSV(ctx context.Context, c shared.Config) (err error) {
	byteValue, err := os.ReadFile(c.File)
	if err != nil {
		return err
	}

	file, err := os.Create(c.File + ".csv")
	if err != nil {
		return err
	}
	defer file.Close()

	fb := bytes.NewBuffer(byteValue)
	scanner := bufio.NewScanner(fb)

	writer := csv.NewWriter(file)
	defer writer.Flush()
	if err := writer.Write(getStructFields(new(shared.DP))); err != nil {
		return err
	}

	for scanner.Scan() {
		b := scanner.Bytes()
		if bytes.HasPrefix(b, shared.DataPoint.String()) {
			dp := new(shared.DP)
			err = json.Unmarshal(b[1:], dp)
			if err != nil {
				return err
			}

			if err := writer.Write(dpToSlice(dp)); err != nil {
				return err
			}
		}
	}

	return nil
}

// Function to get field names of the struct
func getStructFields(s any) []string {
	t := reflect.TypeOf(s).Elem()
	fields := make([]string, t.NumField())
	for i := 0; i < t.NumField(); i++ {
		fields[i] = t.Field(i).Tag.Get("json")
		if fields[i] == "" {
			fields[i] = t.Field(i).Name
		}
	}
	return fields
}

func dpToSlice(dp *shared.DP) (data []string) {
	v := reflect.ValueOf(dp).Elem()
	data = make([]string, v.NumField())
	for i := 0; i < v.NumField(); i++ {
		data[i] = fmt.Sprintf("%v", v.Field(i).Interface())
	}
	return
}

func transformDataPointsToMilliseconds(dps []shared.DP) (clone []shared.DP) {
	clone = make([]shared.DP, len(dps))
	copy(clone, dps)
	for i := range clone {
		clone[i].TTFBH = clone[i].TTFBH / 1000
		clone[i].TTFBL = clone[i].TTFBL / 1000
		clone[i].RMSH = clone[i].RMSH / 1000
		clone[i].RMSL = clone[i].RMSL / 1000
	}
	return
}

func printSliceOfDataPoints(dps []shared.DP, c shared.Config) {
	var data []shared.DP
	if !c.Micro {
		data = transformDataPointsToMilliseconds(dps)
	} else {
		data = dps
	}

	growHostColumns(data)

	for i := range data {
		if i%20 == 0 {
			printDataPointHeaders(data[0].Type)
		}
		dp := data[i]
		printTableRow(BaseStyle, &dp, dp.Type)
	}
}
