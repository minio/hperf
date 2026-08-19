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

import "testing"

func TestIsSelfHost(t *testing.T) {
	cases := []struct {
		name     string
		bind     string
		real     string
		host     string
		expected bool
	}{
		{"real ip matches", "0.0.0.0:9010", "10.0.0.1", "10.0.0.1", true},
		// The bug this replaces: a substring match dropped these peers and the
		// server silently tested a smaller mesh than it was asked to.
		{"real ip is a prefix of the peer", "0.0.0.0:9010", "10.0.0.1", "10.0.0.10", false},
		{"real ipv6 is a prefix of the peer", "[::]:9010", "fd00::1", "fd00::10", false},
		{"real ipv6 matches", "[::]:9010", "fd00::1", "fd00::1", true},
		{"real ipv6 matches expanded", "[::]:9010", "fd00::1", "fd00:0000:0000:0000:0000:0000:0000:0001", true},
		{"bind address matches", "10.0.0.1:9010", "", "10.0.0.1", true},
		{"bind address is a prefix of the peer", "10.0.0.1:9010", "", "10.0.0.10", false},
		{"bracketed bind address matches", "[fd00::1]:9010", "", "fd00::1", true},
		{"wildcard bind tells us nothing", "0.0.0.0:9010", "", "10.0.0.1", false},
		{"ipv6 wildcard bind tells us nothing", "[::]:9010", "", "fd00::1", false},
		{"hostnames are compared as names", "0.0.0.0:9010", "node1.example.com", "node1.example.com", true},
		{"hostname does not match a longer name", "0.0.0.0:9010", "node1.example.com", "node10.example.com", false},
	}

	origBind, origReal := bindAddress, realIP
	defer func() {
		bindAddress, realIP = origBind, origReal
	}()

	for _, c := range cases {
		bindAddress, realIP = c.bind, c.real
		if got := isSelfHost(c.host); got != c.expected {
			t.Errorf("%s: isSelfHost(%q) with bind %q and real ip %q = %v, expected %v",
				c.name, c.host, c.bind, c.real, got, c.expected)
		}
	}
}
