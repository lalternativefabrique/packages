package release

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

type memUploader map[string]string

func (m memUploader) Put(_ context.Context, key string, body io.ReadSeeker, _ int64) error {
	b, err := io.ReadAll(body)
	m[key] = string(b)
	return err
}

func writeFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestPublishVersionLatestAndSums(t *testing.T) {
	dir := t.TempDir()
	files := []string{writeFile(t, dir, "b.tar.gz", "bbb"), writeFile(t, dir, "a.dmg", "aaa")}
	up := memUploader{}

	res, err := Publish(context.Background(), up, Config{Prefix: "/orbit/", Version: "20261104-0800-abc", PublicURL: "https://x.example/"}, files)
	if err != nil {
		t.Fatal(err)
	}

	keys := make([]string, 0, len(up))
	for k := range up {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	want := []string{
		"orbit/20261104-0800-abc/SHA256SUMS", "orbit/20261104-0800-abc/a.dmg", "orbit/20261104-0800-abc/b.tar.gz",
		"orbit/latest/SHA256SUMS", "orbit/latest/a.dmg", "orbit/latest/b.tar.gz",
	}
	if strings.Join(keys, ",") != strings.Join(want, ",") {
		t.Fatalf("keys = %v", keys)
	}

	const sumA = "9834876dcfb05cb167a5c24953eba58c4ac89b1adf57f28f2f9d09af107ee8f0"
	const sumB = "3e744b9dc39389baf0c5a0660589b8402f3dbb49b89b3e75f2c9355852a3c677"
	wantSums := sumA + "  a.dmg\n" + sumB + "  b.tar.gz\n"
	if got := up["orbit/latest/SHA256SUMS"]; got != wantSums {
		t.Fatalf("sums = %q", got)
	}
	if res.Artifacts[0].Name != "a.dmg" || res.Artifacts[0].SHA256 != sumA {
		t.Fatalf("artifact = %+v", res.Artifacts[0])
	}
	if res.Artifacts[0].URL != "https://x.example/orbit/20261104-0800-abc/a.dmg" || res.Artifacts[0].LatestURL != "https://x.example/orbit/latest/a.dmg" {
		t.Fatalf("urls = %+v", res.Artifacts[0])
	}
}

func TestPublishRejects(t *testing.T) {
	dir := t.TempDir()
	f := writeFile(t, dir, "a", "a")
	sums := writeFile(t, dir, "SHA256SUMS", "x")
	cases := map[string]struct {
		version string
		files   []string
	}{
		"latest version": {"latest", []string{f}},
		"empty version":  {"", []string{f}},
		"slash version":  {"a/b", []string{f}},
		"no files":       {"v1", nil},
		"duplicate name": {"v1", []string{f, f}},
		"reserved name":  {"v1", []string{sums}},
		"missing file":   {"v1", []string{filepath.Join(dir, "nope")}},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			up := memUploader{}
			if _, err := Publish(context.Background(), up, Config{Version: c.version}, c.files); err == nil {
				t.Fatal("expected error")
			}
			if len(up) != 0 {
				t.Fatalf("uploaded %v before failing", up)
			}
		})
	}
}
