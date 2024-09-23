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

var listTestsCMD = cli.Command{
	Name:     "list",
	HelpName: "list",
	Prompt:   "hperf",
	Usage:    "List all tests on the selected hosts",
	Action:   runListCMD,
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

		01. List all test on the server

	      {{.Prompt}} {{.HelpName}} --hosts 1.1.1.1

`,
}

func runListCMD(ctx *cli.Context) error {
	config, err := parseConfig(ctx)
	if err != nil {
		return err
	}
	return client.ListTests(GlobalContext, *config)
}
