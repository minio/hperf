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
	"testing"

	"github.com/minio/hperf/shared"
)

func TestHostColumnValue(t *testing.T) {
	cases := []struct {
		addr     string
		expected string
	}{
		{"10.10.1.2", "10.10.1.2"},
		{"10.10.1.2:9010", "10.10.1.2"},
		{"2607:6bc0:8107:432::1", "2607:6bc0:8107:432::1"},
		{"[2607:6bc0:8107:432::1]:9010", "2607:6bc0:8107:432::1"},
		{"[fe80::1%eth0]:9010", "fe80::1%eth0"},
		{"node1.example.com:9010", "node1.example.com"},
	}

	for _, c := range cases {
		if got := hostColumnValue(c.addr); got != c.expected {
			t.Errorf("hostColumnValue(%q) = %q, expected %q", c.addr, got, c.expected)
		}
	}
}

func TestGrowHostColumns(t *testing.T) {
	initHeaders()

	if grew := growHostColumns([]shared.DP{{Local: "10.10.1.2", Remote: "10.10.1.3:9010"}}); grew {
		t.Error("IPv4 addresses should fit the default column width")
	}
	if headerSlice[Local].width != 15 || headerSlice[Remote].width != 15 {
		t.Errorf("widths changed for IPv4: local=%d remote=%d", headerSlice[Local].width, headerSlice[Remote].width)
	}

	v6 := "2607:6bc0:8107:432:8e91:3aff:fec5:79ee"
	if grew := growHostColumns([]shared.DP{{Local: v6, Remote: "[" + v6 + "]:9010"}}); !grew {
		t.Error("IPv6 addresses should widen the host columns")
	}
	if headerSlice[Local].width != len(v6) || headerSlice[Remote].width != len(v6) {
		t.Errorf("widths not grown to %d: local=%d remote=%d", len(v6), headerSlice[Local].width, headerSlice[Remote].width)
	}

	if grew := growHostColumns([]shared.DP{{Local: "10.10.1.2", Remote: "10.10.1.3:9010"}}); grew {
		t.Error("columns should not shrink or report a change for narrower addresses")
	}
}
