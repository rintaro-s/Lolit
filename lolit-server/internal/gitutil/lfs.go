package gitutil

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// lfsPointerPrefix is the header every Git LFS pointer file starts with.
// Lolit stores CAD binaries (SLDPRT/STEP/...) via LFS, so `git show` on
// those paths returns this small pointer text instead of the real bytes.
const lfsPointerPrefix = "version https://git-lfs.github.com/spec/v1"

// IsLFSPointer reports whether blob content is a Git LFS pointer file rather
// than the real file content.
func IsLFSPointer(content []byte) bool {
	return bytes.HasPrefix(bytes.TrimSpace(content), []byte(lfsPointerPrefix))
}

// ParseLFSPointer extracts the oid and size from a Git LFS pointer file.
func ParseLFSPointer(content []byte) (oid string, size int64, err error) {
	lines := strings.Split(string(content), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if v, ok := strings.CutPrefix(line, "oid sha256:"); ok {
			oid = v
		}
		if v, ok := strings.CutPrefix(line, "size "); ok {
			size, err = strconv.ParseInt(v, 10, 64)
			if err != nil {
				return "", 0, fmt.Errorf("parse lfs pointer size: %w", err)
			}
		}
	}
	if oid == "" {
		return "", 0, fmt.Errorf("not a valid lfs pointer (missing oid)")
	}
	return oid, size, nil
}

// FetchLFSObject downloads the real bytes of an LFS object from Gitea's LFS
// HTTP API, authenticating with the server's Gitea admin credentials on the
// caller's behalf (Lolit users never need their own Gitea credentials).
func FetchLFSObject(giteaURL, user, pass, repo, oid string, size int64) ([]byte, error) {
	batchURL := strings.TrimSuffix(giteaURL, "/") + "/" + repo + ".git/info/lfs/objects/batch"
	reqBody, _ := json.Marshal(map[string]interface{}{
		"operation": "download",
		"transfers": []string{"basic"},
		"objects":   []map[string]interface{}{{"oid": oid, "size": size}},
	})

	client := &http.Client{Timeout: 30 * time.Second}
	req, err := http.NewRequest("POST", batchURL, bytes.NewReader(reqBody))
	if err != nil {
		return nil, err
	}
	req.SetBasicAuth(user, pass)
	req.Header.Set("Content-Type", "application/vnd.git-lfs+json")
	req.Header.Set("Accept", "application/vnd.git-lfs+json")
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("lfs batch request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("lfs batch request: server returned %s: %s", resp.Status, string(b))
	}

	var batch struct {
		Objects []struct {
			OID     string `json:"oid"`
			Actions struct {
				Download struct {
					Href   string            `json:"href"`
					Header map[string]string `json:"header"`
				} `json:"download"`
			} `json:"actions"`
			Error *struct {
				Message string `json:"message"`
			} `json:"error"`
		} `json:"objects"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&batch); err != nil {
		return nil, fmt.Errorf("decode lfs batch response: %w", err)
	}
	if len(batch.Objects) == 0 {
		return nil, fmt.Errorf("lfs batch response had no objects")
	}
	obj := batch.Objects[0]
	if obj.Error != nil {
		return nil, fmt.Errorf("lfs object error: %s", obj.Error.Message)
	}
	if obj.Actions.Download.Href == "" {
		return nil, fmt.Errorf("lfs object has no download action (already up to date on server?)")
	}

	dlURL, err := url.Parse(obj.Actions.Download.Href)
	if err != nil {
		return nil, fmt.Errorf("invalid lfs download href: %w", err)
	}
	dlReq, err := http.NewRequest("GET", dlURL.String(), nil)
	if err != nil {
		return nil, err
	}
	for k, v := range obj.Actions.Download.Header {
		dlReq.Header.Set(k, v)
	}
	if dlReq.Header.Get("Authorization") == "" {
		dlReq.SetBasicAuth(user, pass)
	}
	dlResp, err := client.Do(dlReq)
	if err != nil {
		return nil, fmt.Errorf("lfs object download: %w", err)
	}
	defer dlResp.Body.Close()
	if dlResp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(dlResp.Body)
		return nil, fmt.Errorf("lfs object download: server returned %s: %s", dlResp.Status, string(b))
	}
	return io.ReadAll(dlResp.Body)
}
