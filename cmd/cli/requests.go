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

var requestsCMD = cli.Command{
	Name:     "requests",
	HelpName: "requests",
	Prompt:   "hperf",
	Usage:    "A test to measure http/s throughput at the application level, it will send 1*Request*(--concurrency) containing (--payload) waiting for (--delay) between requests, until the end of (--duration)",
	Action:   runRequestsCMD,
	Flags: []cli.Flag{
		hostsFlag,
		portFlag,
		insecureFlag,
		concurrencyFlag,
		delayFlag,
		durationFlag,
		bufferSizeFlag,
		payloadSizeFlag,
		restartOnErrorFlag,
		testIDFlag,
		saveTestFlag,
		dnsServerFlag,
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

		02. Run a test with reduced throughput

	      {{.Prompt}} {{.HelpName}} --hosts 10.10.10.1,10.10.10.2 --delay 100

		03. Run a test with reduced concurrency and throughput

	      {{.Prompt}} {{.HelpName}} --hosts 10.10.10.1,10.10.10.2 --delay 100 --concurrency 1

`,
}

func runRequestsCMD(ctx *cli.Context) error {
	config, err := parseConfig(ctx)
	if err != nil {
		return err
	}
	config.TestType = shared.HTTPTest
	return client.RunTest(GlobalContext, *config)
}
