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
	"strconv"
	"sync/atomic"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/minio/hperf/shared"
)

type header struct {
	label string
	width int
}

type column struct {
	value any
	width int
}

var headerSlice = make([]header, header_length)

type HeaderField int

const (
	IntNumber HeaderField = iota
	Created
	Local
	Remote
	RMSH
	RMSL
	TTFBH
	TTFBL
	TX
	TXH
	TXL
	TXA
	TXT
	TXCount
	ErrCount
	DroppedPackets
	MemoryUsage
	MemoryHigh
	MemoryLow
	CPUUsage
	CPUHigh
	CPULow
	ID
	HumanTime
	Samples
	header_length
)

// The two host columns are the only header widths that change after init: they
// grow to fit the longest address seen. They are read by the end-of-run summary
// while reader goroutines may still be widening them, and growHostColumns is
// called both under responseLock and outside it, so they live outside
// headerSlice as atomics. Every other entry in headerSlice is immutable once
// init has run, which is what makes reading the rest without synchronization
// safe.
var (
	localColWidth  atomic.Int64
	remoteColWidth atomic.Int64
)

// Headers are built once at package init. They used to be built lazily on the
// first data point, from whichever goroutine got there first, while the live
// ticker goroutine was already reading widths -- a race, and one that produced
// an unpadded row if a tick landed before any data point.
func init() {
	resetHeaders()
}

// resetHeaders builds the static table and seeds the two mutable widths from
// it. Tests use it to get back to a known state; nothing else should.
func resetHeaders() {
	initHeaders()
	// Seed from the static table, not via colWidth: the atomics are what
	// colWidth reads for these two fields, and they are still zero here.
	localColWidth.Store(int64(headerSlice[Local].width))
	remoteColWidth.Store(int64(headerSlice[Remote].width))
}

// colWidth returns the render width of a header field, reading the two mutable
// host columns atomically. Always use this rather than headerSlice[f].width.
func colWidth(f HeaderField) int {
	switch f {
	case Local:
		return int(localColWidth.Load())
	case Remote:
		return int(remoteColWidth.Load())
	default:
		return headerSlice[f].width
	}
}

// growTo widens w to at least n and reports whether it changed it. The compare
// and swap matters: growHostColumns has more than one caller and they are not
// all under the same lock, so a plain load-then-store could drop a widening.
func growTo(w *atomic.Int64, n int) bool {
	for {
		cur := w.Load()
		if int64(n) <= cur {
			return false
		}
		if w.CompareAndSwap(cur, int64(n)) {
			return true
		}
	}
}

func initHeaders() {
	headerSlice[IntNumber] = header{"#", 5}
	headerSlice[Created] = header{"Created", 8}
	headerSlice[Local] = header{"Local", 15}
	headerSlice[Remote] = header{"Remote", 15}
	headerSlice[RMSH] = header{"RMS(high)", 9}
	headerSlice[RMSL] = header{"RMS(low)", 9}
	headerSlice[TTFBH] = header{"TTFB(high)", 9}
	headerSlice[TTFBL] = header{"TTFB(low)", 9}
	// BWToString emits up to 11 characters ("999.99 GB/s") and PrintColumns
	// pads but never truncates, so a width of 10 shifted every later column
	// right once a test reached GB/s.
	headerSlice[TX] = header{"TX", 11}
	headerSlice[TXL] = header{"TX(min)", 11}
	headerSlice[TXH] = header{"TX(max)", 11}
	headerSlice[TXA] = header{"TX(avg)", 11}
	headerSlice[TXT] = header{"TX(total)", 15}
	headerSlice[TXCount] = header{"#TX", 10}
	headerSlice[ErrCount] = header{"#ERR", 6}
	headerSlice[DroppedPackets] = header{"#Dropped", 9}
	headerSlice[MemoryUsage] = header{"Mem(used)", 9}
	headerSlice[MemoryHigh] = header{"Mem(high)", 9}
	headerSlice[MemoryLow] = header{"Mem(low)", 9}
	headerSlice[CPUUsage] = header{"CPU(used)", 9}
	headerSlice[CPUHigh] = header{"CPU(high)", 9}
	headerSlice[CPULow] = header{"CPU(low)", 9}
	headerSlice[ID] = header{"ID", 30}
	headerSlice[HumanTime] = header{"Time", 30}
	headerSlice[Samples] = header{"#Samples", 9}
}

// growHostColumns widens the two host columns to fit the addresses seen so far,
// and reports whether either changed so the caller can reprint the header.
func growHostColumns(dps []shared.DP) (grew bool) {
	for i := range dps {
		if growTo(&localColWidth, len(shared.HostOnly(dps[i].Local))) {
			grew = true
		}
		if growTo(&remoteColWidth, len(shared.HostOnly(dps[i].Remote))) {
			grew = true
		}
	}
	return
}

func GenerateFormatString(columnCount int) (fs string) {
	for i := 0; i < columnCount; i++ {
		fs += "%-*s "
	}
	return
}

var (
	ListHeaders          = []HeaderField{IntNumber, ID, HumanTime}
	BandwidthHeaders     = []HeaderField{Created, Local, Remote, TX, ErrCount, DroppedPackets, MemoryUsage, CPUUsage}
	LatencyHeaders       = []HeaderField{Created, Local, Remote, RMSH, RMSL, TTFBH, TTFBL, TX, TXCount, ErrCount, DroppedPackets, MemoryUsage, CPUUsage}
	FullDataPointHeaders = []HeaderField{Created, Local, Remote, RMSH, RMSL, TTFBH, TTFBL, TX, TXCount, ErrCount, DroppedPackets, MemoryUsage, CPUUsage}

	// TX(avg) is inserted next to the existing extremes; every other column
	// keeps its position so the live output stays recognizable.
	RealTimeBandwidthHeaders = []HeaderField{ErrCount, TXCount, TXH, TXL, TXA, TXT, DroppedPackets, MemoryHigh, MemoryLow, CPUHigh, CPULow}
	RealTimeLatencyHeaders   = []HeaderField{ErrCount, TXCount, TXH, TXL, TXA, TXT, RMSH, RMSL, TTFBH, TTFBL, DroppedPackets, MemoryHigh, MemoryLow, CPUHigh, CPULow}
)

var (
	HeaderStyle  = lipgloss.NewStyle().Background(lipgloss.Color("#F2F2F2")).Foreground(lipgloss.Color("#000000"))
	BaseStyle    = lipgloss.NewStyle().Background(lipgloss.Color("#000000")).Foreground(lipgloss.Color("#F2F2F2"))
	SuccessStyle = lipgloss.NewStyle().Background(lipgloss.Color("#009900")).Foreground(lipgloss.Color("#F2F2F2"))
	WarningStyle = lipgloss.NewStyle().Background(lipgloss.Color("#999900")).Foreground(lipgloss.Color("#F2F2F2"))
	ErrorStyle   = lipgloss.NewStyle().Background(lipgloss.Color("#AA0000")).Foreground(lipgloss.Color("#FFFFFF"))
)

func printHeader(fields []HeaderField) {
	fs := GenerateFormatString(len(fields))
	hs := make([]any, 0)
	for i := range fields {
		hs = append(hs, colWidth(fields[i]), headerSlice[fields[i]].label)
	}

	fmt.Println(HeaderStyle.Render(fmt.Sprintf(fs, hs...)))
}

func PrintPercentilesHeader(style lipgloss.Style, tag string, dps []int64, c shared.Config) {
	fs := GenerateFormatString(6)
	hs := []any{
		4, tag,
		10, "count",
		10, "sum",
		10, "min",
		10, "avg",
		10, "max",
	}
	fmt.Println(style.Render(
		fmt.Sprintf(fs, hs...),
	))
}

func PrintPercentiles(style lipgloss.Style, tag string, dps []int64, c shared.Config) {
	PrintPercentilesHeader(style, tag, dps, c)
	fs := GenerateFormatString(6)
	hs := make([]any, 12)
	hs[0] = 4
	hs[1] = ""
	hs[2] = 10
	hs[3] = formatInt(dps[0])
	hs[4] = 10
	hs[6] = 10
	hs[8] = 10
	hs[10] = 10

	if c.Micro {
		hs[5] = formatInt(dps[1])
		hs[7] = formatInt(dps[2])
		hs[9] = formatInt(dps[3])
		hs[11] = formatInt(dps[4])
	} else {
		hs[5] = formatInt(dps[1] / 1000)
		hs[7] = formatInt(dps[2] / 1000)
		hs[9] = formatInt(dps[3] / 1000)
		hs[11] = formatInt(dps[4] / 1000)
	}

	fmt.Println(BaseStyle.Render(
		fmt.Sprintf(fs, hs...),
	))
}

func PrintColumns(style lipgloss.Style, columns ...column) {
	fs := GenerateFormatString(len(columns))
	hs := make([]any, 0)
	for i := range columns {
		hs = append(hs, columns[i].width, columns[i].value)
	}
	fmt.Println(style.Render(
		fmt.Sprintf(fs, hs...),
	))
}

func printDataPointHeaders(t shared.TestType) {
	switch t {
	case shared.StreamTest:
		printHeader(BandwidthHeaders)
	case shared.RequestTest:
		printHeader(LatencyHeaders)
	default:
		printHeader(FullDataPointHeaders)
	}
}

func printRealTimeHeaders(t shared.TestType) {
	switch t {
	case shared.StreamTest:
		printHeader(RealTimeBandwidthHeaders)
	case shared.RequestTest:
		printHeader(RealTimeLatencyHeaders)
	default:
	}
}

func printRealTimeRow(style lipgloss.Style, entry *shared.TestOutput, t shared.TestType) {
	switch t {
	case shared.StreamTest:
		PrintColumns(
			style,
			column{formatInt(int64(entry.ErrCount)), colWidth(ErrCount)},
			column{formatUint(entry.TXC), colWidth(TXCount)},
			column{shared.BWToString(entry.TXH), colWidth(TXH)},
			column{shared.BWToString(entry.TXL), colWidth(TXL)},
			column{shared.BWToString(entry.TXA), colWidth(TXA)},
			column{shared.BToString(entry.TXT), colWidth(TXT)},
			column{formatInt(int64(entry.DP)), colWidth(DroppedPackets)},
			column{formatInt(int64(entry.MH)), colWidth(MemoryHigh)},
			column{formatInt(int64(entry.ML)), colWidth(MemoryLow)},
			column{formatInt(int64(entry.CH)), colWidth(CPUHigh)},
			column{formatInt(int64(entry.CL)), colWidth(CPULow)},
		)
		return
	case shared.RequestTest:
		PrintColumns(
			style,
			column{formatInt(int64(entry.ErrCount)), colWidth(ErrCount)},
			column{formatUint(entry.TXC), colWidth(TXCount)},
			column{shared.BWToString(entry.TXH), colWidth(TXH)},
			column{shared.BWToString(entry.TXL), colWidth(TXL)},
			column{shared.BWToString(entry.TXA), colWidth(TXA)},
			column{shared.BToString(entry.TXT), colWidth(TXT)},
			column{formatInt(entry.RMSH), colWidth(RMSH)},
			column{formatInt(entry.RMSL), colWidth(RMSL)},
			column{formatInt(entry.TTFBH), colWidth(TTFBH)},
			column{formatInt(entry.TTFBL), colWidth(TTFBL)},
			column{formatInt(int64(entry.DP)), colWidth(DroppedPackets)},
			column{formatInt(int64(entry.MH)), colWidth(MemoryHigh)},
			column{formatInt(int64(entry.ML)), colWidth(MemoryLow)},
			column{formatInt(int64(entry.CH)), colWidth(CPUHigh)},
			column{formatInt(int64(entry.CL)), colWidth(CPULow)},
		)
	default:
		shared.DEBUG("Unknown test type, not printing table")
	}
}

func printTableRow(style lipgloss.Style, entry *shared.DP, t shared.TestType) {
	switch t {
	case shared.StreamTest:
		PrintColumns(
			style,
			column{entry.Created.Format("15:04:05"), colWidth(Created)},
			column{shared.HostOnly(entry.Local), colWidth(Local)},
			column{shared.HostOnly(entry.Remote), colWidth(Remote)},
			column{shared.BWToString(entry.TX), colWidth(TX)},
			column{formatInt(int64(entry.ErrCount)), colWidth(ErrCount)},
			column{formatInt(int64(entry.DroppedPackets)), colWidth(DroppedPackets)},
			column{formatInt(int64(entry.MemoryUsedPercent)), colWidth(MemoryUsage)},
			column{formatInt(int64(entry.CPUUsedPercent)), colWidth(CPUUsage)},
		)
		return
	case shared.RequestTest:
		PrintColumns(
			style,
			column{entry.Created.Format("15:04:05"), colWidth(Created)},
			column{shared.HostOnly(entry.Local), colWidth(Local)},
			column{shared.HostOnly(entry.Remote), colWidth(Remote)},
			column{formatInt(entry.RMSH), colWidth(RMSH)},
			column{formatInt(entry.RMSL), colWidth(RMSL)},
			column{formatInt(entry.TTFBH), colWidth(TTFBH)},
			column{formatInt(entry.TTFBL), colWidth(TTFBL)},
			column{shared.BWToString(entry.TX), colWidth(TX)},
			column{formatUint(entry.TXCount), colWidth(TXCount)},
			column{formatInt(int64(entry.ErrCount)), colWidth(ErrCount)},
			column{formatInt(int64(entry.DroppedPackets)), colWidth(DroppedPackets)},
			column{formatInt(int64(entry.MemoryUsedPercent)), colWidth(MemoryUsage)},
			column{formatInt(int64(entry.CPUUsedPercent)), colWidth(CPUUsage)},
		)
	default:
		shared.DEBUG("Unknown test type, not printing table")
	}
}

// ingest folds a batch into the live aggregate and, when retention is on, into
// the data point slice. Callers must hold responseLock.
func ingest(host string, r *shared.DataReponseToClient) {
	for i := range r.DPS {
		liveAggregate.add(host, r.DPS[i])
	}
	liveAggregate.addErrors(len(r.Errors))

	if retainDPS {
		responseDPS = append(responseDPS, r.DPS...)
	}
	responseERR = append(responseERR, r.Errors...)
}

func collectDataPointv2(host string, r *shared.DataReponseToClient) {
	if r == nil {
		return
	}

	responseLock.Lock()
	defer responseLock.Unlock()

	ingest(host, r)
}

func printAndCollectDataPoints(host string, r *shared.DataReponseToClient, c *shared.Config) {
	if r == nil {
		return
	}

	responseLock.Lock()
	defer responseLock.Unlock()

	// This guarantees we are always printing the
	// same header types as the data point types.
	if len(r.DPS) > 0 {
		c.TestType = r.DPS[0].Type
	}
	grew := growHostColumns(r.DPS)
	if printedRows > 0 {
		if grew || printedRows%10 == 0 {
			printDataPointHeaders(c.TestType)
		}
	} else {
		if len(r.DPS) > 0 {
			printDataPointHeaders(c.TestType)
		}
	}

	for i := range r.DPS {
		r.DPS[i].Received = time.Now()
		entry := r.DPS[i]
		printTableRow(BaseStyle, &entry, entry.Type)
		printedRows++
	}

	for i := range r.Errors {
		PrintTError(r.Errors[i])
	}

	ingest(host, r)
}

// printedRows counts rows emitted by the attached-client view. It used to be
// derived from len(responseDPS), which stops being a row count as soon as
// retention is off. Guarded by responseLock.
var printedRows int

// Helper functions to format int/uint values for table display
func formatInt(val int64) string {
	return strconv.FormatInt(val, 10)
}

func formatUint(val uint64) string {
	return strconv.FormatUint(val, 10)
}
