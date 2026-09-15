package authz_test

import (
	"io/fs"
	"testing/fstest"
)

func fstest_(files map[string]string) fs.FS {
	m := fstest.MapFS{}
	for name, content := range files {
		m[name] = &fstest.MapFile{Data: []byte(content)}
	}
	return m
}
