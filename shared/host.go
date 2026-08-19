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
	"net"
	"net/netip"
	"net/url"
	"strings"
)

// NormalizeHost canonicalizes a single host entry. Brackets around an IPv6
// literal are removed and IP literals are rewritten to their canonical form,
// so that the same address always reaches the wire, the self filters and the
// output in one spelling. Hostnames are returned unchanged apart from
// surrounding whitespace.
func NormalizeHost(host string) string {
	h := strings.TrimSpace(host)
	if len(h) > 1 && h[0] == '[' && h[len(h)-1] == ']' {
		h = h[1 : len(h)-1]
	}
	if addr, err := netip.ParseAddr(h); err == nil {
		return addr.String()
	}
	return h
}

// HostOnly returns the host part of an address, dropping the port and the
// brackets of an IPv6 literal. Values without a port are returned as they are,
// which is why this cannot use net.SplitHostPort alone: an unbracketed IPv6
// literal has more colons than SplitHostPort accepts.
func HostOnly(addr string) string {
	if host, _, err := net.SplitHostPort(addr); err == nil {
		return host
	}
	return strings.Trim(addr, "[]")
}

// SameHost reports whether two host entries point at the same machine. IP
// literals are compared as addresses instead of strings, so 10.0.0.1 does not
// match 10.0.0.10 and 2001:db8::1 does match 2001:0db8:0:0:0:0:0:1. Hostnames
// are compared case insensitively.
func SameHost(a string, b string) bool {
	na, nb := NormalizeHost(a), NormalizeHost(b)
	aa, aerr := netip.ParseAddr(na)
	ba, berr := netip.ParseAddr(nb)
	if aerr == nil || berr == nil {
		if aerr != nil || berr != nil {
			return false
		}
		return aa.Unmap() == ba.Unmap()
	}
	return strings.EqualFold(na, nb)
}

// URLHostPort joins host and port for use inside a URL, percent-encoding the
// zone delimiter of a scoped IPv6 address as RFC 6874 requires.
func URLHostPort(host string, port string) string {
	h := NormalizeHost(host)
	if zone := strings.IndexByte(h, '%'); zone >= 0 {
		h = url.PathEscape(h[:zone]) + "%25" + url.PathEscape(h[zone+1:])
	} else {
		h = url.PathEscape(h)
	}
	return net.JoinHostPort(h, port)
}
