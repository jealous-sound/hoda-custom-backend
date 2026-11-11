package report

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

const fileName = "reports.jsonl"

type AbuseType string

const (
	Harassment AbuseType = "HARRASSMENT"
	Sexual     AbuseType = "SEXUAL"
	Explicit   AbuseType = "EXPLICIT"
	Other      AbuseType = "OTHER"
)

func (a AbuseType) Valid() bool {
	switch a {
	case Harassment, Sexual, Explicit, Other:
		return true
	}
	return false
}

type Entry struct {
	Time        int64     `json:"time"`
	Reporter    string    `json:"reporter"`
	IssueType   AbuseType `json:"issue_type"`
	Description string    `json:"description"`
}

type Log struct {
	path  string
	mutex sync.Mutex
}

func NewLog(dir string) (*Log, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create report directory: %w", err)
	}
	return &Log{path: filepath.Join(dir, fileName)}, nil
}

func (l *Log) Append(entry Entry) error {
	line, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("encode report: %w", err)
	}
	l.mutex.Lock()
	defer l.mutex.Unlock()
	file, err := os.OpenFile(l.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open reports: %w", err)
	}
	if _, err := file.Write(append(line, '\n')); err != nil {
		file.Close()
		return fmt.Errorf("write report: %w", err)
	}
	return file.Close()
}

func (l *Log) Entries() ([]Entry, error) {
	l.mutex.Lock()
	data, err := os.ReadFile(l.path)
	l.mutex.Unlock()
	if errors.Is(err, os.ErrNotExist) {
		return []Entry{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read reports: %w", err)
	}
	entries := []Entry{}
	for line := range bytes.Lines(data) {
		var entry Entry
		if err := json.Unmarshal(line, &entry); err == nil {
			entries = append(entries, entry)
		}
	}
	return entries, nil
}
