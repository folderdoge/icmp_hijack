package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestCurrentStatusOverwriteBoundAndNoHistory(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "status.json")
	r := &statusReporter{path: path}
	if err := r.set("connected", "已连接"); err != nil {
		t.Fatal(err)
	}
	initial, _ := os.ReadFile(path)
	if err := r.set("connected", "已连接"); err != nil {
		t.Fatal(err)
	}
	same, _ := os.ReadFile(path)
	if string(initial) != string(same) {
		t.Fatal("unchanged state was rewritten")
	}
	if err := r.set("auth_error", strings.Repeat("失败\t\n", 300)); err != nil {
		t.Fatal(err)
	}
	current, err := readStatus(path)
	if err != nil {
		t.Fatal(err)
	}
	if current.State != "auth_error" || len(current.Detail) > 384 || strings.ContainsAny(current.Detail, "\t\r\n") {
		t.Fatalf("unsafe state: %+v", current)
	}
	files, _ := os.ReadDir(dir)
	if len(files) != 1 || files[0].Name() != "status.json" {
		t.Fatal("history or temporary files retained")
	}
	data, _ := os.ReadFile(path)
	if len(data) > 1024 || strings.Contains(string(data), "已连接") {
		t.Fatal("old state retained or unbounded")
	}
}

func TestCurrentStatusConcurrentWritesAndValidation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	r := &statusReporter{path: path}
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := r.set("connect_error", "正在重试"); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if _, err := readStatus(path); err != nil {
		t.Fatal(err)
	}
	if err := r.set("history", "invalid"); err == nil {
		t.Fatal("unknown enum accepted")
	}
	for _, data := range []string{strings.Repeat("x", 1025), `{"state":"connected","changed_at":"invalid\tstatusdata","detail":"x"}`, `{"state":"unknown","changed_at":"2026-10-06 10:00:00"}`} {
		if err := os.WriteFile(path, []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := readStatus(path); err == nil {
			t.Fatal("bad state accepted")
		}
	}
	if err := r.set("disabled", "已关闭"); err != nil {
		t.Fatal(err)
	}
	var s currentStatus
	data, _ := os.ReadFile(path)
	if err := json.Unmarshal(data, &s); err != nil {
		t.Fatal(err)
	}
}
