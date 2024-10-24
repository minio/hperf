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

package shared

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/minio/pkg/v3/ellipses"
)

var DebugEnabled = false

type WebsocketSignal struct {
	SType SignalType
	Code  SignalCode
	Error string

	// Type specific fields
	Data      []byte
	Config    *Config
	DataPoint *DataReponseToClient
	TestList  []TestInfo
}

type TestInfo struct {
	ID   string
	Time time.Time
}

type TestOutput struct {
	ErrCount int
	TXC      uint64
	TXL      uint64
	TXH      uint64
	TXT      uint64
	RMSL     int64
	RMSH     int64
	TTFBL    int64
	TTFBH    int64
	DP       int
	ML       int
	MH       int
	CL       int
	CH       int
}

type (
	SignalType int
	SignalCode int
	TestType   int
	FilePrefix byte
)

const (
	DataPoint FilePrefix = iota
	ErrorPoint
)

func (f FilePrefix) String() []byte {
	return []byte(strconv.Itoa(int(f)))
}

const (
	Err SignalType = iota
	RunTest
	ListenTest
	ListTests
	GetTest
	DeleteTests
	Ping
	Pong
	Exit
	StopAllTests
	Stats
	Done
)

const (
	Unknown TestType = iota
	LatencyTest
	BandwidthTest
	// HTTPTest
)

const (
	OK SignalCode = iota
	Fail
	Retry
)

type TError struct {
	Error   string
	Created time.Time
}

type DP struct {
	Type              TestType
	TestID            string
	Created           time.Time
	Local             string
	Remote            string
	RMSH              int64
	RMSL              int64
	TTFBH             int64
	TTFBL             int64
	TX                uint64
	TXTotal           uint64
	TXCount           uint64
	ErrCount          int
	DroppedPackets    int
	MemoryUsedPercent int
	CPUUsedPercent    int

	// Client only
	Received time.Time `json:"-"`
}

type DataReponseToClient struct {
	DPS    []DP
	Errors []TError
}

type Config struct {
	Debug          bool          `json:"Debug"`
	Port           string        `json:"Port"`
	Proc           int           `json:"Proc"`
	Concurrency    int           `json:"Concurrency"`
	PayloadSize    int           `json:"PayloadMB"`
	BufferKB       int           `json:"BufferKB"`
	Duration       int           `json:"Duration"`
	RequestDelay   int           `json:"RequestDelay"`
	Hosts          []string      `json:"Hosts"`
	RestartOnError bool          `json:"RestartOnError"`
	DialTimeout    time.Duration `json:"DialTimeout"`
	TestID         string        `json:"TestID"`
	Save           bool          `json:"Save"`
	Insecure       bool          `json:"Insecure"`
	TestType       TestType      `json:"TestType"`
	File           string        `json:"File"`
	// AllowLocalInterface bool          `json:"AllowLocalInterfaces"`

	// Client Only
	ResolveHosts string   `json:"-"`
	PrintFull    bool     `json:"-"`
	PrintErrors  bool     `json:"-"`
	Sort         SortType `json:"-"`
	Micro        bool     `json:"-"`
	HostFilter   string   `json:"-"`
}

func INFO(items ...any) {
	fmt.Println(items...)
}

func DEBUG(items ...any) {
	if DebugEnabled {
		fmt.Println(items...)
	}
}

func BToString(b uint64) string {
	if b <= 999 {
		intS := strconv.FormatUint(b, 10)
		return intS + " B"
	} else if b <= 999_999 {
		intF := float64(b)
		return fmt.Sprintf("%.2f KB", intF/1000)
	} else if b <= 999_999_999 {
		intF := float64(b)
		return fmt.Sprintf("%.2f MB", intF/1_000_000)
	} else if b <= 999_999_999_999 {
		intF := float64(b)
		return fmt.Sprintf("%.2f GB", intF/1_000_000_000)
	} else if b <= 999_999_999_999_999 {
		intF := float64(b)
		return fmt.Sprintf("%.2f TB", intF/1_000_000_000_000)
	}

	return "???"
}

func BWToString(b uint64) string {
	if b <= 999 {
		intS := strconv.FormatUint(b, 10)
		return intS + " B/s"
	} else if b <= 999_999 {
		intF := float64(b)
		return fmt.Sprintf("%.2f KB/s", intF/1000)
	} else if b <= 999_999_999 {
		intF := float64(b)
		return fmt.Sprintf("%.2f MB/s", intF/1_000_000)
	} else if b <= 999_999_999_999 {
		intF := float64(b)
		return fmt.Sprintf("%.2f GB/s", intF/1_000_000_000)
	} else if b <= 999_999_999_999_999 {
		intF := float64(b)
		return fmt.Sprintf("%.2f TB/s", intF/1_000_000_000_000)
	}

	return "???"
}

func ParseHosts(hosts string, dnsServer string) (list []string, err error) {
	list = make([]string, 0)

	if dnsServer != "" {
		DEBUG("Using DNS server: ", dnsServer)
	}
	if strings.Contains(hosts, "file:") {
		DEBUG("Parsing hosts from file: ", hosts)

		fs := strings.Split(hosts, ":")
		if len(fs) < 2 {
			err = errors.New("When using a file for hosts, please use the format( file:path ) example( file:~/hosts.txt )")
			return
		}

		var hb []byte
		hb, err = os.ReadFile(fs[1])
		if err != nil {
			err = errors.New("Could not open file:" + fs[1])
			return
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
			err = errors.New("Hosts within the file ( " + fs[1] + " ) should be per line or comma seperated")
			return
		}

		for _, v := range splitLines {
			// to account to accidental empty lines or commas
			if len(v) == 0 {
				continue
			}
			list = append(list, string(v))
		}

	} else {

		splitHosts := strings.Split(hosts, ",")
		hostList := make([]ellipses.ArgPattern, 0)
		for _, v := range splitHosts {
			if !ellipses.HasEllipses(v) {
				list = append(list, v)
				continue
			}

			x, e := ellipses.FindEllipsesPatterns(v)
			if e != nil {
				err = e
				return
			}
			hostList = append(hostList, x)
		}

		for _, host := range hostList {
			for _, pattern := range host {
				for _, seq := range pattern.Seq {
					list = append(list, pattern.Prefix+seq)
				}
			}
		}

	}

	for i, host := range list {
		if net.ParseIP(host) == nil && dnsServer != "" {
			var ips []net.IP
			ips, err = net.LookupIP(host)
			if err != nil {
				return
			}
			if len(ips) == 0 {
				err = errors.New("Could not look up " + host + ", err: did not find any IPs on record")
				return
			}

			list[i] = ips[0].String()
			continue
		}
	}

	DEBUG("Final host list")
	DEBUG(list)

	return
}

func GetInterfaceAddresses() (list []string, err error) {
	list = make([]string, 0)

	interfaces, err := net.Interfaces()
	if err != nil {
		return nil, err
	}
	for _, intf := range interfaces {
		addrs, err := intf.Addrs()
		if err != nil {
			return nil, err
		}
		for _, addr := range addrs {
			sa := strings.Split(addr.String(), "/")
			list = append(list, sa[0])
		}
	}

	return
}

func WriteStructAndNewLineToFile(f *os.File, prefix FilePrefix, s interface{}) (int, error) {
	outb, err := json.Marshal(s)
	if err != nil {
		return 0, err
	}
	n, err := f.Write(prefix.String())
	if err != nil {
		return n, err
	}
	n, err = f.Write(outb)
	if err != nil {
		return n, err
	}
	n, err = f.Write([]byte{10})
	return n, err
}
