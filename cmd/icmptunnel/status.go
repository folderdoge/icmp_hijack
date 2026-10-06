package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode"
)

type currentStatus struct {
	State     string `json:"state"`
	Detail    string `json:"detail"`
	ChangedAt string `json:"changed_at"`
}

type statusReporter struct {
	mu   sync.Mutex
	path string
	last currentStatus
}

func validState(state string) bool {
	switch state {
	case "disabled", "connecting", "connected", "connect_error", "auth_error", "error":
		return true
	}
	return false
}

func statusDetail(detail string) string {
	detail = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, detail)
	var b strings.Builder
	for _, r := range detail {
		if b.Len()+len(string(r)) > 384 {
			break
		}
		b.WriteRune(r)
	}
	return strings.TrimSpace(b.String())
}

func (s *statusReporter) set(state, detail string) error {
	if s == nil || s.path == "" {
		return nil
	}
	if !validState(state) {
		return errors.New("invalid current state")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	detail = statusDetail(detail)
	if s.last.State == state && s.last.Detail == detail {
		return nil
	}
	next := currentStatus{state, detail, time.Now().Format("2006-01-02 15:04:05")}
	b, err := json.Marshal(next)
	if err != nil {
		return err
	}
	if err = os.MkdirAll(filepath.Dir(s.path), 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(s.path), ".status-*.tmp")
	if err != nil {
		return err
	}
	temp := f.Name()
	defer os.Remove(temp)
	if _, err = f.Write(append(b, '\n')); err != nil {
		_ = f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	if err = os.Rename(temp, s.path); err != nil {
		return err
	}
	s.last = next
	return nil
}

func readStatus(path string) (currentStatus, error) {
	f, err := os.Open(path)
	if err != nil {
		return currentStatus{}, err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, 1025))
	if err != nil {
		return currentStatus{}, err
	}
	if len(b) > 1024 {
		return currentStatus{}, errors.New("current state is too large")
	}
	var status currentStatus
	if err = json.Unmarshal(b, &status); err != nil {
		return status, errors.New("invalid current state")
	}
	if !validState(status.State) {
		return status, errors.New("invalid current state")
	}
	if _, err = time.Parse("2006-01-02 15:04:05", status.ChangedAt); err != nil {
		return status, errors.New("invalid current state time")
	}
	status.Detail = statusDetail(status.Detail)
	return status, nil
}

func runStatus(args []string) error {
	f := flag.NewFlagSet("status", flag.ContinueOnError)
	path := f.String("file", "", "current state JSON path")
	state := f.String("set", "", "overwrite current state")
	detail := f.String("detail", "", "current state description")
	if err := f.Parse(args); err != nil {
		return err
	}
	if *path == "" || f.NArg() != 0 {
		return errors.New("status requires --file PATH")
	}
	if *state != "" {
		return (&statusReporter{path: *path}).set(*state, *detail)
	}
	s, err := readStatus(*path)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(os.Stdout, "%s\t%s\t%s\n", s.State, s.ChangedAt, s.Detail)
	return err
}
