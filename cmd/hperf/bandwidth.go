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
	"github.com/minio/hperf/shared"
)

var bandwidthCMD = cli.Command{
	Name:   "bandwidth",
	Usage:  "start a test to measure bandwidth, open --concurrency number of sockets, write data upto --duration",
	Action: runBandwidth,
	Flags: []cli.Flag{
		hostsFlag,
		portFlag,
		concurrencyFlag,
		durationFlag,
		testIDFlag,
		bufferSizeFlag,
		payloadSizeFlag,
		restartOnErrorFlag,
		dnsServerFlag,
		saveTestFlag,
	},
	CustomHelpTemplate: `NAME:
  {{.HelpName}} - {{.Usage}}

USAGE:
  {{.HelpName}} [FLAGS]

NOTE:
  Matching concurrency with your thread count can often lead to
  improved performance, it is even better to run concurrency at
  50% of the GOMAXPROCS.

FLAGS:
  {{range .VisibleFlags}}{{.}}
  {{end}}
EXAMPLES:
  1. Run a basic test:
   {{.Prompt}} {{.HelpName}} --hosts 10.10.10.1,10.10.10.2

  2. Run a test with custom concurrency:
   {{.Prompt}} {{.HelpName}} --hosts 10.10.10.1,10.10.10.2 --concurrency 24

  3. Run a test with custom buffer sizes, for MTU specific testing:
   {{.Prompt}} {{.HelpName}} --hosts 10.10.10.1,10.10.10.2 --bufferSize 9000 --payloadSize 9000
`,
}

func runBandwidth(ctx *cli.Context) error {
	config, err := parseConfig(ctx)
	if err != nil {
		return err
	}
	config.TestType = shared.BandwidthTest
	return client.RunTest(GlobalContext, *config)
}
