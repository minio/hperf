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
	"github.com/minio/hperf/server"
	"github.com/minio/hperf/shared"
)

var (
	bindFlag = cli.StringFlag{
		Name:   "bind",
		EnvVar: "HPERF_BIND",
		Value:  "0.0.0.0:9010",
		Usage:  "Hperf will bind to the specified address",
	}

	storagePathFlag = cli.StringFlag{
		Name:   "storagePath",
		EnvVar: "HPERF_STORAGE_PATH",
		Value:  "$pwd",
		Usage:  "All test results will be saved in this directory",
	}

	serverCMD = cli.Command{
		Name:     "server",
		HelpName: "server",
		Prompt:   "hperf",
		Usage:    "Run hperf server, you can interact with this server using the client",
		Action:   runServer,
		Flags:    []cli.Flag{bindFlag, storagePathFlag, debugFlag},
		CustomHelpTemplate: `
	NAME: {{.HelpName}} - {{.Usage}}

	FLAGS:
		{{range .VisibleFlags}}{{.}}		
		{{end}}
	EXAMPLES:
	
		01. Run HPerf server with defaults
		  
		    {{.Prompt}} {{.HelpName}}

		02. Run HPerf server with custom file path

		    {{.Prompt}} {{.HelpName}} --storagePath /path/on/disk

		03. Run HPerf server with custom file path and custom address

		    {{.Prompt}} {{.HelpName}} --storagePath /path/on/disk --bind 0.0.0.0:9000

`,
	}
)

func runServer(ctx *cli.Context) error {
	shared.DebugEnabled = debug
	return server.RunServer(GlobalContext, ctx.String("bind"), ctx.String("storagePath"))
}
