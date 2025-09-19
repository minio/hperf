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
	"github.com/minio/cli"
	"github.com/minio/hperf/client"
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
		printStatsFlag,
		printErrFlag,
		sortFlag,
		microSecondsFlag,
		hostFilterFlag,
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
  2. Analyze test results and print full output:
    {{.Prompt}} {{.HelpName}} --hosts 10.10.10.1 --file latency-test-1 --print-full
  3. Analyze test results with sorted output:
    {{.Prompt}} {{.HelpName}} --hosts 10.10.10.1 --file latency-test-1 --sort RMSH
  4. Analyze test results with sorted output:
    {{.Prompt}} {{.HelpName}} --hosts 10.10.10.1 --file latency-test-1 --sort RMSH --host-filter 10.10.10.1
`,
}

func runAnalyze(ctx *cli.Context) error {
	config, err := parseConfig(ctx)
	if err != nil {
		return cli.NewExitError(err.Error(), 1)
	}
	return client.AnalyzeTest(GlobalContext, *config)
}
