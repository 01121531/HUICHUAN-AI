package datasetcapture

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestBrowserListsPagesAndDownloadsAllowedFiles(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sample-20260715-node.jsonl")
	content := []byte("{\"session_id\":\"one\"}\n{\"session_id\":\"two\"}\n{\"session_id\":\"three\"}\n")
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "sample-20260715-node.jsonl.corrupt"), []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "sample-20260715-other-node.jsonl"), content, 0o600); err != nil {
		t.Fatal(err)
	}

	browser := NewBrowser(filepath.Join(dir, "sample-{date}-{node}.jsonl"), "node")
	files, err := browser.ListFiles()
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files[0].Name != filepath.Base(path) {
		t.Fatalf("unexpected files: %#v", files)
	}
	page, err := browser.Records(files[0].ID, 1, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Records) != 2 || !page.HasMore || page.TotalRows != 3 {
		t.Fatalf("unexpected page: %#v", page)
	}
	if string(page.Records[0]) != `{"session_id":"three"}` || string(page.Records[1]) != `{"session_id":"two"}` {
		t.Fatalf("records are not newest-first: %q", page.Records)
	}
	line, _, err := browser.Record(files[0].ID, 2)
	if err != nil {
		t.Fatal(err)
	}
	if string(line) != "{\"session_id\":\"two\"}\n" {
		t.Fatalf("unexpected record: %q", line)
	}
	if _, err := browser.Resolve("../../not-allowed"); err != ErrCaptureFileNotFound {
		t.Fatalf("expected not found for unknown id, got %v", err)
	}
	deleted, err := browser.Delete(files[0].ID)
	if err != nil || deleted.Name != filepath.Base(path) {
		t.Fatalf("delete failed: file=%#v err=%v", deleted, err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("capture file still exists after delete: %v", err)
	}
	if _, err := browser.Delete(files[0].ID); err != ErrCaptureFileNotFound {
		t.Fatalf("second delete should return not found, got %v", err)
	}
}

func TestBrowserDiscoversPartitionedConversationFiles(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "node-node", "user-42", "token-7", "session-0011223344556677.jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("{\"session_id\":\"0011223344556677\"}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	browser := NewBrowser(filepath.Join(dir, "sample-{date}-{node}.jsonl"), "node")
	files, err := browser.ListFiles()
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files[0].UserKey != "42" || files[0].TokenKey != "7" || files[0].SessionID != "0011223344556677" {
		t.Fatalf("unexpected partitioned files: %#v", files)
	}
}

func TestBrowserRejectsMalformedJSONL(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sample.jsonl")
	if err := os.WriteFile(path, []byte("{\"ok\":true}\nnot-json\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	browser := NewBrowser(path, "node")
	files, err := browser.ListFiles()
	if err != nil || len(files) != 1 {
		t.Fatalf("list files: %v %#v", err, files)
	}
	if _, err := browser.Records(files[0].ID, 1, 20); err == nil {
		t.Fatal("expected malformed JSONL error")
	}
}
