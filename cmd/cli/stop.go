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

var stopCMD = cli.Command{
	Name:     "stop",
	HelpName: "stop",
	Prompt:   "hperf",
	Usage:    "Stop a specific test or all test on the selected hosts",
	Action:   runStopCMD,
	Flags: []cli.Flag{
		dnsServerFlag,
		hostsFlag,
		portFlag,
		testIDFlag,
	},
	CustomHelpTemplate: `
	NAME: {{.HelpName}} - {{.Usage}}

	FLAGS:
		{{range .VisibleFlags}}{{.}}
		{{end}}
	EXAMPLES:

		01. Stop all tests on hosts .1 and .2

	      {{.Prompt}} {{.HelpName}} --hosts 10.10.10.1,10.10.10.2

		02. Stop test by ID on hosts .1 and .2

        {{.Prompt}} {{.HelpName}} --hosts 10.10.10.1,10.10.10.2 --id my_test_id

`,
}

func runStopCMD(ctx *cli.Context) error {
	config, err := parseConfig(ctx)
	if err != nil {
		return err
	}
	return client.Stop(GlobalContext, *config)
}
