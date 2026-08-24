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
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/minio/hperf/shared"
)

// newTestForTest builds a test with synthetic readers and a temporary storage
// path. It deliberately goes through newTest so the validation and file setup
// under examination are the real ones.
func newTestForTest(tb testing.TB, cfg shared.Config, peers ...string) *test {
	tb.Helper()

	dir := tb.TempDir()
	oldBase, oldReal, oldBind := basePath, realIP, bindAddress
	basePath = dir + string(os.PathSeparator)
	realIP = "10.99.0.1"
	bindAddress = "10.99.0.1:9010"
	tb.Cleanup(func() {
		basePath, realIP, bindAddress = oldBase, oldReal, oldBind
		testLock.Lock()
		tests = make([]*test, 0)
		testLock.Unlock()
	})

	if len(peers) == 0 {
		peers = []string{"10.99.0.2", "10.99.0.3"}
	}
	cfg.Hosts = peers
	if cfg.Port == "" {
		cfg.Port = "9010"
	}
	if cfg.PayloadSize == 0 {
		cfg.PayloadSize = 1024
	}
	if cfg.Concurrency == 0 {
		cfg.Concurrency = 2
	}

	t, err := newTest(cfg)
	if err != nil {
		tb.Fatalf("newTest: %v", err)
	}
	return t
}

// TestConsAndStateAreRaceFree covers the crash this work started from: cons was
// written by two goroutines while a third iterated and deleted from it, which
// is an unrecoverable runtime throw rather than a catchable panic. Run with
// -race.
func TestConsAndStateAreRaceFree(t *testing.T) {
	tst := newTestForTest(t, shared.Config{TestType: shared.StreamTest, TestID: "racetest"})

	var wg sync.WaitGroup
	stop := make(chan struct{})
	work := func(f func(i int)) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; ; i++ {
				select {
				case <-stop:
					return
				default:
				}
				f(i)
			}
		}()
	}

	// A client attaching, the documented --id reattach flow.
	work(func(int) { tst.attach(&wsPeer{pid: fmt.Sprint(time.Now().UnixNano())}) })
	// The sampling loop shipping stats.
	work(func(int) { sendAndSaveData(tst) })
	// Request goroutines reporting failures.
	work(func(i int) { tst.AddError(errors.New("boom"), fmt.Sprint(i%8)) })
	// The sampler producing data points.
	work(func(int) { generateDataPoints(tst) })
	// Another command walking the test list.
	work(func(int) { _ = snapshotTests() })

	time.Sleep(1500 * time.Millisecond)
	close(stop)
	wg.Wait()
}

// TestStreamReaderReportsEveryInterval guards the trap in the Read fast path:
// hasStats has to be set on every chunk, not only alongside the one-shot TTFB
// registration. A stream request never completes, so gating it on the first
// chunk silently deletes every bandwidth data point after the first interval.
func TestStreamReaderReportsEveryInterval(t *testing.T) {
	cfg := shared.Config{TestType: shared.StreamTest, TestID: "streamintervals"}
	tst := newTestForTest(t, cfg, "10.99.0.2")
	r := tst.Readers[0]

	ar := &asyncReader{pr: r, c: &tst.Config, ctx: context.Background(), start: time.Now()}
	buf := make([]byte, 512)

	for interval := 1; interval <= 3; interval++ {
		// One request, many chunks -- what a stream test actually does.
		for i := 0; i < 20; i++ {
			if _, err := ar.Read(buf); err != nil {
				t.Fatalf("interval %d: read: %v", interval, err)
			}
		}
		r.lastDataPointTime = time.Now().Add(-time.Second)

		tst.DPS = nil
		generateDataPoints(tst)

		tst.M.Lock()
		got := len(tst.DPS)
		tst.M.Unlock()
		if got != 1 {
			t.Fatalf("interval %d: got %d data points, want 1 -- the reader stopped reporting", interval, got)
		}
	}
}

// TestGenerateDataPointsRateUsesMeasuredWindow pins the property that makes the
// "statistics stall depresses throughput" theory false: the rate divides by the
// measured window, so a slow sampling loop cannot bias it.
func TestGenerateDataPointsRateUsesMeasuredWindow(t *testing.T) {
	const offered = 100 << 20 // bytes per second

	for i, window := range []time.Duration{time.Second, 1500 * time.Millisecond, 3 * time.Second, 10 * time.Second} {
		tst := newTestForTest(t, shared.Config{
			TestType: shared.StreamTest,
			TestID:   fmt.Sprintf("ratewindow-%d", i),
		}, "10.99.0.2")
		r := tst.Readers[0]
		r.TX.Store(uint64(float64(offered) * window.Seconds()))
		r.hasStats.Store(true)
		r.lastDataPointTime = time.Now().Add(-window)

		generateDataPoints(tst)

		tst.M.Lock()
		dps := tst.DPS
		tst.M.Unlock()
		if len(dps) != 1 {
			t.Fatalf("window %s: got %d data points", window, len(dps))
		}
		errPct := (float64(dps[0].TX) - offered) / offered * 100
		if math.Abs(errPct) > 1 {
			t.Errorf("window %s: rate off by %.2f%% (got %d)", window, errPct, dps[0].TX)
		}
	}
}

// TestShortWindowProducesNoDataPoint guards against a rate computed over a
// window too short to mean anything. The final flush lands microseconds after
// the last scheduled sample, and bytes/elapsed over that window yields a figure
// orders of magnitude above line rate -- which then becomes TX(max). Measured
// at 2.37 GB/s against a 726 MB/s steady state before this guard.
func TestShortWindowProducesNoDataPoint(t *testing.T) {
	tst := newTestForTest(t, shared.Config{
		TestType: shared.StreamTest, TestID: "shortwindow",
	}, "10.99.0.2")
	r := tst.Readers[0]

	// A full window emits, and the bytes are attributed to it.
	r.TX.Store(100 << 20)
	r.hasStats.Store(true)
	r.lastDataPointTime = time.Now().Add(-time.Second)
	generateDataPoints(tst)

	tst.M.Lock()
	first := len(tst.DPS)
	tst.M.Unlock()
	if first != 1 {
		t.Fatalf("full window produced %d data points, want 1", first)
	}

	// An immediate second pass is the final-flush case: same bytes rate, but
	// microseconds of window. It must not emit.
	r.TX.Store(352000)
	r.hasStats.Store(true)
	generateDataPoints(tst)

	tst.M.Lock()
	second := len(tst.DPS)
	tst.M.Unlock()
	if second != first {
		tst.M.Lock()
		rate := tst.DPS[len(tst.DPS)-1].TX
		tst.M.Unlock()
		t.Errorf("sub-threshold window emitted a data point reporting %s",
			shared.BWToString(rate))
	}

	// The skipped bytes must still be there, not silently dropped, so the next
	// real sample accounts for them.
	if got := r.TX.Load(); got != 352000 {
		t.Errorf("skipped window lost its bytes: TX = %d, want 352000", got)
	}
	if !r.hasStats.Load() {
		t.Error("skipped window cleared hasStats, so the reader would miss its next sample")
	}
}

// TestPersistedRecordFormat is the on-disk golden: one prefix byte, the JSON,
// then a newline. Anything that changes this breaks download, analyze and csv.
func TestPersistedRecordFormat(t *testing.T) {
	tst := newTestForTest(t, shared.Config{
		TestType: shared.StreamTest, TestID: "formatgolden", Save: true,
	}, "10.99.0.2")

	dp := shared.DP{
		Type: shared.StreamTest, TestID: "formatgolden",
		Created: time.Unix(1700000000, 0).UTC(),
		Local:   "10.99.0.1", Remote: "10.99.0.2:9010",
		TX: 1000, TXTotal: 1000, TXCount: 3, DroppedPackets: -1,
	}
	terr := shared.TError{Error: "boom", Created: time.Unix(1700000001, 0).UTC()}

	persist(tst, []shared.DP{dp}, []shared.TError{terr})
	if err := tst.DataFile.Sync(); err != nil {
		t.Fatal(err)
	}

	raw, err := os.ReadFile(tst.DataFile.Name())
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSuffix(string(raw), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("got %d lines, want 2: %q", len(lines), raw)
	}
	if lines[0][0] != '0' {
		t.Errorf("data point prefix = %q, want '0'", lines[0][0])
	}
	if lines[1][0] != '1' {
		t.Errorf("error point prefix = %q, want '1'", lines[1][0])
	}

	var roundTripped shared.DP
	if err := json.Unmarshal([]byte(lines[0][1:]), &roundTripped); err != nil {
		t.Fatalf("data point does not parse: %v", err)
	}
	if roundTripped.TXCount != dp.TXCount || roundTripped.Remote != dp.Remote {
		t.Errorf("round trip mismatch: %+v", roundTripped)
	}
	var errRoundTripped shared.TError
	if err := json.Unmarshal([]byte(lines[1][1:]), &errRoundTripped); err != nil {
		t.Fatalf("error point does not parse: %v", err)
	}
	if errRoundTripped.Error != "boom" {
		t.Errorf("error round trip mismatch: %+v", errRoundTripped)
	}
}

// TestResetTestFilesIsAnchored covers silent data loss: the cleanup glob had no
// separator, so starting "--id test" deleted every saved test whose ID merely
// began with "test".
func TestResetTestFilesIsAnchored(t *testing.T) {
	dir := t.TempDir()
	oldBase := basePath
	basePath = dir + string(os.PathSeparator)
	t.Cleanup(func() { basePath = oldBase })

	keep := []string{"test2.1", "testing.1", "other.1"}
	for _, name := range append([]string{"test.1", "test.2"}, keep...) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	if err := resetTestFiles(&test{ID: "test"}); err != nil {
		t.Fatalf("resetTestFiles: %v", err)
	}

	for _, name := range keep {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Errorf("%s was deleted by a test with id \"test\"", name)
		}
	}
	for _, name := range []string{"test.1", "test.2"} {
		if _, err := os.Stat(filepath.Join(dir, name)); !os.IsNotExist(err) {
			t.Errorf("%s should have been removed", name)
		}
	}
}

// TestUnsafeTestIDRejected keeps a client-supplied id from escaping the storage
// directory, and keeps an empty id from matching every saved test.
func TestUnsafeTestIDRejected(t *testing.T) {
	for _, id := range []string{"", "../escape", "a/b", "a*b", ".", "..", strings.Repeat("x", 65)} {
		if err := shared.ValidateTestID(id); err == nil {
			t.Errorf("ValidateTestID(%q) = nil, want an error", id)
		}
	}
	for _, id := range []string{"1755600000", "bandwidth-30", "my_test.1", "A-b_c.9"} {
		if err := shared.ValidateTestID(id); err != nil {
			t.Errorf("ValidateTestID(%q) = %v, want nil", id, err)
		}
	}
}

func openFDs(tb testing.TB) int {
	tb.Helper()
	if runtime.GOOS != "linux" {
		tb.Skip("fd counting needs /proc")
	}
	entries, err := os.ReadDir("/proc/self/fd")
	if err != nil {
		tb.Fatal(err)
	}
	return len(entries)
}

// TestStreamOneTestFileClosesFile covers the descriptor leak: the file was
// opened inside the caller's loop with no Close, so every download cost the
// server one permanently open descriptor per file.
func TestStreamOneTestFileClosesFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "leak.1")
	if err := os.WriteFile(path, []byte("0{}\n0{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	// A peer with no connection makes every write fail, which exercises the
	// early-return path that leaked in addition to the normal one.
	peer := &wsPeer{pid: "p"}
	before := openFDs(t)
	for i := 0; i < 200; i++ {
		_ = streamOneTestFile(peer, new(shared.WebsocketSignal), path)
	}
	if after := openFDs(t); after > before+2 {
		t.Errorf("descriptors grew from %d to %d over 200 calls", before, after)
	}
}

// TestNonOKResponseReleasesConnection covers the other unbounded leak: a non-200
// reply returned without closing the body, so net/http could never reuse or
// release the connection and each failure burned an fd and two goroutines.
func TestNonOKResponseReleasesConnection(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io_Copy_Discard(r)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	cfg := shared.Config{
		TestType: shared.RequestTest, TestID: "leakcheck",
		Insecure: true, PayloadSize: 512, Concurrency: 1,
	}
	host := strings.TrimPrefix(srv.URL, "http://")
	hostOnly, port, _ := strings.Cut(host, ":")
	tst := newTestForTest(t, cfg, hostOnly)
	tst.Config.Port = port

	r := newPerformanceReaderForASingleHost(tst.Config, hostOnly, port)
	tst.Readers = []*netPerfReader{r}

	// sendRequestToHost returns its slot to the concurrency semaphore on the
	// way out, and the semaphore starts full, so a direct call has to take a
	// slot first or the return blocks forever.
	call := func() {
		cid := <-r.concurrency
		sendRequestToHost(tst, r, cid)
	}

	// Warm up so the initial pool allocations are not counted.
	for i := 0; i < 20; i++ {
		call()
	}
	before := openFDs(t)
	beforeG := runtime.NumGoroutine()

	for i := 0; i < 300; i++ {
		call()
	}

	time.Sleep(200 * time.Millisecond)
	after := openFDs(t)
	afterG := runtime.NumGoroutine()

	if after > before+8 {
		t.Errorf("descriptors grew from %d to %d over 300 failed requests", before, after)
	}
	if afterG > beforeG+16 {
		t.Errorf("goroutines grew from %d to %d over 300 failed requests", beforeG, afterG)
	}
}

func io_Copy_Discard(r *http.Request) (int64, error) {
	defer r.Body.Close()
	buf := make([]byte, 32*1024)
	var total int64
	for {
		n, err := r.Body.Read(buf)
		total += int64(n)
		if err != nil {
			return total, nil
		}
	}
}

// TestFinishReclaimsReaderBuffers covers the retention leak: a finished test
// kept one PayloadSize buffer and one http.Client per peer alive for the
// server's lifetime.
func TestFinishReclaimsReaderBuffers(t *testing.T) {
	tst := newTestForTest(t, shared.Config{
		TestType: shared.StreamTest, TestID: "reclaim", PayloadSize: 1 << 20,
	}, "10.99.0.2", "10.99.0.3")

	for _, r := range tst.Readers {
		if len(r.buf) == 0 {
			t.Fatal("reader has no payload buffer before finish")
		}
	}

	tst.finish()

	// The readers must be released by dropping the reference, not by zeroing
	// fields on them: request goroutines still hold their own pointers and may
	// be inside Read, copying from r.buf.
	if tst.Readers != nil {
		t.Errorf("finish() left %d readers referenced, so their payload buffers cannot be collected", len(tst.Readers))
	}
	if tst.live() {
		t.Error("test still reports itself as live after finish")
	}
	if tst.attach(&wsPeer{pid: "late"}) {
		t.Error("attach succeeded on a finished test")
	}
}

// TestFinishLeavesPeersWritable guards a subtle liveness bug: createAndRunTest
// sends Done over the peer that started the test, and that send happens after
// finish() runs. If finish() retired the attached peers, the write would be
// refused, the client would never see Done, and every run would hang until its
// grace period expired instead of completing.
func TestFinishLeavesPeersWritable(t *testing.T) {
	tst := newTestForTest(t, shared.Config{
		TestType: shared.StreamTest, TestID: "donepath",
	}, "10.99.0.2")

	peer := &wsPeer{pid: "starter"}
	if !tst.attach(peer) {
		t.Fatal("attach failed on a live test")
	}

	tst.finish()

	if peer.dead.Load() {
		t.Fatal("finish() retired an attached peer, so Done can never be sent")
	}
	// The test must also have stopped tracking it, so nothing writes to a
	// client the test no longer owns.
	tst.M.Lock()
	remaining := len(tst.cons)
	tst.M.Unlock()
	if remaining != 0 {
		t.Errorf("finish() left %d peers attached", remaining)
	}
}

// TestDuplicateLiveTestIDRejected stops a second run from clobbering the files
// of one already in progress.
func TestDuplicateLiveTestIDRejected(t *testing.T) {
	tst := newTestForTest(t, shared.Config{
		TestType: shared.StreamTest, TestID: "dupe", Save: true,
	}, "10.99.0.2")

	if _, err := newTest(tst.Config); err == nil {
		t.Error("a second test with a live id was accepted")
	}

	tst.finish()
	if _, err := newTest(tst.Config); err != nil {
		t.Errorf("reusing the id of a finished test should be allowed: %v", err)
	}
}
