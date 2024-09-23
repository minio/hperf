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
		Usage:  "Hperf will bind to the specified address",
	}

	storagePathFlag = cli.StringFlag{
		Name:   "storage-path",
		EnvVar: "HPERF_STORAGE_PATH",
		Value:  getPWD(),
		Usage:  "All test results will be saved in this directory",
	}

	serverCMD = cli.Command{
		Name:     "server",
		HelpName: "server",
		Prompt:   "hperf",
		Usage:    "Run hperf server, you can interact with this server using the client",
		Action:   runServer,
		Flags:    []cli.Flag{addressFlag, storagePathFlag, debugFlag},
		CustomHelpTemplate: `
	NAME: {{.HelpName}} - {{.Usage}}

	FLAGS:
		{{range .VisibleFlags}}{{.}}		
		{{end}}
	EXAMPLES:
	
		01. Run HPerf server with defaults
		  
		    {{.Prompt}} {{.HelpName}}

		02. Run HPerf server with custom file path

		    {{.Prompt}} {{.HelpName}} --storage-path /path/on/disk

		03. Run HPerf server with custom file path and custom address

		    {{.Prompt}} {{.HelpName}} --storage-path /path/on/disk --address 0.0.0.0:9000

`,
	}
)

func runServer(ctx *cli.Context) error {
	shared.DebugEnabled = debug
	return server.RunServer(GlobalContext, ctx.String("address"), ctx.String("storage-path"))
}
