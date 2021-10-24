/*
 * Copyright (c) 2017, MegaEase
 * All rights reserved.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package common

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
)

// URLFriendlyCharactersRegex - safe characters for friendly url, rfc3986 section 2.3
var URLFriendlyCharactersRegex = regexp.MustCompile(`^[A-Za-z0-9\-_\.~]{1,253}$`)

// ValidateName validates the name.
func ValidateName(name string) error {
	if !URLFriendlyCharactersRegex.Match([]byte(name)) {
		return fmt.Errorf("invalid constant: %s", name)
	}

	return nil
}

// IsDirEmpty returns true if a directory is empty.
func IsDirEmpty(name string) bool {
	f, err := os.Open(name)
	if err != nil {
		return os.IsNotExist(err)
	}
	defer f.Close()

	_, err = f.Readdirnames(1)
	return err == io.EOF
}

// ExpandDir cleans the dir, and returns itself if it's absolute,
// otherwise prefix with the current/working directory.
func ExpandDir(dir string) string {
	wd := filepath.Dir(os.Args[0])
	if filepath.IsAbs(dir) {
		return filepath.Clean(dir)
	}
	return filepath.Clean(filepath.Join(wd, dir))
}

// MkdirAll wraps os.MakeAll with fixed perm.
func MkdirAll(path string) error {
	return os.MkdirAll(ExpandDir(path), 0o700)
}

// RemoveAll wraps os.RemoveAll.
func RemoveAll(path string) error {
	return os.RemoveAll(ExpandDir(path))
}

// BackupAndCleanDir cleans old stuff in both dir and backupDir,
// and backups dir to backupDir.
// The backupDir generated by appending postfix `_bak` for dir.
// It does nothing if dir does not exist.
func BackupAndCleanDir(dir string) error {
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return nil
	}

	dir = ExpandDir(dir)
	backupDir := dir + "_bak"

	err := os.RemoveAll(backupDir)
	if err != nil {
		return err
	}

	err = os.Rename(dir, backupDir)
	if err != nil {
		return err
	}

	return MkdirAll(dir)
}

// NormalizeZapLogPath is a workaround for https://github.com/uber-go/zap/issues/621
// the workaround is from https://github.com/ipfs/go-log/issues/73
func NormalizeZapLogPath(path string) string {
	if runtime.GOOS == "windows" {
		return "file:////%3F/" + filepath.ToSlash(path)
	}

	return path
}
