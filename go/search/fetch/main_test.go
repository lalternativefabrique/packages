package fetch

import (
	"net/http"
	"os"
	"testing"
)

func TestMain(m *testing.M) {
	UseHTTPClient(&http.Client{Timeout: fetchTimeout})
	os.Exit(m.Run())
}
