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
	"slices"
	"testing"
)

func TestNormalizeHost(t *testing.T) {
	cases := []struct {
		host     string
		expected string
	}{
		{"10.10.1.2", "10.10.1.2"},
		{"[10.10.1.2]", "10.10.1.2"},
		{"2607:6bc0:8107:432::1", "2607:6bc0:8107:432::1"},
		{"[2607:6bc0:8107:432::1]", "2607:6bc0:8107:432::1"},
		{"2607:6BC0:8107:0432:0000:0000:0000:0001", "2607:6bc0:8107:432::1"},
		{"[fe80::1%eth0]", "fe80::1%eth0"},
		{" 10.10.1.2 ", "10.10.1.2"},
		{"node1.example.com", "node1.example.com"},
	}

	for _, c := range cases {
		if got := NormalizeHost(c.host); got != c.expected {
			t.Errorf("NormalizeHost(%q) = %q, expected %q", c.host, got, c.expected)
		}
	}
}

func TestHostOnly(t *testing.T) {
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
		if got := HostOnly(c.addr); got != c.expected {
			t.Errorf("HostOnly(%q) = %q, expected %q", c.addr, got, c.expected)
		}
	}
}

func TestSameHost(t *testing.T) {
	cases := []struct {
		a        string
		b        string
		expected bool
	}{
		// The bug this replaces: a substring match dropped legitimate peers.
		{"10.0.0.10", "10.0.0.1", false},
		{"fd00::10", "fd00::1", false},
		{"10.0.0.1", "10.0.0.1", true},
		// Same address, different spelling.
		{"fd00::1", "fd00:0000:0000:0000:0000:0000:0000:0001", true},
		{"[fd00::1]", "fd00::1", true},
		{"::ffff:10.0.0.1", "10.0.0.1", true},
		// Zones belong to the identity of a link local address.
		{"fe80::1%eth0", "fe80::1%eth1", false},
		{"fe80::1%eth0", "fe80::1%eth0", true},
		// Hostnames.
		{"node1.example.com", "NODE1.example.com", true},
		{"node1.example.com", "node10.example.com", false},
		{"node1.example.com", "10.0.0.1", false},
	}

	for _, c := range cases {
		if got := SameHost(c.a, c.b); got != c.expected {
			t.Errorf("SameHost(%q, %q) = %v, expected %v", c.a, c.b, got, c.expected)
		}
	}
}

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
		// An already bracketed host has to survive, operators copy them from
		// documentation and from Helm values.
		{"[::1]", "9010", "[::1]:9010", "::1"},
		{"[2607:6bc0:8107:432::1]", "9010", "[2607:6bc0:8107:432::1]:9010", "2607:6bc0:8107:432::1"},
		// RFC 6874: the zone delimiter is percent encoded inside a URL.
		{"fe80::1%eth0", "9010", "[fe80::1%25eth0]:9010", "fe80::1%eth0"},
		{"[fe80::1%eth0]", "9010", "[fe80::1%25eth0]:9010", "fe80::1%eth0"},
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

func TestParseHostsNormalizes(t *testing.T) {
	cases := []struct {
		hosts    string
		expected []string
	}{
		{"[2607:6bc0:8107:432::1],[2607:6bc0:8107:432::2]", []string{"2607:6bc0:8107:432::1", "2607:6bc0:8107:432::2"}},
		{"2607:6BC0:8107:0432:0000:0000:0000:0001", []string{"2607:6bc0:8107:432::1"}},
		{"2607:6bc0:8107:432::{1...3}", []string{"2607:6bc0:8107:432::1", "2607:6bc0:8107:432::2", "2607:6bc0:8107:432::3"}},
		{"10.10.1.{2...4}", []string{"10.10.1.2", "10.10.1.3", "10.10.1.4"}},
		{"fe80::1%eth0", []string{"fe80::1%eth0"}},
		{"node1.example.com,node2.example.com", []string{"node1.example.com", "node2.example.com"}},
	}

	for _, c := range cases {
		got, err := ParseHosts(c.hosts, "", IPFamilyAuto)
		if err != nil {
			t.Errorf("ParseHosts(%q) returned an error: %v", c.hosts, err)
			continue
		}
		if !slices.Equal(got, c.expected) {
			t.Errorf("ParseHosts(%q) = %v, expected %v", c.hosts, got, c.expected)
		}
	}
}

func TestParseHostsRejectsUnknownFamily(t *testing.T) {
	if _, err := ParseHosts("10.10.1.2", "", "ipv5"); err == nil {
		t.Error("ParseHosts should reject an unknown ip family")
	}
}

func TestHostFilterDoesNotPrefixMatch(t *testing.T) {
	dps := []DP{
		{Local: "10.0.0.1", Remote: "10.0.0.11:9010"},
		{Local: "10.0.0.11", Remote: "10.0.0.1:9010"},
		{Local: "fd00::1", Remote: "[fd00::10]:9010"},
		{Local: "fd00::10", Remote: "[fd00::1]:9010"},
	}

	if got := len(HostFilter("10.0.0.11", dps)); got != 2 {
		t.Errorf("HostFilter(10.0.0.11) returned %d data points, expected 2", got)
	}
	if got := len(HostFilter("fd00::1", dps)); got != 2 {
		t.Errorf("HostFilter(fd00::1) returned %d data points, expected 2", got)
	}
	// Expanded spelling of the same address still matches.
	if got := len(HostFilter("fd00:0000:0000:0000:0000:0000:0000:0010", dps)); got != 2 {
		t.Errorf("HostFilter(expanded fd00::10) returned %d data points, expected 2", got)
	}
}
