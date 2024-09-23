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
	Name:     "bandwidth",
	HelpName: "bandwidth",
	Prompt:   "hperf",
	Usage:    "A test which focuses on measuring bandwidth, It will open up a single socket(x --concurrency) and write as much data as possible for the configured --duration",
	Action:   runBandwidthCMD,
	Flags: []cli.Flag{
		dnsServerFlag,
		hostsFlag,
		portFlag,
		testIDFlag,
		insecureFlag,
		concurrencyFlag,
		durationFlag,
		bufferSizeFlag,
		payloadSizeFlag,
		restartOnErrorFlag,
		saveTestFlag,
	},
	CustomHelpTemplate: `
	NAME: {{.HelpName}} 

	{{.Usage}}

	FLAGS:
		{{range .VisibleFlags}}{{.}}
		{{end}}
	EXAMPLES:

		01. Run a basic test

        {{.Prompt}} {{.HelpName}} --hosts 10.10.10.1,10.10.10.2

		02. Run a test with custom concurrency 
	      - Matching concurrency with your thread count can often lead to improved performance 
				- Sometimes it's even better to run concurrency at thread_count/2

        {{.Prompt}} {{.HelpName}} --hosts 10.10.10.1,10.10.10.2 --concurrency 24

		03. Run a test with custom buffer sizes 
	      - This can be handy when testing MTU and other parameters for optimizations

        {{.Prompt}} {{.HelpName}} --hosts 10.10.10.1,10.10.10.2 --bufferSize 8192 --payloadSize 8192

`,
}

func runBandwidthCMD(ctx *cli.Context) error {
	config, err := parseConfig(ctx)
	if err != nil {
		return err
	}
	config.TestType = shared.BandwidthTest
	return client.RunTest(GlobalContext, *config)
}
