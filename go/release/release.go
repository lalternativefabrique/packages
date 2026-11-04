// Package release publishes build artifacts to a public S3 bucket the way the
// Skalpai CLIs are shipped: every file under <prefix>/<version>/ and mirrored
// under <prefix>/latest/, beside one SHA256SUMS, all public-read.
package release

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

const Latest = "latest"

const sumsName = "SHA256SUMS"

type Uploader interface {
	Put(ctx context.Context, key string, body io.ReadSeeker, size int64) error
}

type Config struct {
	Prefix    string
	Version   string
	PublicURL string
}

type Artifact struct {
	Name      string
	SHA256    string
	URL       string
	LatestURL string
}

type Result struct {
	Version   string
	Artifacts []Artifact
	SumsURL   string
}

func Publish(ctx context.Context, up Uploader, cfg Config, files []string) (Result, error) {
	if cfg.Version == "" || cfg.Version == Latest || strings.Contains(cfg.Version, "/") {
		return Result{}, fmt.Errorf("invalid version %q", cfg.Version)
	}
	if len(files) == 0 {
		return Result{}, errors.New("no artifact to publish")
	}

	artifacts := make([]Artifact, 0, len(files))
	seen := map[string]bool{}
	for _, file := range files {
		name := filepath.Base(file)
		if name == sumsName || seen[name] {
			return Result{}, fmt.Errorf("duplicate or reserved artifact name %q", name)
		}
		seen[name] = true
		sum, err := sha256File(file)
		if err != nil {
			return Result{}, err
		}
		artifacts = append(artifacts, Artifact{
			Name:      name,
			SHA256:    sum,
			URL:       cfg.url(cfg.Version, name),
			LatestURL: cfg.url(Latest, name),
		})
	}
	sort.Slice(artifacts, func(i, j int) bool { return artifacts[i].Name < artifacts[j].Name })

	var sums bytes.Buffer
	for _, a := range artifacts {
		fmt.Fprintf(&sums, "%s  %s\n", a.SHA256, a.Name)
	}

	for _, dir := range []string{cfg.Version, Latest} {
		for _, file := range files {
			if err := putFile(ctx, up, cfg.key(dir, filepath.Base(file)), file); err != nil {
				return Result{}, err
			}
		}
		if err := up.Put(ctx, cfg.key(dir, sumsName), bytes.NewReader(sums.Bytes()), int64(sums.Len())); err != nil {
			return Result{}, fmt.Errorf("upload %s: %w", sumsName, err)
		}
	}

	return Result{Version: cfg.Version, Artifacts: artifacts, SumsURL: cfg.url(cfg.Version, sumsName)}, nil
}

func (c Config) key(dir, name string) string {
	return path.Join(strings.Trim(c.Prefix, "/"), dir, name)
}

func (c Config) url(dir, name string) string {
	return strings.TrimRight(c.PublicURL, "/") + "/" + c.key(dir, name)
}

func putFile(ctx context.Context, up Uploader, key, file string) error {
	f, err := os.Open(file)
	if err != nil {
		return err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return err
	}
	if err := up.Put(ctx, key, f, info.Size()); err != nil {
		return fmt.Errorf("upload %s: %w", key, err)
	}
	return nil
}

func sha256File(file string) (string, error) {
	f, err := os.Open(file)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
