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
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/minio/hperf/shared"
)

func TestGrowHostColumns(t *testing.T) {
	resetHeaders()
	t.Cleanup(resetHeaders)

	if grew := growHostColumns([]shared.DP{{Local: "10.10.1.2", Remote: "10.10.1.3:9010"}}); grew {
		t.Error("IPv4 addresses should fit the default column width")
	}
	if colWidth(Local) != 15 || colWidth(Remote) != 15 {
		t.Errorf("widths changed for IPv4: local=%d remote=%d", colWidth(Local), colWidth(Remote))
	}

	v6 := "2607:6bc0:8107:432:8e91:3aff:fec5:79ee"
	if grew := growHostColumns([]shared.DP{{Local: v6, Remote: "[" + v6 + "]:9010"}}); !grew {
		t.Error("IPv6 addresses should widen the host columns")
	}
	if colWidth(Local) != len(v6) || colWidth(Remote) != len(v6) {
		t.Errorf("widths not grown to %d: local=%d remote=%d", len(v6), colWidth(Local), colWidth(Remote))
	}

	if grew := growHostColumns([]shared.DP{{Local: "10.10.1.2", Remote: "10.10.1.3:9010"}}); grew {
		t.Error("columns should not shrink or report a change for narrower addresses")
	}
}

// TestHostColumnWidthIsRaceFree pins the fix for the width race: the end-of-run
// per-host summary reads these widths outside responseLock while reader
// goroutines are still widening them, which is reachable whenever the run ends
// on its grace timeout. Run with -race.
func TestHostColumnWidthIsRaceFree(t *testing.T) {
	resetHeaders()
	t.Cleanup(resetHeaders)

	var wg sync.WaitGroup
	stop := make(chan struct{})

	// Writers, as ingest does.
	for w := 0; w < 3; w++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for i := 0; ; i++ {
				select {
				case <-stop:
					return
				default:
				}
				host := strings.Repeat("h", 10+(i+id)%40)
				growHostColumns([]shared.DP{{Local: host, Remote: host + ":9010"}})
			}
		}(w)
	}

	// Readers, as the summary and the live table do.
	for r := 0; r < 2; r++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				_ = colWidth(Local)
				_ = colWidth(Remote)
				_ = colWidth(TXA)
			}
		}()
	}

	time.Sleep(300 * time.Millisecond)
	close(stop)
	wg.Wait()

	// Widths only ever grow, so the result must be the widest host seen.
	if got := colWidth(Local); got < 10 {
		t.Errorf("local width = %d, expected it to have grown", got)
	}
}
