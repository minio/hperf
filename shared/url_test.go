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
	"net/url"
	"testing"
)

func TestURLHostPort(t *testing.T) {
	cases := []struct {
		host     string
		port     string
		expected string
		hostname string
	}{
		{"10.10.1.2", "9010", "10.10.1.2:9010", "10.10.1.2"},
		{"node1.example.com", "9010", "node1.example.com:9010", "node1.example.com"},
		{"2607:6bc0:8107:432::1", "9010", "[2607:6bc0:8107:432::1]:9010", "2607:6bc0:8107:432::1"},
		{"::1", "9010", "[::1]:9010", "::1"},
		{"fe80::1%eth0", "9010", "[fe80::1%25eth0]:9010", "fe80::1%eth0"},
	}

	for _, c := range cases {
		got := URLHostPort(c.host, c.port)
		if got != c.expected {
			t.Errorf("URLHostPort(%q, %q) = %q, expected %q", c.host, c.port, got, c.expected)
		}

		u, err := url.Parse("http://" + got + "/stream")
		if err != nil {
			t.Errorf("url.Parse of %q failed: %v", got, err)
			continue
		}
		if u.Hostname() != c.hostname {
			t.Errorf("url.Parse(%q).Hostname() = %q, expected %q", got, u.Hostname(), c.hostname)
		}
	}
}
