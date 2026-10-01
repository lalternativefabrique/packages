package fileguard

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"strings"
	"testing"
	"time"
)

// eicar is the industry test string every antivirus reports as infected,
// assembled at run time so this file itself is not flagged on a dev machine.
var eicar = strings.Join([]string{`X5O!P%@AP[4\PZX54(P^)7CC)7}$`, "EICAR-STANDARD-", "ANTIVIRUS-TEST-FILE!$H+H*"}, "")

// fakeClamd speaks clamd's INSTREAM and PING the way clamd does, flagging any
// stream that contains the EICAR string and refusing streams past maxStream.
func fakeClamd(t *testing.T, maxStream int) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go serveClamd(conn, maxStream)
		}
	}()
	return "tcp://" + ln.Addr().String()
}

func serveClamd(conn net.Conn, maxStream int) {
	defer conn.Close()
	cmd := make([]byte, 0, 16)
	one := make([]byte, 1)
	for {
		if _, err := conn.Read(one); err != nil {
			return
		}
		if one[0] == 0 {
			break
		}
		cmd = append(cmd, one[0])
	}
	switch string(cmd) {
	case "zPING":
		_, _ = conn.Write([]byte("PONG\x00"))
		return
	case "zINSTREAM":
	default:
		_, _ = conn.Write([]byte("UNKNOWN COMMAND\x00"))
		return
	}
	var stream bytes.Buffer
	size := make([]byte, 4)
	for {
		if _, err := io.ReadFull(conn, size); err != nil {
			return
		}
		n := binary.BigEndian.Uint32(size)
		if n == 0 {
			break
		}
		if _, err := io.CopyN(&stream, conn, int64(n)); err != nil {
			return
		}
		if maxStream > 0 && stream.Len() > maxStream {
			_, _ = conn.Write([]byte("INSTREAM size limit exceeded. ERROR\x00"))
			return
		}
	}
	if strings.Contains(stream.String(), "EICAR-STANDARD-"+"ANTIVIRUS-TEST-FILE") {
		_, _ = conn.Write([]byte("stream: Eicar-Test-Signature FOUND\x00"))
		return
	}
	_, _ = conn.Write([]byte("stream: OK\x00"))
}

func TestScanCleanAndInfected(t *testing.T) {
	s, err := NewScanner(fakeClamd(t, 0), ScannerOptions{ChunkSize: 7})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	v, err := s.Scan(ctx, strings.NewReader(strings.Repeat("harmless ", 1000)))
	if err != nil || v.Infected {
		t.Fatalf("clean file: %+v %v", v, err)
	}
	v, err = s.Scan(ctx, strings.NewReader("a pdf with "+eicar+" inside"))
	if err != nil || !v.Infected || v.Signature != "Eicar-Test-Signature" {
		t.Fatalf("eicar: %+v %v", v, err)
	}
}

func TestScanPastTheScannersLimitIsNotScanned(t *testing.T) {
	s, _ := NewScanner(fakeClamd(t, 100), ScannerOptions{ChunkSize: 32})
	_, err := s.Scan(context.Background(), bytes.NewReader(make([]byte, 10_000)))
	if !errors.Is(err, ErrScanTooLarge) {
		t.Fatalf("got %v", err)
	}
}

func TestScanWithoutClamdIsNotClean(t *testing.T) {
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	addr := ln.Addr().String()
	_ = ln.Close()
	s, _ := NewScanner("tcp://"+addr, ScannerOptions{Timeout: time.Second})
	v, err := s.Scan(context.Background(), strings.NewReader("x"))
	if !errors.Is(err, ErrScannerUnavailable) || v.Infected {
		t.Fatalf("got %+v %v", v, err)
	}
	if err := s.Ping(context.Background()); !errors.Is(err, ErrScannerUnavailable) {
		t.Fatalf("ping: %v", err)
	}
}

func TestPing(t *testing.T) {
	s, _ := NewScanner(fakeClamd(t, 0), ScannerOptions{})
	if err := s.Ping(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestNewScannerRefusesBadAddresses(t *testing.T) {
	for _, addr := range []string{"clamd:3310", "http://clamd:3310", "tcp://", "udp://x:1"} {
		if _, err := NewScanner(addr, ScannerOptions{}); err == nil {
			t.Errorf("%q accepted", addr)
		}
	}
}

func TestParseReply(t *testing.T) {
	for line, want := range map[string]Verdict{
		"stream: OK":                         {},
		"stream: Win.Test.EICAR_HDB-1 FOUND": {Infected: true, Signature: "Win.Test.EICAR_HDB-1"},
	} {
		got, err := parseReply(line)
		if err != nil || got != want {
			t.Errorf("parseReply(%q) = %+v, %v", line, got, err)
		}
	}
	if _, err := parseReply("lstat() failed. ERROR"); !errors.Is(err, ErrScannerUnavailable) {
		t.Errorf("unexpected error kind: %v", err)
	}
}
