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
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/minio/hperf/shared"
)

type header struct {
	label string
	width int
}

type column struct {
	value interface{}
	width int
}

var headerSlice = make([]header, 16)

type HeaderField int

const (
	IntNumber HeaderField = iota
	Created
	Local
	Remote
	PMSH
	PMSL
	TTFBH
	TTFBL
	TX
	TXCount
	ErrCount
	DroppedPackets
	MemoryUsage
	CPUUsage
	ID
	HumanTime
)

func initHeaders() {
	headerSlice[IntNumber] = header{"#", 5}
	headerSlice[Created] = header{"Created", 8}
	headerSlice[Local] = header{"Local", 15}
	headerSlice[Remote] = header{"Remote", 15}
	headerSlice[PMSH] = header{"PMSH", 4}
	headerSlice[PMSL] = header{"PMSL", 4}
	headerSlice[TTFBH] = header{"TTFBH", 5}
	headerSlice[TTFBL] = header{"TTFBL", 5}
	headerSlice[TX] = header{"TX", 9}
	headerSlice[TXCount] = header{"#TX", 6}
	headerSlice[ErrCount] = header{"#ERR", 6}
	headerSlice[DroppedPackets] = header{"#Dropped", 9}
	headerSlice[MemoryUsage] = header{"MemUsed", 7}
	headerSlice[CPUUsage] = header{"CPUUsed", 7}
	headerSlice[ID] = header{"ID", 30}
	headerSlice[HumanTime] = header{"Time", 30}
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
	LatencyHeaders       = []HeaderField{Created, Local, Remote, PMSH, PMSL, TXCount, ErrCount, DroppedPackets, MemoryUsage, CPUUsage}
	HTTPHeaders          = []HeaderField{Created, Local, Remote, PMSH, PMSL, TTFBH, TTFBL, TX, TXCount, ErrCount, DroppedPackets, MemoryUsage, CPUUsage}
	FullDataPointHeaders = []HeaderField{Created, Local, Remote, PMSH, PMSL, TTFBH, TTFBL, TX, TXCount, ErrCount, DroppedPackets, MemoryUsage, CPUUsage}
)

var (
	HeaderStyle  = lipgloss.NewStyle().Background(lipgloss.Color("#F2F2F2")).Foreground(lipgloss.Color("#000000"))
	BaseStyle    = lipgloss.NewStyle().Background(lipgloss.Color("#000000")).Foreground(lipgloss.Color("#F2F2F2"))
	SuccessStyle = lipgloss.NewStyle().Background(lipgloss.Color("#009900")).Foreground(lipgloss.Color("#F2F2F2"))
	WarningStyle = lipgloss.NewStyle().Background(lipgloss.Color("#999900")).Foreground(lipgloss.Color("#F2F2F2"))
	ErrorStyle   = lipgloss.NewStyle().Background(lipgloss.Color("#AA0000")).Foreground(lipgloss.Color("#FFFFFF"))
)

func printHeader(fields []HeaderField) {
	if headerSlice[0].width == 0 {
		initHeaders()
	}
	fs := GenerateFormatString(len(fields))
	hs := make([]interface{}, 0)
	for i := range fields {
		h := headerSlice[fields[i]]
		hs = append(hs, h.width, h.label)
	}

	fmt.Println(HeaderStyle.Render(fmt.Sprintf(fs, hs...)))
}

func PrintColumns(style lipgloss.Style, columns ...column) {
	fs := GenerateFormatString(len(columns))
	hs := make([]interface{}, 0)
	for i := range columns {
		hs = append(hs, columns[i].width, columns[i].value)
	}
	fmt.Println(style.Render(
		fmt.Sprintf(fs, hs...),
	))
}

func printDataPointHeaders(t shared.TestType) {
	switch t {
	case shared.BandwidthTest:
		printHeader(BandwidthHeaders)
	case shared.LatencyTest:
		printHeader(LatencyHeaders)
	case shared.HTTPTest:
		printHeader(HTTPHeaders)
	default:
		printHeader(FullDataPointHeaders)
	}
}

func printTableRow(style lipgloss.Style, entry *shared.DP, t shared.TestType) {
	switch t {
	case shared.LatencyTest:
		PrintColumns(
			style,
			column{entry.Created.Format("15:04:05"), headerSlice[Created].width},
			column{strings.Split(entry.Local, ":")[0], headerSlice[Local].width},
			column{strings.Split(entry.Remote, ":")[0], headerSlice[Remote].width},
			column{formatInt(entry.PMSH), headerSlice[PMSH].width},
			column{formatInt(entry.PMSL), headerSlice[PMSL].width},
			column{formatUint(entry.TXCount), headerSlice[TXCount].width},
			column{formatInt(int64(entry.ErrCount)), headerSlice[ErrCount].width},
			column{formatInt(int64(entry.DroppedPackets)), headerSlice[DroppedPackets].width},
			column{formatInt(int64(entry.MemoryUsedPercent)), headerSlice[MemoryUsage].width},
			column{formatInt(int64(entry.CPUUsedPercent)), headerSlice[MemoryUsage].width},
		)
		return
	case shared.BandwidthTest:
		PrintColumns(
			style,
			column{entry.Created.Format("15:04:05"), headerSlice[Created].width},
			column{strings.Split(entry.Local, ":")[0], headerSlice[Local].width},
			column{strings.Split(entry.Remote, ":")[0], headerSlice[Remote].width},
			column{shared.BandwidthBytesToString(entry.TX), headerSlice[TX].width},
			column{formatInt(int64(entry.ErrCount)), headerSlice[ErrCount].width},
			column{formatInt(int64(entry.DroppedPackets)), headerSlice[DroppedPackets].width},
			column{formatInt(int64(entry.MemoryUsedPercent)), headerSlice[MemoryUsage].width},
			column{formatInt(int64(entry.CPUUsedPercent)), headerSlice[CPUUsage].width},
		)
		return
	case shared.HTTPTest:
		PrintColumns(
			style,
			column{entry.Created.Format("15:04:05"), headerSlice[Created].width},
			column{strings.Split(entry.Local, ":")[0], headerSlice[Local].width},
			column{strings.Split(entry.Remote, ":")[0], headerSlice[Remote].width},
			column{formatInt(entry.PMSH), headerSlice[PMSH].width},
			column{formatInt(entry.PMSL), headerSlice[PMSL].width},
			column{formatInt(entry.TTFBH), headerSlice[TTFBH].width},
			column{formatInt(entry.TTFBL), headerSlice[TTFBH].width},
			column{shared.BandwidthBytesToString(entry.TX), headerSlice[TX].width},
			column{formatUint(entry.TXCount), headerSlice[TXCount].width},
			column{formatInt(int64(entry.ErrCount)), headerSlice[ErrCount].width},
			column{formatInt(int64(entry.DroppedPackets)), headerSlice[DroppedPackets].width},
			column{formatInt(int64(entry.MemoryUsedPercent)), headerSlice[MemoryUsage].width},
			column{formatInt(int64(entry.CPUUsedPercent)), headerSlice[CPUUsage].width},
		)
	default:
		shared.DEBUG("Unknown test type, not printing table")
	}
}

func praseDataPoint(r *shared.DataReponseToClient, c *shared.Config) {
	if r == nil {
		return
	}

	responseLock.Lock()
	defer responseLock.Unlock()

	s1 := lipgloss.NewStyle()

	// This guarantees we are always printing the
	// same header types as the data point types.
	if len(r.DPS) > 0 {
		c.TestType = r.DPS[0].Type
	}
	if len(responseDPS) > 0 {
		if len(responseDPS)%10 == 0 {
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
		sp1 := strings.Split(entry.Local, ":")
		sp2 := strings.Split(sp1[0], ".")
		s1 = s1.Background(lipgloss.Color(getHex(sp2[len(sp2)-1])))
		printTableRow(s1, &entry, entry.Type)
	}

	for i := range r.Errors {
		PrintTError(r.Errors[i])
	}

	responseDPS = append(responseDPS, r.DPS...)
	responseERR = append(responseERR, r.Errors...)
}

// Helper functions to format int/uint values for table display
func formatInt(val int64) string {
	return strconv.FormatInt(val, 10)
}

func formatUint(val uint64) string {
	return strconv.FormatUint(val, 10)
}

var baseStyle = lipgloss.NewStyle().
	BorderStyle(lipgloss.NormalBorder()).
	BorderForeground(lipgloss.Color("240"))

func getHex(firstOcted string) (hex string) {
	of, err := strconv.ParseFloat(firstOcted, 64)
	if err != nil {
		return COLORS[0]
	}
	index := (of / 25) * 10
	if index > 10 {
		index = 10
	} else if index < 0 {
		index = 0
	}
	return COLORS[int(index)]
}

var COLORS = []string{
	"#00000E",
	"#00001E",
	"#00002E",
	"#00003E",
	"#00004E",
	"#00005E",
	"#00006E",
	"#00007E",
	"#00008E",
	"#00009E",
	"#0000AE",
}