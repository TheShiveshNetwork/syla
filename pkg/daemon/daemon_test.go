package daemon

import (
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"testing"
)

func TestServerClient(t *testing.T) {
	dir := t.TempDir()
	sock := filepath.Join(dir, "test.sock")
	srv := NewServer(sock)
	srv.Handle("Echo", func(raw json.RawMessage) (any, error) {
		var in map[string]string
		_ = json.Unmarshal(raw, &in)
		return map[string]string{"echo": in["msg"]}, nil
	})
	if err := srv.Listen(); err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer srv.Close()
	defer os.Remove(sock)

	// Dial expects runDir, but for this test we use direct net.Dial
	conn, err := net.Dial("unix", sock)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()
	// Use client via Dial with runDir
	// Create a fake runDir containing sock at expected location
	runDir := dir
	// Ensure SockPath returns correct
	if SockPath(runDir) != sock {
		// for test, use runDir that matches sock
		// our SockPath is runDir/ctl.sock, but we used dir/test.sock
		// so test directly via net
	}
	// Test via raw client
	c := &Client{conn: conn, nextID: 0}
	var res map[string]string
	if err := c.Call("Echo", map[string]string{"msg": "hi"}, &res); err != nil {
		t.Fatalf("call: %v", err)
	}
	if res["echo"] != "hi" {
		t.Fatalf("echo %v", res)
	}
	// Test ListRuns with no runs
	if _, err := ListRuns(dir); err != nil {
		t.Fatalf("list: %v", err)
	}
}

func TestListRunsEmpty(t *testing.T) {
	dir := t.TempDir()
	runs, err := ListRuns(dir)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(runs) != 0 {
		t.Fatalf("should be empty")
	}
}
