package report

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEntriesReadBackWhatWasAppendedInOrder(t *testing.T) {
	dir := t.TempDir()
	log, err := NewLog(dir)
	if err != nil {
		t.Fatal(err)
	}
	if entries, err := log.Entries(); err != nil || len(entries) != 0 {
		t.Fatalf("a new log holds %v (%v)", entries, err)
	}
	first := Entry{Time: 1, Reporter: "a", IssueType: Harassment, Description: "Guild Name: X, Id: guild-1"}
	second := Entry{Time: 2, Reporter: "b", IssueType: Other, Description: "World Chat Line: 7"}
	for _, entry := range []Entry{first, second} {
		if err := log.Append(entry); err != nil {
			t.Fatal(err)
		}
	}
	entries, err := log.Entries()
	if err != nil || len(entries) != 2 || entries[0] != first || entries[1] != second {
		t.Fatalf("entries = %+v (%v)", entries, err)
	}
}

func TestEntriesSkipALineThatIsNotAReport(t *testing.T) {
	dir := t.TempDir()
	log, err := NewLog(dir)
	if err != nil {
		t.Fatal(err)
	}
	kept := Entry{Time: 3, Reporter: "a", IssueType: Sexual, Description: "kept"}
	if err := log.Append(kept); err != nil {
		t.Fatal(err)
	}
	file, err := os.OpenFile(filepath.Join(dir, fileName), os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteString("{half a line\n"); err != nil {
		t.Fatal(err)
	}
	file.Close()
	entries, err := log.Entries()
	if err != nil || len(entries) != 1 || entries[0] != kept {
		t.Fatalf("entries = %+v (%v)", entries, err)
	}
}
