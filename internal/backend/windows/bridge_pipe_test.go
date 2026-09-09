//go:build windows

package windows

import (
	"fmt"
	"io"
	"os"
	"sync/atomic"
	"testing"
	"time"
)

var bridgePipeTestID atomic.Uint64

func uniqueBridgePipeName(t *testing.T) string {
	t.Helper()
	return fmt.Sprintf(`\\.\pipe\gotto-hando-test-%d-%d`, os.Getpid(), bridgePipeTestID.Add(1))
}

func acceptBridgePipe(t *testing.T, l *PipeListener) <-chan io.ReadWriteCloser {
	t.Helper()
	accepted := make(chan io.ReadWriteCloser, 1)
	go func() {
		conn, err := l.Accept()
		if err != nil {
			t.Errorf("Accept: %v", err)
			return
		}
		accepted <- conn
	}()
	return accepted
}

func TestPipeConnCloseFlushesTerminalFrameBeforeDisconnect(t *testing.T) {
	name := uniqueBridgePipeName(t)
	l, err := listenBridgeAt(name)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()

	accepted := acceptBridgePipe(t, l)
	client, err := DialBridge(name)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	server := <-accepted
	if _, err := server.Write([]byte("terminal-done\n")); err != nil {
		t.Fatal(err)
	}
	closed := make(chan error, 1)
	go func() { closed <- server.Close() }()

	select {
	case err := <-closed:
		t.Fatalf("Close returned before client read terminal frame: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	buf := make([]byte, len("terminal-done\n"))
	if _, err := io.ReadFull(client, buf); err != nil {
		t.Fatalf("read terminal frame: %v", err)
	}
	if got := string(buf); got != "terminal-done\n" {
		t.Errorf("terminal frame = %q", got)
	}
	select {
	case err := <-closed:
		if err != nil {
			t.Fatalf("Close: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Close did not finish after client consumed terminal frame")
	}
}

func TestPipeConnCloseAfterDisconnectedClientAllowsNextAccept(t *testing.T) {
	name := uniqueBridgePipeName(t)
	l, err := listenBridgeAt(name)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()

	accepted := acceptBridgePipe(t, l)
	client, err := DialBridge(name)
	if err != nil {
		t.Fatal(err)
	}
	server := <-accepted
	_ = client.Close()
	// FlushFileBuffers may either observe the disconnected peer or complete
	// successfully when no output remained buffered. In both cases Close must
	// disconnect the instance so the listener can accept again.
	_ = server.Close()

	nextAccepted := acceptBridgePipe(t, l)
	nextClient, err := DialBridge(name)
	if err != nil {
		t.Fatal(err)
	}
	defer nextClient.Close()
	select {
	case nextServer := <-nextAccepted:
		_ = nextClient.Close()
		_ = nextServer.Close()
	case <-time.After(2 * time.Second):
		t.Fatal("listener did not accept a client after failed flush")
	}
}
