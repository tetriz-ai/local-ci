// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package cliutils

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

const outputManifest = ".local-ci-owned.json"
const outputIgnore = "# Created by local-ci — keeps local-ci's working output out of git.\n*\n"

type outputOwnership struct {
	Version int               `json:"version"`
	Files   map[string]string `json:"files"`
}

// SetupOutputCliDir prepares <repo>/output/local-ci. Previous output is cleared only
// when every entry still matches the last ownership snapshot. Unknown or edited
// files require manual relocation; neither --force nor a familiar filename claims them.
// os.Root confines operations to the repository. Callers must serialize runs in
// a worktree and prevent concurrent edits to its output while a run is active.
func SetupOutputCliDir(path string) error {
	root, rel, err := openOutputRoot(path)
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()
	if err := verifyOutputOwnership(root, rel); err != nil {
		return err
	}
	if err := root.RemoveAll(rel); err != nil {
		return fmt.Errorf("clearing owned output: %w", err)
	}
	if err := root.MkdirAll(rel, 0o750); err != nil {
		return fmt.Errorf("creating output directory: %w", err)
	}
	// Each file is newly created; never truncate an existing hardlink or symlink.
	if err := writeNewOutputFile(root, filepath.Join(rel, ".gitignore"), []byte(outputIgnore)); err != nil {
		return err
	}
	if err := writeNewOutputFile(root, filepath.Join(rel, "stdoutput.log"), nil); err != nil {
		return err
	}
	// Keep existing project ignore policy byte-for-byte. The inner ignore also
	// hides the logs when an existing output/.gitignore has different rules.
	parentIgnore := filepath.Join(filepath.Dir(rel), ".gitignore")
	if _, err := root.Lstat(parentIgnore); os.IsNotExist(err) {
		if err := writeNewOutputFile(root, parentIgnore, []byte(outputIgnore)); err != nil {
			return err
		}
	} else if err != nil {
		return fmt.Errorf("checking existing output ignore: %w", err)
	}
	return recordOutputOwnership(root, rel)
}

// FinalizeOutputCliDir records files produced during a run, after writers have
// been flushed. A crash or failed snapshot leaves changed output unclaimed, so
// the next setup refuses instead of deleting it. Host programs using setup must
// call finalization after their writers stop to enable automatic reuse.
func FinalizeOutputCliDir(path string) error {
	root, rel, err := openOutputRoot(path)
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()
	return recordOutputOwnership(root, rel)
}

func openOutputRoot(path string) (*os.Root, string, error) {
	path = filepath.Clean(path)
	if filepath.Base(path) != "local-ci" || filepath.Base(filepath.Dir(path)) != "output" {
		return nil, "", fmt.Errorf("output path must end in output/local-ci")
	}
	root, err := os.OpenRoot(filepath.Dir(filepath.Dir(path)))
	if err != nil {
		return nil, "", fmt.Errorf("opening repository root: %w", err)
	}
	rel := filepath.Join("output", "local-ci")
	for _, name := range []string{"output", rel} {
		info, err := root.Lstat(name)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			_ = root.Close()
			return nil, "", fmt.Errorf("checking %s: %w", name, err)
		}
		if !info.IsDir() {
			_ = root.Close()
			return nil, "", fmt.Errorf("refusing non-directory or symlink at %s", name)
		}
	}
	return root, rel, nil
}

func verifyOutputOwnership(root *os.Root, rel string) error {
	if _, err := root.Lstat(rel); os.IsNotExist(err) {
		return nil
	} else if err != nil {
		return fmt.Errorf("checking output directory: %w", err)
	}
	current, err := snapshotOutput(root, rel)
	if err != nil {
		return err
	}
	manifestPath := filepath.Join(rel, outputManifest)
	info, err := root.Lstat(manifestPath)
	if os.IsNotExist(err) {
		if len(current) == 0 {
			return nil
		}
		return fmt.Errorf("unowned output exists in %s; move it aside manually before running local-ci", rel)
	}
	if err != nil {
		return fmt.Errorf("checking output ownership: %w", err)
	}
	if !info.Mode().IsRegular() || info.Size() > 4<<20 {
		return fmt.Errorf("output ownership manifest must be a regular file at most 4 MiB")
	}
	file, err := root.Open(manifestPath)
	if err != nil {
		return fmt.Errorf("reading output ownership: %w", err)
	}
	decoder := json.NewDecoder(io.LimitReader(file, 4<<20))
	decoder.DisallowUnknownFields()
	var previous outputOwnership
	decodeErr := decoder.Decode(&previous)
	trailingErr := decoder.Decode(new(any))
	closeErr := file.Close()
	if decodeErr != nil || trailingErr != io.EOF || previous.Version != 1 || previous.Files == nil || closeErr != nil {
		return fmt.Errorf("invalid output ownership manifest; move output aside manually")
	}
	for name, digest := range previous.Files {
		if name == outputManifest || !fs.ValidPath(name) || strings.Contains(name, "\\") || (digest != "directory" && !validOutputDigest(digest)) {
			return fmt.Errorf("invalid output ownership entry")
		}
	}
	for name, digest := range current {
		if previous.Files[name] != digest {
			return fmt.Errorf("unowned or edited output %s; move it aside manually before running local-ci", filepath.Join(rel, name))
		}
	}
	return nil
}

func validOutputDigest(digest string) bool {
	decoded, err := hex.DecodeString(digest)
	return err == nil && len(decoded) == sha256.Size
}

func snapshotOutput(root *os.Root, rel string) (map[string]string, error) {
	subtree, err := root.OpenRoot(rel)
	if err != nil {
		return nil, fmt.Errorf("opening output directory: %w", err)
	}
	defer func() { _ = subtree.Close() }()
	files := make(map[string]string)
	// Go 1.25 ReadDir entries can resolve Info against the process cwd.
	// Enumerate names only, then inspect every entry explicitly through the root.
	var walk func(string) error
	walk = func(name string) error {
		if strings.Contains(filepath.ToSlash(name), "\\") {
			return fmt.Errorf("output name cannot be represented safely: %s", name)
		}
		info, err := subtree.Lstat(name)
		if err != nil {
			return err
		}
		if info.IsDir() {
			if name != "." {
				files[filepath.ToSlash(name)] = "directory"
			}
			dir, err := subtree.Open(name)
			if err != nil {
				return err
			}
			names, readErr := dir.Readdirnames(-1)
			closeErr := dir.Close()
			if readErr != nil {
				return readErr
			}
			if closeErr != nil {
				return closeErr
			}
			for _, child := range names {
				if err := walk(filepath.Join(name, child)); err != nil {
					return err
				}
			}
			return nil
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("refusing nonregular output %s", name)
		}
		if name == outputManifest {
			return nil
		}
		file, err := subtree.Open(name)
		if err != nil {
			return err
		}
		digest := sha256.New()
		_, readErr := io.Copy(digest, file)
		closeErr := file.Close()
		if readErr != nil {
			return readErr
		}
		if closeErr != nil {
			return closeErr
		}
		files[filepath.ToSlash(name)] = hex.EncodeToString(digest.Sum(nil))
		return nil
	}
	err = walk(".")
	if err != nil {
		return nil, fmt.Errorf("inspecting output ownership: %w", err)
	}
	return files, nil
}

func recordOutputOwnership(root *os.Root, rel string) error {
	files, err := snapshotOutput(root, rel)
	if err != nil {
		return err
	}
	body, err := json.MarshalIndent(outputOwnership{Version: 1, Files: files}, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding output ownership: %w", err)
	}
	if len(body)+1 > 4<<20 {
		return fmt.Errorf("output ownership snapshot exceeds 4 MiB")
	}
	nonce := make([]byte, 16)
	if _, err := rand.Read(nonce); err != nil {
		return fmt.Errorf("naming output ownership snapshot: %w", err)
	}
	temporary := filepath.Join(rel, ".local-ci-owned-"+hex.EncodeToString(nonce)+".tmp")
	if err := writeNewOutputFile(root, temporary, append(body, '\n')); err != nil {
		return err
	}
	defer func() { _ = root.Remove(temporary) }()
	if err := root.Rename(temporary, filepath.Join(rel, outputManifest)); err != nil {
		return fmt.Errorf("installing output ownership snapshot: %w", err)
	}
	return nil
}

func writeNewOutputFile(root *os.Root, name string, body []byte) error {
	file, err := root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("creating %s: %w", name, err)
	}
	_, writeErr := file.Write(body)
	closeErr := file.Close()
	if writeErr != nil {
		return fmt.Errorf("writing %s: %w", name, writeErr)
	}
	if closeErr != nil {
		return fmt.Errorf("closing %s: %w", name, closeErr)
	}
	return nil
}
