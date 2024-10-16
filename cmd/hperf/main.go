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
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"runtime"
	"strconv"
	"syscall"
	"time"

	"github.com/minio/cli"
	"github.com/minio/hperf/client"
	"github.com/minio/hperf/shared"
)

var version = "0.0.0-dev"

// Help template for mc
var mcHelpTemplate = `NAME:
  {{.Name}} - {{.Usage}}

USAGE:
  {{.Name}} {{if .VisibleFlags}}[FLAGS] {{end}}COMMAND{{if .VisibleFlags}} [COMMAND FLAGS | -h]{{end}} [ARGUMENTS...]

COMMANDS:
  {{range .VisibleCommands}}{{join .Names ", "}}{{ "\t" }}{{.Usage}}
  {{end}}{{if .VisibleFlags}}
GLOBAL FLAGS:
  {{range .VisibleFlags}}{{.}}
  {{end}}{{end}}
`

func main() {
	CreateApp().Run(os.Args)
}

func InvalidFlagValueError(value interface{}, name string) error {
	return fmt.Errorf("Invalid flag value (%s) for flag (%s)", value, name)
}

var (
	debug       = false
	insecure    = false
	globalFlags = []cli.Flag{
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
	}
	hostsFlag = cli.StringFlag{
		Name:   "hosts",
		EnvVar: "HPERF_HOSTS",
		Usage:  "list of hosts for the current command",
	}
	portFlag = cli.StringFlag{
		Name:   "port",
		Value:  "9010",
		EnvVar: "HPERF_PORT",
		Usage:  "port used to communicate with hosts",
	}
	insecureFlag = cli.BoolTFlag{
		Name:   "insecure",
		EnvVar: "HPERF_INSECURE",
		Usage:  "use http instead of https",
	}
	debugFlag = cli.BoolFlag{
		Name:   "debug",
		EnvVar: "HPERF_DEBUG",
		Usage:  "enable debug output logs for client and server for a command",
	}
	concurrencyFlag = cli.IntFlag{
		Name:   "concurrency",
		EnvVar: "HPERF_CONCURRENCY",
		Value:  runtime.GOMAXPROCS(0) * 2,
		Usage:  "this flags controls how many concurrent requests to run per host",
	}
	delayFlag = cli.IntFlag{
		Name:   "request-delay",
		Value:  0,
		EnvVar: "HPERF_REQUEST_DELAY",
		Usage:  "adds a delay (in milliseconds) before sending http requests from host to host",
	}
	durationFlag = cli.IntFlag{
		Name:   "duration",
		Value:  30,
		EnvVar: "HPERF_DURATION",
		Usage:  "controls how long a test will run",
	}
	bufferSizeFlag = cli.IntFlag{
		Name:   "buffer-size",
		Value:  32000,
		EnvVar: "HPERF_BUFFER_SIZE",
		Usage:  "buffer size in bytes",
	}
	payloadSizeFlag = cli.IntFlag{
		Name:   "payload-size",
		Value:  1000000,
		EnvVar: "HPERF_PAYLOAD_SIZE",
		Usage:  "payload size in bytes",
	}
	restartOnErrorFlag = cli.BoolTFlag{
		Name:   "restart-on-error",
		EnvVar: "HPERF_RESTART_ON_ERROR",
		Usage:  "restart tests/clients upon error",
	}
	testIDFlag = cli.StringFlag{
		Name:  "id",
		Usage: "specify custom ID per test",
	}
	fileFlag = cli.StringFlag{
		Name:  "file",
		Usage: "input file path",
	}
	saveTestFlag = cli.BoolTFlag{
		Name:   "save",
		EnvVar: "HPERF_SAVE",
		Usage:  "save tests results on the server for retrieve later",
	}
	dnsServerFlag = cli.StringFlag{
		Name:   "dns-server",
		EnvVar: "HPERF_DNS_SERVER",
		Usage:  "use a custom DNS server to resolve hosts",
	}
	printStatsFlag = cli.BoolFlag{
		Name:  "print-stats",
		Usage: "Print stat points",
	}
	printErrFlag = cli.BoolFlag{
		Name:  "print-errors",
		Usage: "Print errors",
	}
)

var (
	baseFlags = []cli.Flag{
		debugFlag,
		insecureFlag,
	}
	Commands = []cli.Command{
		analyzeCMD,
		bandwidthCMD,
		deleteCMD,
		latencyCMD,
		listenCMD,
		listTestsCMD,
		requestsCMD,
		serverCMD,
		statDownloadCMD,
		stopCMD,
	}
)

func CreateApp() *cli.App {
	cli.HelpFlag = cli.BoolFlag{
		Name:  "help, h",
		Usage: "show help",
	}

	app := cli.NewApp()
	app.Action = func(ctx *cli.Context) error {
		showAppHelpAndExit(ctx)
		return exitStatus(0)
	}

	app.Before = before
	app.OnUsageError = func(context *cli.Context, err error, isSubcommand bool) error {
		fmt.Println(err)
		return nil
	}

	app.Name = "hperf"
	app.HideHelpCommand = true
	app.Usage = "MinIO network performance test utility for infrastructure at scale"
	app.Commands = Commands
	app.Author = "MinIO, Inc."
	app.Copyright = "(c) 2021-2024 MinIO, Inc."
	app.Version = version
	app.Flags = baseFlags
	app.CustomAppHelpTemplate = mcHelpTemplate
	app.EnableBashCompletion = false
	return app
}

var (
	GlobalContext    context.Context
	GlobalCancelFunc context.CancelCauseFunc
)

func before(ctx *cli.Context) error {
	debug = ctx.Bool("debug")
	insecure = ctx.Bool("insecure")
	GlobalContext, GlobalCancelFunc = context.WithCancelCause(context.Background())
	go handleOSSignal(GlobalCancelFunc)
	return nil
}

func parseConfig(ctx *cli.Context) (*shared.Config, error) {
	shared.DebugEnabled = debug

	var config *shared.Config
	hosts, err := shared.ParseHosts(
		ctx.String(hostsFlag.Name),
		ctx.String(dnsServerFlag.Name),
	)
	if err != nil {
		goto Error
	}

	config = &shared.Config{
		DialTimeout:    0,
		Debug:          debug,
		Hosts:          hosts,
		Insecure:       insecure,
		TestType:       shared.LatencyTest,
		Duration:       ctx.Int(durationFlag.Name),
		RequestDelay:   ctx.Int(delayFlag.Name),
		Concurrency:    ctx.Int(concurrencyFlag.Name),
		Proc:           ctx.Int(concurrencyFlag.Name),
		PayloadSize:    ctx.Int(payloadSizeFlag.Name),
		BufferKB:       ctx.Int(bufferSizeFlag.Name),
		Port:           ctx.String(portFlag.Name),
		Save:           ctx.BoolT(saveTestFlag.Name),
		TestID:         ctx.String(testIDFlag.Name),
		RestartOnError: ctx.BoolT(restartOnErrorFlag.Name),
		File:           ctx.String(fileFlag.Name),
		PrintFull:      ctx.Bool(printStatsFlag.Name),
		PrintErrors:    ctx.Bool(printErrFlag.Name),
	}

	switch ctx.Command.Name {
	case "latency", "bandwidth", "http", "get":
		if ctx.String("id") == "" {
			config.TestID = strconv.Itoa(int(time.Now().Unix()))
		}
	case "download":
		if ctx.String("id") == "" {
			err = errors.New("--id is required")
		}
		if ctx.String("file") == "" {
			err = errors.New("--file is required")
		}
	case "analyze":
		if ctx.String("file") == "" {
			err = errors.New("--file is required")
		}
	default:
	}

Error:
	if err != nil {
		cli.ShowCommandHelp(ctx, ctx.Command.Name)
		fmt.Println("")
		fmt.Println(client.ErrorStyle.Render("  " + err.Error()))
		fmt.Println("")
		fmt.Println("")
	}

	prettyprint(config, "CONFIG")
	return config, err
}

func prettyprint(data *shared.Config, title string) {
	if !data.Debug {
		return
	}
	dataB, err := json.Marshal(data)
	if err != nil {
		fmt.Println(err)
	}
	var out bytes.Buffer
	err = json.Indent(&out, dataB, "", "  ")
	if err != nil {
		fmt.Println(err)
	}
	fmt.Println(title, " ==============================")
	// outData := out.Bytes()
	fmt.Println(string(out.Bytes()))
	fmt.Println("=================")
}

func exitStatus(status int) error {
	return cli.NewExitError("", status)
}

func showAppHelpAndExit(cliCtx *cli.Context) {
	cli.ShowAppHelp(cliCtx)
	os.Exit(1)
}

func handleOSSignal(cancel context.CancelCauseFunc) {
	signalCh := make(chan os.Signal, 1)
	signal.Notify(signalCh,
		syscall.SIGHUP,
		syscall.SIGINT,
		syscall.SIGTERM,
		syscall.SIGQUIT)
	sig := <-signalCh
	cancel(fmt.Errorf("OS Signal caugh: %d", sig))
}
