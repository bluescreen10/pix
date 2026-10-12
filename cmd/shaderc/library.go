package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/bluescreen10/pix/render/shaders"
)

// libraryIncludeDir writes the embedded pix headers to disk, as pix/<name>.glsl, and
// returns the directory to put on glslc's include path. glslc is a separate process
// and cannot read the embedded files, so they live under the user cache directory,
// one directory per revision of the headers, written once and reused after.
func libraryIncludeDir() (string, error) {
	library, err := fs.Sub(shaders.Library, "lib")
	if err != nil {
		return "", err
	}
	revision, err := libraryRevision(library)
	if err != nil {
		return "", err
	}
	cache, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(cache, "pix", "shaderc", revision)
	if _, err := os.Stat(dir); err == nil {
		return dir, nil
	}
	if err := writeLibrary(library, dir); err != nil {
		return "", fmt.Errorf("write pix shader library: %w", err)
	}
	return dir, nil
}

// libraryRevision hashes every header's path and contents.
func libraryRevision(library fs.FS) (string, error) {
	hash := sha256.New()
	err := fs.WalkDir(library, ".", func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		contents, err := fs.ReadFile(library, path)
		if err != nil {
			return err
		}
		fmt.Fprintf(hash, "%s\x00%d\x00", path, len(contents))
		hash.Write(contents)
		return nil
	})
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil))[:16], nil
}

// writeLibrary writes the headers into a temporary sibling of dir and renames it into
// place, so concurrent go:generate runs never read a half-written library.
func writeLibrary(library fs.FS, dir string) error {
	if err := os.MkdirAll(filepath.Dir(dir), 0755); err != nil {
		return err
	}
	staging, err := os.MkdirTemp(filepath.Dir(dir), filepath.Base(dir)+"-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(staging)

	if err := os.CopyFS(filepath.Join(staging, "pix"), library); err != nil {
		return err
	}
	if err := os.Rename(staging, dir); err != nil {
		// Another run got there first; its copy is the same headers.
		if _, statErr := os.Stat(dir); statErr == nil {
			return nil
		}
		return err
	}
	return nil
}
