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

package client

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"runtime/debug"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/fasthttp/websocket"
	"github.com/minio/hperf/shared"
)

var (
	responseDPS    = make([]shared.DP, 0)
	responseERR    = make([]shared.TError, 0)
	responseLock   = sync.Mutex{}
	websockets     []*wsClient
	hostsDoingWork atomic.Int32
)

type wsClient struct {
	ID   int
	Host string
	Con  *websocket.Conn
}

func (c *wsClient) SendError(e error) error {
	if e == nil {
		return nil
	}
	msg := new(shared.WebsocketSignal)
	msg.SType = shared.Err
	msg.Error = e.Error()
	return c.Con.WriteJSON(msg)
}

func (c *wsClient) Close() (err error) {
	return c.Remove()
}

func (c *wsClient) Remove() (err error) {
	err = c.Con.Close()
	websockets[c.ID] = nil
	return
}

func itterateWebsockets(action func(c *wsClient)) {
	for i := 0; i < len(websockets); i++ {
		if websockets[i] == nil {
			continue
		}
		action(websockets[i])
	}
}

func (c *wsClient) NewSignal(signal shared.SignalType, conf *shared.Config) *shared.WebsocketSignal {
	msg := new(shared.WebsocketSignal)
	msg.SType = signal
	msg.Config = conf
	return msg
}

func (c *wsClient) Ping() (err error) {
	msg := new(shared.WebsocketSignal)
	msg.SType = shared.Ping
	err = c.Con.WriteJSON(msg)
	return
}

var (
	testList = make(map[string]shared.TestInfo)
	testLock = sync.Mutex{}
)

func initializeClient(ctx context.Context, c *shared.Config) (err error) {
	websockets = make([]*wsClient, len(c.Hosts))

	clientID := 0
	done := make(chan struct{}, len(c.Hosts))
	for _, host := range c.Hosts {
		go handleWSConnection(ctx, c, host, clientID, done)
		clientID++
	}

	doneCount := 0
	timeout := time.NewTicker(time.Second * 10)

	for {
		select {
		case <-done:
			doneCount++
			hostsDoingWork.Add(1)
			if doneCount == len(c.Hosts) {
				return
			}
		case <-ctx.Done():
			return errors.New("Context canceled")
		case <-timeout.C:
			return errors.New("Timeout when connecting to hosts")
		}
	}
}

func handleWSConnection(ctx context.Context, c *shared.Config, host string, id int, done chan struct{}) {
	var err error
	defer func() {
		r := recover()
		if r != nil {
			fmt.Println(r, string(debug.Stack()))
		}
		if ctx.Err() != nil {
			hostsDoingWork.Add(-1)
			return
		}
		if c.RestartOnError && err != nil {
			time.Sleep(500 * time.Millisecond)
			go handleWSConnection(ctx, c, host, id, done)
		} else {
			hostsDoingWork.Add(-1)
		}
	}()

	socket := websockets[id]
	if socket == nil {
		websockets[id] = new(wsClient)
		socket = websockets[id]
		socket.ID = id
	}

	socket.Host = host

	dialer := websocket.Dialer{
		Proxy:            http.ProxyFromEnvironment,
		HandshakeTimeout: time.Second * c.DialTimeout,
		ReadBufferSize:   1000000,
		WriteBufferSize:  1000000,
	}

	shared.DEBUG(WarningStyle.Render("Connecting to ", host, ":", c.Port))

	con, _, dialErr := dialer.DialContext(
		ctx,
		"ws://"+host+":"+c.Port+"/ws/"+host,
		nil)
	if dialErr != nil {
		PrintError(dialErr)
		err = dialErr
		return
	}
	socket.Con = con

	msg := new(shared.WebsocketSignal)
	err = con.ReadJSON(&msg)
	if err != nil {
		err = fmt.Errorf("Unable to read message from server on first connect %s", err)
		PrintError(err)
		return
	}
	if msg.Code != shared.OK {
		err = fmt.Errorf("Received %d from server on connect", msg.Code)
		PrintError(err)
		return
	}
	shared.DEBUG(SuccessStyle.Render("Connected to ", host, ":", c.Port))

	done <- struct{}{}
	for {
		signal := new(shared.WebsocketSignal)
		err = con.ReadJSON(&signal)
		if err != nil {
			PrintError(err)
			return
		}
		if shared.DebugEnabled {
			fmt.Printf("WebsocketSignal: %+v\n", signal)
		}
		switch signal.SType {
		case shared.Stats:
			go praseDataPoint(signal.DataPoint, c)
		case shared.ListTests:
			go parseTestList(signal.TestList)
		case shared.GetTest:
			go receiveJSONDataPoint(signal.Data, c)
		case shared.Err:
			go PrintErrorString(signal.Error)
		case shared.Done:
			shared.DEBUG(SuccessStyle.Render("Host Finished: ", con.RemoteAddr().String()))
			return
		}
	}
}

func PrintTError(err shared.TError) {
	fmt.Println(ErrorStyle.Render(err.Created.Format(time.RFC3339), " - ", err.Error))
}

func PrintErrorString(err string) {
	fmt.Println(ErrorStyle.Render(err))
}

func PrintError(err error) {
	if err == nil {
		return
	}
	fmt.Println(ErrorStyle.Render("ERROR: ", err.Error()))
}

func receiveJSONDataPoint(data []byte, c *shared.Config) {
	responseLock.Lock()
	defer responseLock.Unlock()

	dp := new(shared.DP)
	err := json.Unmarshal(data, &dp)
	if err != nil {
		PrintError(err)
		return
	}

	responseDPS = append(responseDPS, *dp)
}

func keepAliveLoop(ctx context.Context, tickerfunc func() (shouldExit bool)) error {
	for ctx.Err() == nil {
		time.Sleep(1 * time.Second)

		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		if tickerfunc != nil && tickerfunc() {
			break
		}

		if hostsDoingWork.Load() <= 0 {
			return ctx.Err()
		}

	}
	return ctx.Err()
}

func Listen(ctx context.Context, c shared.Config) (err error) {
	cancelContext, cancel := context.WithCancel(ctx)
	defer cancel()
	err = initializeClient(cancelContext, &c)
	if err != nil {
		return
	}

	itterateWebsockets(func(ws *wsClient) {
		err = ws.Con.WriteJSON(ws.NewSignal(shared.ListenTest, &c))
		if err != nil {
			return
		}
	})

	return keepAliveLoop(ctx, nil)
}

func Stop(ctx context.Context, c shared.Config) (err error) {
	cancelContext, cancel := context.WithCancel(ctx)
	defer cancel()
	err = initializeClient(cancelContext, &c)
	if err != nil {
		return
	}

	itterateWebsockets(func(ws *wsClient) {
		err = ws.Con.WriteJSON(ws.NewSignal(shared.StopAllTests, &c))
		if err != nil {
			return
		}
	})

	return keepAliveLoop(ctx, nil)
}

func RunTest(ctx context.Context, c shared.Config) (err error) {
	cancelContext, cancel := context.WithCancel(ctx)
	defer cancel()
	err = initializeClient(cancelContext, &c)
	if err != nil {
		return
	}

	itterateWebsockets(func(ws *wsClient) {
		err = ws.Con.WriteJSON(ws.NewSignal(shared.RunTest, &c))
		if err != nil {
			return
		}
	})

	return keepAliveLoop(ctx, nil)
}

func ListTests(ctx context.Context, c shared.Config) (err error) {
	cancelContext, cancel := context.WithCancel(ctx)
	defer cancel()
	err = initializeClient(cancelContext, &c)
	if err != nil {
		return
	}

	itterateWebsockets(func(ws *wsClient) {
		err = ws.Con.WriteJSON(ws.NewSignal(shared.ListTests, &c))
		if err != nil {
			return
		}
	})

	err = keepAliveLoop(ctx, nil)
	if err != nil {
		return
	}

	printHeader(ListHeaders)
	tableStyle := lipgloss.NewStyle()

	keys := []string{}
	for id := range testList {
		keys = append(keys, id)
	}

	slices.SortFunc(keys, func(a string, b string) int {
		if testList[a].Time.Before(testList[b].Time) {
			return 1
		} else {
			return -1
		}
	})

	for i := range keys {
		PrintColumns(
			tableStyle,
			column{strconv.Itoa(i), headerSlice[IntNumber].width},
			column{keys[i], headerSlice[ID].width},
			column{testList[keys[i]].Time.Format("02/01/2006 3:04 PM"), headerSlice[ID].width},
		)
	}

	return err
}

func DeleteTests(ctx context.Context, c shared.Config) (err error) {
	cancelContext, cancel := context.WithCancel(ctx)
	defer cancel()
	err = initializeClient(cancelContext, &c)
	if err != nil {
		return
	}

	itterateWebsockets(func(ws *wsClient) {
		err = ws.Con.WriteJSON(ws.NewSignal(shared.DeleteTests, &c))
		if err != nil {
			return
		}
	})

	return keepAliveLoop(ctx, nil)
}

func parseTestList(list []shared.TestInfo) {
	testLock.Lock()
	defer testLock.Unlock()

	for i := range list {
		_, ok := testList[list[i].ID]
		if !ok {
			testList[list[i].ID] = list[i]
		}
	}
}

func GetTest(ctx context.Context, c shared.Config) (err error) {
	cancelContext, cancel := context.WithCancel(ctx)
	defer cancel()
	err = initializeClient(cancelContext, &c)
	if err != nil {
		return
	}

	itterateWebsockets(func(ws *wsClient) {
		err = ws.Con.WriteJSON(ws.NewSignal(shared.GetTest, &c))
		if err != nil {
			fmt.Println(err)
			return
		}
	})

	_ = keepAliveLoop(ctx, nil)

	if len(responseDPS) < 1 {
		PrintErrorString("No datapoints found")
		return
	}

	slices.SortFunc(responseDPS, func(a shared.DP, b shared.DP) int {
		if a.Created.Before(b.Created) {
			return -1
		} else {
			return 1
		}
	})

	if c.Output != "" {
		fmt.Println("saving:", c.Output)
		f, err := os.Create(c.Output)
		if err != nil {
			return err
		}
		for i := range responseDPS {
			outb, err := json.Marshal(responseDPS[i])
			if err != nil {
				PrintError(err)
				continue
			}
			_, err = f.Write(append(outb, []byte{10}...))
			if err != nil {
				return err
			}
		}

		return nil
	}

	printDataPointHeaders(responseDPS[0].Type)
	for i := range responseDPS {
		dp := responseDPS[i]
		sp1 := strings.Split(dp.Local, ":")
		sp2 := strings.Split(sp1[0], ".")
		s1 := lipgloss.NewStyle().Background(lipgloss.Color(getHex(sp2[len(sp2)-1])))
		printTableRow(s1, &dp, dp.Type)
	}

	return nil
}
