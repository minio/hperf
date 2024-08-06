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

var listenCMD = cli.Command{
	Name:     "listen",
	HelpName: "listen",
	Prompt:   "hperf",
	Usage:    "Receive live data from one or all active tests on the selected hosts",
	Action:   runListenCMD,
	Flags: []cli.Flag{
		dnsServerFlag,
		hostsFlag,
		portFlag,
		testIDFlag,
	},
	CustomHelpTemplate: `
	NAME: {{.HelpName}}

	{{.Usage}}

	FLAGS:
		{{range .VisibleFlags}}{{.}}
		{{end}}
	EXAMPLES:

		01. Listen to a specific test

      {{.Prompt}} {{.HelpName}} --hosts 10.10.10.1,10.10.10.2 --id my_test_id

		02. Listen to all active tests

      {{.Prompt}} {{.HelpName}} --hosts 10.10.10.1,10.10.10.2

`,
}

func runListenCMD(ctx *cli.Context) error {
	config, err := parseConfig(ctx)
	if err != nil {
		return err
	}
	config.Duration = 0
	return client.Listen(GlobalContext, *config)
}
