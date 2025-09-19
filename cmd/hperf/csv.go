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

var csvCMD = cli.Command{
	Name:   "csv",
	Usage:  "Transform a test file to csv file",
	Action: runCSV,
	Flags: []cli.Flag{
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
  1. Transform a test file to csv file:
    {{.Prompt}} {{.HelpName}} --file /tmp/output-file
`,
}

func runCSV(ctx *cli.Context) error {
	config, err := parseConfig(ctx)
	if err != nil {
		return cli.NewExitError(err.Error(), 1)
	}

	return client.MakeCSV(GlobalContext, *config)
}
