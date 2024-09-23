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

package server

import (
	"bufio"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/gofiber/contrib/websocket"
	"github.com/minio/hperf/shared"
)

func streamTestFilesToWebsocket(con *websocket.Conn, testID string) (err error) {
	var files []string
	files, err = filepath.Glob(filepath.Join(basePath, testID+".*"))
	if err != nil {
		return
	}
	msg := new(shared.WebsocketSignal)
	for _, path := range files {
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		s := bufio.NewScanner(f)
		for s.Scan() {
			msg.Data = s.Bytes()
			msg.SType = shared.GetTest
			msg.Code = 200
			err = con.WriteJSON(msg)
			if err != nil {
				return err
			}
		}
		if s.Err() != nil {
			return s.Err()
		}
	}

	return nil
}

func deleteTestsFromDisk(con *websocket.Conn, signal shared.WebsocketSignal) (err error) {
	defer SendDone(con)

	if signal.Config.TestID == "" {
		err = os.RemoveAll(basePath)
		if err != nil {
			SendError(con, err)
		}
	}

	var files []string
	files, err = filepath.Glob(filepath.Join(basePath, signal.Config.TestID+".*"))
	if err != nil {
		SendError(con, err)
		return
	}

	for _, path := range files {
		err = os.Remove(path)
		if err != nil {
			SendError(con, err)
		}
	}

	return
}

func listTestsFromDisk() (finalList []shared.TestInfo, err error) {
	var files []string
	files, err = filepath.Glob(filepath.Join(basePath, "*.1"))
	if err != nil {
		return
	}

	finalList = make([]shared.TestInfo, 0)
	for _, path := range files {
		var stat os.FileInfo
		stat, err = os.Stat(path)
		if err != nil {
			return
		}
		trimPath := strings.TrimSuffix(path, ".1")
		finalPath := strings.Split(trimPath, string(os.PathSeparator))
		finalList = append(finalList, shared.TestInfo{
			ID:   finalPath[len(finalPath)-1],
			Time: stat.ModTime(),
		})
	}
	return
}

func resetTestFiles(t *test) (err error) {
	var files []string
	files, err = filepath.Glob(filepath.Join(basePath, t.ID+"*"))
	if err != nil {
		return
	}

	for _, match := range files {
		err = os.Remove(match)
		if err != nil {
			return
		}
	}
	return
}

func newTestFile(t *test) (f *os.File, err error) {
	if t.DataFile != nil {
		t.DataFile.Close()
	}

	err = os.MkdirAll(basePath, 0o777)
	if err != nil {
		return
	}
	t.DataFileIndex++
	t.DataFile, err = os.Create(basePath + t.ID + "." + strconv.Itoa(t.DataFileIndex))
	if err != nil {
		return
	}

	return
}
