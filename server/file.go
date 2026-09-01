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

// testFiles returns the on-disk files belonging to one test.
//
// Read and delete paths deliberately do NOT apply shared.ValidateTestID: files
// already on disk may have been written by an older server under looser rules,
// and rejecting them here would leave a long-lived server pod listing tests it
// then refuses to serve or remove.
//
// They must not glob, though. An ID handed to filepath.Glob is a *pattern*, so
// "*" matched every test's files and "[ab]*" matched a chosen subset -- which
// made `delete --id '*'` destroy every saved test, the same failure as the
// unanchored pattern in resetTestFiles. Matching directory entries by exact
// prefix has no pattern semantics, so a metacharacter in an ID is just a
// character.
//
// The suffix must be the numeric index newTestFile assigns. That has always
// been the format, so it costs no legacy compatibility, and it stops "my_test"
// from claiming "my_test.1.1" -- which belongs to the test named "my_test.1".
func testFiles(id string) ([]string, error) {
	if id == "" {
		return nil, errors.New("test id is empty")
	}
	// An entry name can never contain a separator, so these could only ever
	// match nothing; rejecting them gives a clearer answer than silence.
	if strings.ContainsRune(id, '/') || strings.ContainsRune(id, os.PathSeparator) ||
		id == "." || id == ".." {
		return nil, fmt.Errorf("invalid test id (%s)", id)
	}

	entries, err := os.ReadDir(basePath)
	if err != nil {
		// A missing storage directory simply holds no tests, which is what the
		// previous glob reported too.
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	prefix := id + "."
	files := make([]string, 0, 4)
	for _, e := range entries {
		if e.IsDir() || !strings.HasPrefix(e.Name(), prefix) {
			continue
		}
		if _, convErr := strconv.Atoi(strings.TrimPrefix(e.Name(), prefix)); convErr != nil {
			continue
		}
		files = append(files, filepath.Join(basePath, e.Name()))
	}
	return files, nil
}

func streamTestFilesToWebsocket(p *wsPeer, testID string) (err error) {
	files, err := testFiles(testID)
	if err != nil {
		return err
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
	// without --id asks for. It has to return here: falling through would look
	// for files under a directory that no longer exists.
	if signal.Config.TestID == "" {
		if err = os.RemoveAll(basePath); err != nil {
			SendError(p, err)
		}
		return
	}

	files, err := testFiles(signal.Config.TestID)
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
