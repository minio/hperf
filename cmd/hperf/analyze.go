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

package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"slices"

	"github.com/charmbracelet/lipgloss"
	"github.com/minio/cli"
	"github.com/minio/hperf/client"
	"github.com/minio/hperf/shared"
)

var analyzeCMD = cli.Command{
	Name:   "analyze",
	Usage:  "Analyze the give test",
	Action: runAnalyze,
	Flags: []cli.Flag{
		dnsServerFlag,
		hostsFlag,
		portFlag,
		fileFlag,
	},
	CustomHelpTemplate: `NAME:
  {{.HelpName}} - {{.Usage}}

USAGE:
  {{.HelpName}} [FLAGS]

FLAGS:
  {{range .VisibleFlags}}{{.}}
  {{end}}
EXAMPLES:
  1. Analyze test results in file '/tmp/latency-test-1':
    {{.Prompt}} {{.HelpName}} --hosts 10.10.10.1 --file latency-test-1
`,
}

func runAnalyze(ctx *cli.Context) error {
	config, err := parseConfig(ctx)
	if err != nil {
		return err
	}
	return AnalyzeTest(GlobalContext, *config)
}

func AnalyzeTest(ctx context.Context, c shared.Config) (err error) {
	_, cancel := context.WithCancel(ctx)
	defer cancel()

	f, err := os.Open(c.File)
	if err != nil {
		return err
	}

	dps := make([]shared.DP, 0)
	errors := make([]shared.TError, 0)

	s := bufio.NewScanner(f)
	for s.Scan() {
		b := s.Bytes()
		if !bytes.Contains(b, []byte("Error")) {
			dp := new(shared.DP)
			err := json.Unmarshal(b, dp)
			if err != nil {
				return err
			}
			dps = append(dps, *dp)
		} else {
			dperr := new(shared.TError)
			err := json.Unmarshal(b, dperr)
			if err != nil {
				return err
			}
			errors = append(errors, *dperr)
		}
	}

	// adjust stats
	for i := range dps {
		// Highest RMSH can never be 0, but it's the default value of golang int64.
		// if we find a 0 we just set it to an impossibly high value.
		if dps[i].RMSH == 0 {
			dps[i].RMSH = 999999999
		}
	}

	dps10 := math.Ceil((float64(len(dps)) / 100) * 10)
	dps90 := math.Floor((float64(len(dps)) / 100) * 90)

	slices.SortFunc(dps, func(a shared.DP, b shared.DP) int {
		if a.RMSH < b.RMSH {
			return -1
		} else {
			return 1
		}
	})

	dps10s := make([]shared.DP, 0)
	dps50s := make([]shared.DP, 0)
	dps90s := make([]shared.DP, 0)

	// total, sum, low, mean, high
	dps10stats := []int64{0, 0, 999999999, 0, 0}
	dps50stats := []int64{0, 0, 999999999, 0, 0}
	dps90stats := []int64{0, 0, 999999999, 0, 0}

	for i := range dps {
		if i <= int(dps10) {
			dps10s = append(dps10s, dps[i])
			updateBracketStats(dps10stats, dps[i])
		} else if i >= int(dps90) {
			dps90s = append(dps90s, dps[i])
			updateBracketStats(dps90stats, dps[i])
		} else {
			dps50s = append(dps50s, dps[i])
			updateBracketStats(dps50stats, dps[i])
		}
	}

	printBracker(dps10stats, "? < 10%", client.SuccessStyle)
	printBracker(dps50stats, "10% < ? < 90%", client.WarningStyle)
	printBracker(dps90stats, "? > 90%", client.ErrorStyle)

	return nil
}

func printBracker(b []int64, tag string, style lipgloss.Style) {
	fmt.Println(style.Render(
		fmt.Sprintf(" %s | Total %d | Low %d | Avg %d | High %d | Microseconds ",
			tag,
			b[0],
			b[2],
			b[3],
			b[4],
		),
	))
}

func updateBracketStats(b []int64, dp shared.DP) {
	b[0]++
	b[1] += dp.RMSH
	if dp.RMSH < b[2] {
		b[2] = dp.RMSH
	}
	b[3] = b[1] / b[0]
	if dp.RMSH > b[4] {
		b[4] = dp.RMSH
	}
}
