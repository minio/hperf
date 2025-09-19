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
	"os"

	"github.com/minio/cli"
	"github.com/minio/hperf/server"
	"github.com/minio/hperf/shared"
)

func getPWD() string {
	pwd, _ := os.Getwd()
	return pwd
}

var (
	addressFlag = cli.StringFlag{
		Name:   "address",
		EnvVar: "HPERF_ADDRESS",
		Value:  "0.0.0.0:9010",
		Usage:  "bind to the specified address",
	}
	realIPFlag = cli.StringFlag{
		Name:   "real-ip",
		EnvVar: "HPERF_REAL_IP",
		Value:  "",
		Usage:  "The real IP used to connect to other servers. If the --address is bound to the real IP then this flag can be skipped.",
	}
	storagePathFlag = cli.StringFlag{
		Name:   "storage-path",
		EnvVar: "HPERF_STORAGE_PATH",
		Value:  getPWD(),
		Usage:  "all test results will be saved in this directory",
	}

	serverCMD = cli.Command{
		Name:   "server",
		Usage:  "start an interactive server",
		Action: runServer,
		Flags:  []cli.Flag{addressFlag, realIPFlag, storagePathFlag, debugFlag},
		CustomHelpTemplate: `NAME:
  {{.HelpName}} - {{.Usage}}

USAGE:
  {{.HelpName}} [FLAGS]

FLAGS:
  {{range .VisibleFlags}}{{.}}
  {{end}}
EXAMPLES:
  1. Run HPerf server with defaults:
    {{.Prompt}} {{.HelpName}}

  2. Run HPerf server with custom file path
    {{.Prompt}} {{.HelpName}} --storage-path /path/on/disk

  3. Run HPerf server with custom file path and custom address
    {{.Prompt}} {{.HelpName}} --storage-path /path/on/disk --address 0.0.0.0:9000

  4. Run HPerf server with custom file path and floating(real) ip
    {{.Prompt}} {{.HelpName}} --storage-path /path/on/disk --address 0.0.0.0:9000 --real-ip 152.121.12.4
`,
	}
)

func runServer(ctx *cli.Context) error {
	shared.DebugEnabled = debug
	err := server.RunServer(
		GlobalContext,
		ctx.String("address"),
		ctx.String("real-ip"),
		ctx.String("storage-path"),
	)
	if err != nil {
		return cli.NewExitError(err.Error(), 1)
	}
	return nil
}
