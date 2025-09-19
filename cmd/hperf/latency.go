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
	"fmt"

	"github.com/minio/cli"
	"github.com/minio/hperf/client"
	"github.com/minio/hperf/shared"
)

var latency = cli.Command{
	Name:   "latency",
	Usage:  "Start a latency test and analyze the results",
	Action: runLatency,
	Flags: []cli.Flag{
		hostsFlag,
		portFlag,
		durationFlag,
		testIDFlag,
		saveTestFlag,
		dnsServerFlag,
		microSecondsFlag,
		printAllFlag,
	},
	CustomHelpTemplate: `NAME:
  {{.HelpName}} - {{.Usage}}

USAGE:
  {{.HelpName}} [FLAGS]

FLAGS:
  {{range .VisibleFlags}}{{.}}
  {{end}}
EXAMPLES:
  1. Run a 30 second latency test and show all data points after the test finishes:
   {{.Prompt}} {{.HelpName}} --duration 30 --hosts 10.10.10.1,10.10.10.2 --print-all

  2. Run a 30 second latency test with custom id:
   {{.Prompt}} {{.HelpName}} --duration 60 --hosts 10.10.10.1,10.10.10.2 --id latency-60
`,
}

func runLatency(ctx *cli.Context) error {
	config, err := parseConfig(ctx)
	if err != nil {
		return err
	}
	config.TestType = shared.RequestTest
	config.BufferSize = 1000
	config.PayloadSize = 1000
	config.Concurrency = 1
	config.RequestDelay = 200
	config.RestartOnError = true

	fmt.Println("")
	shared.INFO(" Test ID:", config.TestID)
	fmt.Println("")

	err = client.RunTest(GlobalContext, *config)
	if err != nil {
		return cli.NewExitError(err.Error(), 1)
	}
	fmt.Println("")
	shared.INFO(" Testing finished ..")

	return client.AnalyzeLatencyTest(GlobalContext, *config)
}
