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
	"syscall"

	"github.com/minio/cli"
	"github.com/minio/hperf/client"
	"github.com/minio/hperf/shared"
)

const (
	VERSION = "4.0.8"
)

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
		Usage:  "Hosts that will used for the current command",
	}
	portFlag = cli.StringFlag{
		Name:   "port",
		Value:  "9010",
		EnvVar: "HPERF_PORT",
		Usage:  "Port used to communicate with hosts",
	}
	insecureFlag = cli.BoolTFlag{
		Name:   "insecure",
		EnvVar: "HPERF_INSECURE",
		Usage:  "Use http instead of https",
	}
	debugFlag = cli.BoolFlag{
		Name:   "debug",
		EnvVar: "HPERF_DEBUG",
		Usage:  "Enable debug output in the client and on the servers for the particular command",
	}
	concurrencyFlag = cli.IntFlag{
		Name:   "concurrency",
		EnvVar: "HPERF_CONCURRENCY",
		Value:  runtime.NumCPU() * 2,
		Usage:  "This flags controls how many concurrent requests we run between each host, the default is (number of cpus)x2",
	}
	delayFlag = cli.IntFlag{
		Name:   "requestDelay",
		Value:  0,
		EnvVar: "HPERF_REQUEST_DELAY",
		Usage:  "Creates a delay (in Milliseconds) before sending http requests from host to host",
	}
	durationFlag = cli.IntFlag{
		Name:   "duration",
		Value:  30,
		EnvVar: "HPERF_DURATION",
		Usage:  "Controls how long the test will be ran",
	}
	bufferSizeFlag = cli.IntFlag{
		Name:   "bufferSize",
		Value:  32000,
		EnvVar: "HPERF_BUFFER_SIZE",
		Usage:  "Buffer size in Bytes",
	}
	payloadSizeFlag = cli.IntFlag{
		Name:   "payloadSize",
		Value:  1000000,
		EnvVar: "HPERF_PAYLOAD_SIZE",
		Usage:  "Payload size in Bytes",
	}
	restartOnErrorFlag = cli.BoolTFlag{
		Name:   "restartOnError",
		EnvVar: "HPERF_RESTART_ON_ERROR",
		Usage:  "restart tests/clients if an error occures",
	}
	testIDFlag = cli.StringFlag{
		Name:  "id",
		Usage: "The id is a tag you can use to label tests which run on the servers",
	}
	saveTestFlag = cli.BoolTFlag{
		Name:   "save",
		EnvVar: "HPERF_SAVE",
		Usage:  "Save tests results on the server for querying later",
	}
	dnsServerFlag = cli.StringFlag{
		Name:   "dnsServer",
		EnvVar: "HPERF_DNS_SERVER",
		Usage:  "Hperf will use this DNS server to resolve hosts which are not in an IP format",
	}
)

var (
	baseFlags = []cli.Flag{
		debugFlag,
	}
	Commands = []cli.Command{
		serverCMD,

		bandwidthCMD,
		requestsCMD,
		latencyCMD,
		listenCMD,

		listTestsCMD,
		getTestsCMD,
		stopCMD,
		deleteCMD,
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
	app.HideHelpCommand = false
	app.Usage = "MinIO network performance test utility for infrastructure at scale"
	app.Commands = Commands
	app.Author = "MinIO, Inc."
	app.Version = VERSION
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
		TestType:       shared.LatencyTest,
		Duration:       ctx.Int(durationFlag.Name),
		RequestDelay:   ctx.Int(delayFlag.Name),
		Concurrency:    ctx.Int(concurrencyFlag.Name),
		Insecure:       ctx.Bool(insecureFlag.Name),
		Proc:           ctx.Int(concurrencyFlag.Name),
		PayloadSize:    ctx.Int(payloadSizeFlag.Name),
		BufferKB:       ctx.Int(bufferSizeFlag.Name),
		Port:           ctx.String(portFlag.Name),
		Save:           ctx.BoolT(saveTestFlag.Name),
		TestID:         ctx.String(testIDFlag.Name),
		RestartOnError: ctx.BoolT(restartOnErrorFlag.Name),
		Hosts:          hosts,
	}

	if ctx.String("id") == "" {
		switch ctx.Command.Name {
		case "latency", "bandwidth", "http":
			err = errors.New("--id is required")
		case "get":
			err = errors.New("--id is required")
		default:
		}
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
