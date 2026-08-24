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
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/minio/hperf/shared"
)

// testGlob builds the pattern matching one test's files.
//
// Read and delete paths deliberately do NOT apply shared.ValidateTestID: files
// already on disk may have been written by an older server under no rules at
// all, and rejecting them here would leave a long-lived server pod listing
// tests it then refuses to serve or remove. The property that actually matters
// is that the pattern cannot escape the storage directory, which is what this
// checks. ValidateTestID still governs IDs that become NEW paths, in newTest.
func testGlob(id string) (string, error) {
	if id == "" {
		return "", errors.New("test id is empty")
	}
	base := filepath.Clean(basePath)
	pattern := filepath.Join(base, id+".*")
	// Join cleans its result, so an id carrying a separator or ".." moves the
	// pattern out of the storage directory and its parent stops being base.
	if filepath.Dir(pattern) != base {
		return "", fmt.Errorf("invalid test id (%s)", id)
	}
	return pattern, nil
}

func streamTestFilesToWebsocket(p *wsPeer, testID string) (err error) {
	pattern, err := testGlob(testID)
	if err != nil {
		return err
	}

	var files []string
	files, err = filepath.Glob(pattern)
	if err != nil {
		return
	}
	msg := new(shared.WebsocketSignal)
	for _, path := range files {
		if err = streamOneTestFile(p, msg, path); err != nil {
			return err
		}
	}

	return nil
}

// streamOneTestFile is a separate function so the file is closed when it
// returns. Opening inside the caller's loop leaked one descriptor per file per
// download, for the lifetime of the server, and every error return leaked too.
func streamOneTestFile(p *wsPeer, msg *shared.WebsocketSignal, path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	s := bufio.NewScanner(f)
	for s.Scan() {
		msg.Data = s.Bytes()
		msg.SType = shared.GetTest
		msg.Code = 200
		if err := p.writeJSON(msg); err != nil {
			return err
		}
	}
	return s.Err()
}

func deleteTestsFromDisk(p *wsPeer, signal shared.WebsocketSignal) (err error) {
	defer SendDone(p)

	// An empty ID means "delete every test", which is what `hperf delete`
	// without --id asks for. It has to return here: falling through would glob
	// ".*" against a directory that no longer exists.
	if signal.Config.TestID == "" {
		if err = os.RemoveAll(basePath); err != nil {
			SendError(p, err)
		}
		return
	}

	pattern, err := testGlob(signal.Config.TestID)
	if err != nil {
		SendError(p, err)
		return
	}

	var files []string
	files, err = filepath.Glob(pattern)
	if err != nil {
		SendError(p, err)
		return
	}

	for _, path := range files {
		if err = os.Remove(path); err != nil {
			SendError(p, err)
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
	if err = shared.ValidateTestID(t.ID); err != nil {
		return
	}

	// The pattern is anchored with the separator. Without it, "--id test"
	// matched -- and deleted -- test2.1, testing.1 and every other test whose
	// ID merely started with "test", and an empty ID matched everything.
	var files []string
	files, err = filepath.Glob(filepath.Join(basePath, t.ID+".*"))
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
