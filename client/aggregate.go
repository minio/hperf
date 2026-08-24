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
	"fmt"
	"math"
	"slices"
	"strings"

	"github.com/minio/hperf/shared"
)

// aggregate maintains the live summary incrementally, updated once per incoming
// data point and read once per tick.
//
// The live table used to be built by rescanning every data point received so
// far, on every tick. The client receives hosts*(hosts-1) data points a second,
// so that rescan was ~1.9 million row visits over a 30 second run at 64 hosts
// and ~2.9 billion over a 300 second run at 256 hosts -- and it read the slice
// without holding the lock that guards appends to it.
//
// All fields are guarded by responseLock.
type aggregate struct {
	testType shared.TestType
	samples  uint64
	errCount int

	txSum   uint64
	txMin   uint64
	txMax   uint64
	txTotal uint64
	txCount uint64

	rmsMin  int64
	rmsMax  int64
	ttfbMin int64
	ttfbMax int64
	rmsN    uint64
	ttfbN   uint64

	memMin int
	memMax int
	cpuMin int
	cpuMax int
	hostN  int

	// dropped is the largest per-test drop delta any host has reported, or -1
	// while no host has reported a usable counter.
	dropped int

	perHost map[string]*shared.HostAverage
}

func newAggregate() *aggregate {
	return &aggregate{
		txMin:   math.MaxUint64,
		rmsMin:  math.MaxInt64,
		ttfbMin: math.MaxInt64,
		memMin:  math.MaxInt,
		cpuMin:  math.MaxInt,
		dropped: -1,
		perHost: make(map[string]*shared.HostAverage),
	}
}

// noSample reports whether a low-watermark field carries the "nothing measured
// in this interval" sentinel rather than a measurement. Servers seed the low
// watermarks to MaxInt64 and reset them every interval, and a stream test never
// completes a request, so its RMS fields are sentinel for the whole run.
func noSample(v int64) bool { return v == math.MaxInt64 || v <= 0 }

// add folds one data point in. host is the address the client dialed, used as
// the sender identity: DP.Local is whatever the server thinks it is, which is
// the same wildcard string on every node when --real-ip was not set.
func (a *aggregate) add(host string, dp shared.DP) {
	if a.samples == 0 {
		a.testType = dp.Type
	}
	a.samples++

	a.txSum += dp.TX
	a.txTotal += dp.TXTotal
	a.txCount += dp.TXCount
	a.txMin = min(a.txMin, dp.TX)
	a.txMax = max(a.txMax, dp.TX)

	if !noSample(dp.RMSL) {
		a.rmsMin = min(a.rmsMin, dp.RMSL)
		a.rmsN++
	}
	if dp.RMSH > 0 {
		a.rmsMax = max(a.rmsMax, dp.RMSH)
	}
	if !noSample(dp.TTFBL) {
		a.ttfbMin = min(a.ttfbMin, dp.TTFBL)
		a.ttfbN++
	}
	if dp.TTFBH > 0 {
		a.ttfbMax = max(a.ttfbMax, dp.TTFBH)
	}

	a.memMin = min(a.memMin, dp.MemoryUsedPercent)
	a.memMax = max(a.memMax, dp.MemoryUsedPercent)
	a.cpuMin = min(a.cpuMin, dp.CPUUsedPercent)
	a.cpuMax = max(a.cpuMax, dp.CPUUsedPercent)

	if dp.DroppedPackets >= 0 {
		a.dropped = max(a.dropped, dp.DroppedPackets)
	}

	if host == "" {
		host = shared.HostOnly(dp.Local)
	}
	h, ok := a.perHost[host]
	if !ok {
		h = &shared.HostAverage{Host: host, TXMin: math.MaxUint64}
		a.perHost[host] = h
		a.hostN++
	}
	h.Samples++
	h.TXSum += dp.TX
	h.TXTotal += dp.TXTotal
	h.TXMin = min(h.TXMin, dp.TX)
	h.TXMax = max(h.TXMax, dp.TX)
}

func (a *aggregate) addErrors(n int) { a.errCount += n }

// output renders the accumulated state. micro leaves the timers in
// microseconds; otherwise they are converted to milliseconds, matching what the
// per-data-point tables do.
func (a *aggregate) output(micro bool) *shared.TestOutput {
	to := &shared.TestOutput{
		ErrCount: a.errCount,
		Samples:  a.samples,
		TXC:      a.txCount,
		TXT:      a.txTotal,
		DP:       a.dropped,
	}
	if a.samples == 0 {
		return to
	}

	to.TXH = a.txMax
	to.TXA = a.txSum / a.samples
	if a.txMin != math.MaxUint64 {
		to.TXL = a.txMin
	}

	if a.rmsN > 0 {
		to.RMSL = a.rmsMin
	}
	to.RMSH = a.rmsMax
	if a.ttfbN > 0 {
		to.TTFBL = a.ttfbMin
	}
	to.TTFBH = a.ttfbMax

	if a.memMin != math.MaxInt {
		to.ML = a.memMin
	}
	to.MH = a.memMax
	if a.cpuMin != math.MaxInt {
		to.CL = a.cpuMin
	}
	to.CH = a.cpuMax

	if !micro {
		to.TTFBH /= 1000
		to.TTFBL /= 1000
		to.RMSH /= 1000
		to.RMSL /= 1000
	}
	return to
}

// hosts returns the per-host breakdown, slowest average first, so the hosts
// worth investigating are at the top.
func (a *aggregate) hosts() []shared.HostAverage {
	out := make([]shared.HostAverage, 0, len(a.perHost))
	for _, h := range a.perHost {
		c := *h
		if c.TXMin == math.MaxUint64 {
			c.TXMin = 0
		}
		out = append(out, c)
	}
	slices.SortFunc(out, func(x, y shared.HostAverage) int {
		if c := cmpUint(x.Avg(), y.Avg()); c != 0 {
			return c
		}
		return strings.Compare(x.Host, y.Host)
	})
	return out
}

func cmpUint(a, b uint64) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	default:
		return 0
	}
}

// fleetAverage is the mean flow rate across every host, used to decide which
// hosts are worth flagging.
func (a *aggregate) fleetAverage() uint64 {
	if a.samples == 0 {
		return 0
	}
	return a.txSum / a.samples
}

// printHostAverages prints the per-host throughput breakdown once a run ends.
// The live row is a whole-run summary across every flow; this is where you see
// which host is dragging it down. It takes a snapshot rather than the aggregate
// so the caller can release responseLock before doing terminal I/O.
func printHostAverages(hosts []shared.HostAverage, fleet uint64) {
	if len(hosts) == 0 {
		return
	}

	fmt.Println("")
	fmt.Println(" Per-host throughput (one row per host, slowest average first)")
	fmt.Println("")

	printHeader([]HeaderField{Local, TXA, TXL, TXH, TXT, Samples})
	for i := range hosts {
		h := hosts[i]
		style := BaseStyle
		// Flag any host averaging under half of the fleet-wide average: at
		// scale that is the signal worth chasing, and it is invisible in a
		// single aggregate number.
		if fleet > 0 && h.Avg()*2 < fleet {
			style = WarningStyle
		}
		PrintColumns(
			style,
			column{h.Host, headerSlice[Local].width},
			column{shared.BWToString(h.Avg()), headerSlice[TXA].width},
			column{shared.BWToString(h.TXMin), headerSlice[TXL].width},
			column{shared.BWToString(h.TXMax), headerSlice[TXH].width},
			column{shared.BToString(h.TXTotal), headerSlice[TXT].width},
			column{formatUint(h.Samples), headerSlice[Samples].width},
		)
	}
	fmt.Println("")
}
