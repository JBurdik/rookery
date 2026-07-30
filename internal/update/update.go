// Package update checks GitHub Releases and installs verified Rook binaries.
package update

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

const (
	Repository   = "JBurdik/rookery"
	apiBase      = "https://api.github.com"
	downloadBase = "https://github.com"
)

type release struct {
	TagName string `json:"tag_name"`
}

// Check reports the latest stable release when it is newer than current.
func Check(ctx context.Context, client *http.Client, current string) (string, bool, error) {
	if client == nil {
		client = &http.Client{Timeout: 3 * time.Second}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiBase+"/repos/"+Repository+"/releases/latest", nil)
	if err != nil {
		return "", false, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := client.Do(req)
	if err != nil {
		return "", false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", false, fmt.Errorf("latest release: %s", resp.Status)
	}
	var latest release
	if err := json.NewDecoder(resp.Body).Decode(&latest); err != nil {
		return "", false, err
	}
	if latest.TagName == "" {
		return "", false, fmt.Errorf("latest release has no tag")
	}
	return latest.TagName, newer(latest.TagName, current), nil
}

// Install downloads the archive for this OS and architecture, verifies its
// SHA-256 against the release checksums, and atomically replaces executable.
func Install(ctx context.Context, client *http.Client, tag, executable string) error {
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		return fmt.Errorf("updates are not available for %s", runtime.GOOS)
	}
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	name := fmt.Sprintf("rook_%s_%s_%s.tar.gz", tag, runtime.GOOS, runtime.GOARCH)
	base := downloadBase + "/" + Repository + "/releases/download/" + tag + "/"
	checksums, err := get(ctx, client, base+"checksums.txt")
	if err != nil {
		return err
	}
	want, err := checksumFor(string(checksums), name)
	if err != nil {
		return err
	}
	archive, err := get(ctx, client, base+name)
	if err != nil {
		return err
	}
	got := sha256.Sum256(archive)
	if !strings.EqualFold(hex.EncodeToString(got[:]), want) {
		return fmt.Errorf("checksum mismatch for %s", name)
	}
	return replaceFromArchive(archive, executable)
}

func get(ctx context.Context, client *http.Client, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download %s: %s", url, resp.Status)
	}
	return io.ReadAll(resp.Body)
}

func checksumFor(contents, name string) (string, error) {
	for _, line := range strings.Split(contents, "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 && strings.TrimPrefix(fields[len(fields)-1], "*") == name {
			if len(fields[0]) == sha256.Size*2 {
				return fields[0], nil
			}
		}
	}
	return "", fmt.Errorf("no checksum for %s", name)
}

func replaceFromArchive(archive []byte, executable string) error {
	zr, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return err
	}
	defer zr.Close()
	tr := tar.NewReader(zr)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		if hdr.Typeflag != tar.TypeReg || filepath.Base(hdr.Name) != "rook" {
			continue
		}
		dir := filepath.Dir(executable)
		file, err := os.CreateTemp(dir, ".rook-update-*")
		if err != nil {
			return err
		}
		tmp := file.Name()
		defer os.Remove(tmp)
		if _, err := io.Copy(file, tr); err != nil {
			file.Close()
			return err
		}
		if err := file.Chmod(0o755); err != nil {
			file.Close()
			return err
		}
		if err := file.Close(); err != nil {
			return err
		}
		return os.Rename(tmp, executable)
	}
	return fmt.Errorf("release archive contains no rook executable")
}

func newer(candidate, current string) bool {
	parse := func(v string) ([3]int, bool) {
		var parts [3]int
		v = strings.TrimPrefix(v, "v")
		if i := strings.IndexByte(v, '-'); i >= 0 {
			v = v[:i]
		}
		items := strings.Split(v, ".")
		if len(items) != 3 {
			return parts, false
		}
		for i, item := range items {
			n, err := strconv.Atoi(item)
			if err != nil || n < 0 {
				return parts, false
			}
			parts[i] = n
		}
		return parts, true
	}
	a, okA := parse(candidate)
	b, okB := parse(current)
	if !okA || !okB {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return a[i] > b[i]
		}
	}
	return false
}
