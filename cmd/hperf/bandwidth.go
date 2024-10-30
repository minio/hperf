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

var bandwidthCMD = cli.Command{
	Name:   "bandwidth",
	Usage:  "Start a test to measure bandwidth",
	Action: runBandwidth,
	Flags: []cli.Flag{
		hostsFlag,
		portFlag,
		durationFlag,
		saveTestFlag,
		testIDFlag,
		concurrencyFlag,
		dnsServerFlag,
		microSecondsFlag,
		printAllFlag,
	},
	CustomHelpTemplate: `NAME:
  {{.HelpName}} - {{.Usage}}

USAGE:
  {{.HelpName}} [FLAGS]

NOTES:
	When testing for bandwidth it is recommended to start with concurrency 10 and increase the count as needed. Normally 10 is enough to saturate a 100Gb NIC.

FLAGS:
  {{range .VisibleFlags}}{{.}}
  {{end}}
EXAMPLES:
  1. Run a basic test which prints all data points when finished:
   {{.Prompt}} {{.HelpName}} --hosts 10.10.10.1,10.10.10.2 --print-all

  2. Run a test with custom concurrency:
   {{.Prompt}} {{.HelpName}} --hosts 10.10.10.1,10.10.10.2 --concurrency 10

  3. Run a 30 seconds bandwidth test:
   {{.Prompt}} {{.HelpName}} --hosts 10.10.10.1,10.10.10.2 --duration 30 --id bandwidth-30
`,
}

func runBandwidth(ctx *cli.Context) error {
	config, err := parseConfig(ctx)
	if err != nil {
		return err
	}

	config.TestType = shared.StreamTest
	config.BufferSize = 32000
	config.PayloadSize = 32000
	config.RequestDelay = 0
	config.RestartOnError = true

	fmt.Println("")
	shared.INFO(" Test ID:", config.TestID)
	fmt.Println("")

	err = client.RunTest(GlobalContext, *config)
	if err != nil {
		return err
	}

	fmt.Println("")
	shared.INFO(" Testing finished..")

	return client.AnalyzeBandwidthTest(GlobalContext, *config)
}
