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
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/minio/hperf/shared"
)

func streamDP(host string, tx uint64) shared.DP {
	return shared.DP{
		Type: shared.StreamTest, Local: host, Remote: "10.0.0.9:9010",
		TX: tx, TXTotal: tx, TXCount: 1,
		RMSL: math.MaxInt64, RMSH: 0,
		TTFBL: math.MaxInt64, TTFBH: 0,
		DroppedPackets: -1,
	}
}

// TestAggregateMinAvgMax is the property that makes the three throughput
// columns readable together: they summarize one population, so min <= avg <= max
// always holds and avg is the true mean.
func TestAggregateMinAvgMax(t *testing.T) {
	a := newAggregate()
	rates := []uint64{100, 200, 300, 400, 500}
	var sum uint64
	for i, r := range rates {
		a.add(fmt.Sprintf("10.0.0.%d", i+1), streamDP(fmt.Sprintf("10.0.0.%d", i+1), r))
		sum += r
	}

	to := a.output(true)
	if to.TXL != 100 {
		t.Errorf("TXL = %d, want 100", to.TXL)
	}
	if to.TXH != 500 {
		t.Errorf("TXH = %d, want 500", to.TXH)
	}
	if want := sum / uint64(len(rates)); to.TXA != want {
		t.Errorf("TXA = %d, want %d", to.TXA, want)
	}
	if to.TXL > to.TXA || to.TXA > to.TXH {
		t.Errorf("min <= avg <= max violated: %d %d %d", to.TXL, to.TXA, to.TXH)
	}
	if to.Samples != uint64(len(rates)) {
		t.Errorf("Samples = %d, want %d", to.Samples, len(rates))
	}
	if to.TXT != sum {
		t.Errorf("TXT = %d, want %d", to.TXT, sum)
	}
}

// TestAggregateIgnoresLatencySentinels covers the sentinel leak: servers seed
// the low watermarks to MaxInt64 and a stream test never completes a request, so
// its RMS fields stay sentinel for the whole run. Rendering that as a latency
// would print a nonsense number.
func TestAggregateIgnoresLatencySentinels(t *testing.T) {
	a := newAggregate()
	for i := 0; i < 5; i++ {
		a.add("10.0.0.1", streamDP("10.0.0.1", 1000))
	}

	to := a.output(true)
	if to.RMSL != 0 {
		t.Errorf("RMSL = %d, want 0 when nothing was measured", to.RMSL)
	}
	if to.TTFBL != 0 {
		t.Errorf("TTFBL = %d, want 0 when nothing was measured", to.TTFBL)
	}

	// A real measurement must still come through.
	dp := streamDP("10.0.0.1", 1000)
	dp.RMSL, dp.RMSH = 1500, 9000
	dp.TTFBL, dp.TTFBH = 300, 800
	a.add("10.0.0.1", dp)

	to = a.output(true)
	if to.RMSL != 1500 || to.RMSH != 9000 {
		t.Errorf("RMS = %d/%d, want 1500/9000", to.RMSL, to.RMSH)
	}
	if to.TTFBL != 300 || to.TTFBH != 800 {
		t.Errorf("TTFB = %d/%d, want 300/800", to.TTFBL, to.TTFBH)
	}
}

// TestAggregateEmptyIsSafe makes sure a tick before any data point renders zeros
// rather than MaxUint64 sentinels.
func TestAggregateEmptyIsSafe(t *testing.T) {
	to := newAggregate().output(false)
	if to.Samples != 0 || to.TXA != 0 || to.TXL != 0 || to.TXH != 0 {
		t.Errorf("empty aggregate rendered %+v", to)
	}
}

// TestAggregateMillisecondConversion pins the unit handling that the live row
// depends on.
func TestAggregateMillisecondConversion(t *testing.T) {
	a := newAggregate()
	dp := streamDP("10.0.0.1", 10)
	dp.RMSL, dp.RMSH = 2000, 5000
	dp.TTFBL, dp.TTFBH = 1000, 3000
	a.add("10.0.0.1", dp)

	if to := a.output(true); to.RMSL != 2000 || to.RMSH != 5000 {
		t.Errorf("micro: RMS = %d/%d, want 2000/5000", to.RMSL, to.RMSH)
	}
	if to := a.output(false); to.RMSL != 2 || to.RMSH != 5 {
		t.Errorf("milli: RMS = %d/%d, want 2/5", to.RMSL, to.RMSH)
	}
}

// TestAggregatePerHost covers the per-host breakdown, including that a host is
// identified by the address the client dialed. DP.Local is whatever the server
// believes, which is the same wildcard string on every node without --real-ip.
func TestAggregatePerHost(t *testing.T) {
	a := newAggregate()
	// Both servers report the same useless Local value.
	for _, rate := range []uint64{1000, 2000, 3000} {
		dp := streamDP("0.0.0.0:9010", rate)
		a.add("10.0.0.1", dp)
	}
	for _, rate := range []uint64{100, 200} {
		dp := streamDP("0.0.0.0:9010", rate)
		a.add("10.0.0.2", dp)
	}

	hosts := a.hosts()
	if len(hosts) != 2 {
		t.Fatalf("got %d hosts, want 2 -- dialed address is not being used as identity", len(hosts))
	}
	// Slowest first.
	if hosts[0].Host != "10.0.0.2" {
		t.Errorf("hosts[0] = %s, want the slower 10.0.0.2", hosts[0].Host)
	}
	if got, want := hosts[0].Avg(), uint64(150); got != want {
		t.Errorf("10.0.0.2 avg = %d, want %d", got, want)
	}
	if got, want := hosts[1].Avg(), uint64(2000); got != want {
		t.Errorf("10.0.0.1 avg = %d, want %d", got, want)
	}
	if hosts[0].TXMin != 100 || hosts[0].TXMax != 200 {
		t.Errorf("10.0.0.2 min/max = %d/%d, want 100/200", hosts[0].TXMin, hosts[0].TXMax)
	}
}

// TestAggregateDroppedIsUnknownUntilReported keeps -1 ("no usable counter")
// distinct from 0 ("no drops").
func TestAggregateDroppedIsUnknownUntilReported(t *testing.T) {
	a := newAggregate()
	a.add("h", streamDP("h", 10))
	if to := a.output(true); to.DP != -1 {
		t.Errorf("DP = %d, want -1 when no host reported a counter", to.DP)
	}

	dp := streamDP("h", 10)
	dp.DroppedPackets = 7
	a.add("h", dp)
	if to := a.output(true); to.DP != 7 {
		t.Errorf("DP = %d, want 7", to.DP)
	}
}

// TestIngestIsRaceFree exercises the ingest path against a concurrent reader of
// the aggregate, which is what the live tick does. Run with -race.
func TestIngestIsRaceFree(t *testing.T) {
	responseLock.Lock()
	liveAggregate = newAggregate()
	responseDPS = responseDPS[:0]
	responseERR = responseERR[:0]
	retainDPS = false
	responseLock.Unlock()

	var wg sync.WaitGroup
	stop := make(chan struct{})

	for h := 0; h < 6; h++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			host := fmt.Sprintf("10.0.0.%d", id)
			batch := &shared.DataReponseToClient{DPS: make([]shared.DP, 32)}
			for i := range batch.DPS {
				batch.DPS[i] = streamDP(host, uint64(1000+i))
			}
			for {
				select {
				case <-stop:
					return
				default:
				}
				collectDataPointv2(host, batch)
			}
		}(h)
	}

	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			responseLock.Lock()
			_ = liveAggregate.output(false)
			responseLock.Unlock()
		}
	}()

	time.Sleep(time.Second)
	close(stop)
	wg.Wait()

	responseLock.Lock()
	samples := liveAggregate.samples
	retained := len(responseDPS)
	retainDPS = true
	responseLock.Unlock()

	if samples == 0 {
		t.Error("no samples were folded in")
	}
	if retained != 0 {
		t.Errorf("retention was off but %d data points were kept", retained)
	}
}

// TestAnalyzeParsesErrorPoints covers the silent loss in `analyze`: the error
// branch tested the prefix on b[1:], which is always '{', so no error point ever
// matched and `--print-errors` reported none on a file full of them.
func TestAnalyzeParsesErrorPoints(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "round.json")

	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	dp := shared.DP{
		Type: shared.RequestTest, TestID: "rt", Created: time.Unix(1700000000, 0),
		Local: "10.0.0.1", Remote: "10.0.0.2:9010", RMSH: 900, RMSL: 100,
	}
	if _, err := shared.WriteStructAndNewLine(f, shared.DataPoint, dp); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		te := shared.TError{Error: fmt.Sprintf("failure-%d", i), Created: time.Unix(1700000001, 0)}
		if _, err := shared.WriteStructAndNewLine(f, shared.ErrorPoint, te); err != nil {
			t.Fatal(err)
		}
	}
	f.Close()

	dps, errs, err := parseTestFile(path)
	if err != nil {
		t.Fatalf("parseTestFile: %v", err)
	}
	if len(dps) != 1 {
		t.Errorf("got %d data points, want 1", len(dps))
	}
	if len(errs) != 3 {
		t.Fatalf("got %d error points, want 3 -- error points are being dropped", len(errs))
	}
	if errs[0].Error != "failure-0" {
		t.Errorf("errs[0] = %q", errs[0].Error)
	}
}

// TestHostAccountingIsSymmetric guards the counter that decides when a run is
// over. hostsDoingWork reaching zero means "every host reported Done", so a
// host that never connected must not decrement it. It used to: the increment
// happened only for hosts whose first dial succeeded while the decrement ran
// for every goroutine, so with as many dead hosts as live ones the counter hit
// zero and a 300 second run exited successfully after one tick, having saved
// nothing.
func TestHostAccountingIsSymmetric(t *testing.T) {
	hostsDoingWork.Store(0)

	live := &wsClient{ID: 0, Host: "10.0.0.1"}
	dead := &wsClient{ID: 1, Host: "10.0.0.2"}

	// Only the reachable host is ever counted.
	if !live.hold() {
		t.Fatal("hold on a fresh socket did not take effect")
	}
	if got := hostsDoingWork.Load(); got != 1 {
		t.Fatalf("after one live host: %d, want 1", got)
	}

	// The unreachable host's goroutine ends and releases. It never held, so it
	// must not decrement.
	dead.release()
	if got := hostsDoingWork.Load(); got != 1 {
		t.Errorf("an unreachable host decremented the counter: %d, want 1", got)
	}

	// Reconnects must not double count.
	live.hold()
	live.hold()
	if got := hostsDoingWork.Load(); got != 1 {
		t.Errorf("reconnect double counted: %d, want 1", got)
	}

	// And releasing twice must not go negative.
	live.release()
	live.release()
	if got := hostsDoingWork.Load(); got != 0 {
		t.Errorf("after release: %d, want 0", got)
	}
}

// TestReportReadyNeverBlocks covers the wedge in the reconnect path: the
// readiness channel is drained a fixed number of times and then abandoned, and
// each reconnect re-enters the handler, so a blocking send would eventually
// park the goroutine before its read loop -- silently dropping the host from the
// results with nothing left to notice.
//
// This calls the production wsClient.reportReady. An earlier version of this
// test declared its own copy of the closure, which meant it would have passed
// against the blocking implementation it was supposed to be guarding.
func TestReportReadyNeverBlocks(t *testing.T) {
	// One host, so the buffer is one deep, and drain it as initializeClient
	// would.
	ready := make(chan connectResult, 1)
	socket := &wsClient{ID: 0, Host: "10.0.0.1"}

	socket.reportReady(ready, nil)
	got := <-ready
	if got.id != socket.ID || got.err != nil {
		t.Fatalf("first report = %+v, want id 0 and no error", got)
	}

	// Every subsequent report models one reconnect generation against a channel
	// nobody is draining any more. None may block.
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < maxReconnects+5; i++ {
			socket.reportReady(ready, errors.New("flap"))
		}
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("reportReady blocked; a reconnecting host would be dropped from the run")
	}
}

// TestHandshakeTimeoutIsNeverUnbounded covers the reconnect hang: the config
// hardcodes DialTimeout to zero, which websocket.Dialer treats as no timeout,
// so a dial against an address that black-holes packets held the run open until
// the kernel gave up.
func TestHandshakeTimeoutIsNeverUnbounded(t *testing.T) {
	if got := handshakeTimeout(&shared.Config{DialTimeout: 0}); got != defaultDialTimeout {
		t.Errorf("unset DialTimeout gave %s, want %s", got, defaultDialTimeout)
	}
	if got := handshakeTimeout(&shared.Config{DialTimeout: -5}); got != defaultDialTimeout {
		t.Errorf("negative DialTimeout gave %s, want %s", got, defaultDialTimeout)
	}
	// An explicit value is honored, and is read as seconds.
	if got := handshakeTimeout(&shared.Config{DialTimeout: 3}); got != 3*time.Second {
		t.Errorf("DialTimeout 3 gave %s, want 3s", got)
	}
}

func TestFilterSelfRemovesEveryMatch(t *testing.T) {
	hosts := []string{"10.0.0.1", "10.0.0.2", "10.0.0.1", "10.0.0.10"}
	got := filterSelf(hosts, "10.0.0.1")
	for _, h := range got {
		if h == "10.0.0.1" {
			t.Fatalf("filterSelf left a copy of itself behind: %v", got)
		}
	}
	// A prefix match must not be swallowed.
	if len(got) != 2 {
		t.Errorf("got %v, want the two other hosts", got)
	}
}
