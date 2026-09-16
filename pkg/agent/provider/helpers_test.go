package provider

import (
	"os"
	"path/filepath"
	"testing"
)

func writeFakeScript(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "fake.sh")
	script := "#!/bin/sh\n" + content + "\n"
	if err := os.WriteFile(path, []byte(script), 0755); err != nil {
		t.Fatalf("write fake %v", err)
	}
	return path
}
