package main

import (
	"os"
	"testing"
)

// TestOpenStorageReadsAndWritesAtTheRequestedOffset verifies that the storage offset is transparent to callers.
func TestOpenStorageReadsAndWritesAtTheRequestedOffset(t *testing.T) {
	file, err := os.CreateTemp(t.TempDir(), "storage-*.img")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()

	initial := make([]byte, 32)
	if _, err := file.Write(initial); err != nil {
		t.Fatal(err)
	}

	if _, err := file.WriteAt([]byte("original"), 16); err != nil {
		t.Fatal(err)
	}

	storage, err := openStorage(file.Name(), 16, true)
	if err != nil {
		t.Fatal(err)
	}
	defer storage.Close()

	buf := make([]byte, 8)
	if _, err := storage.ReadAt(buf, 0); err != nil {
		t.Fatal(err)
	}

	if string(buf) != "original" {
		t.Fatalf("got %q, want %q", string(buf), "original")
	}

	if _, err := storage.WriteAt([]byte("updated"), 0); err != nil {
		t.Fatal(err)
	}

	raw := make([]byte, 7)
	if _, err := file.ReadAt(raw, 16); err != nil {
		t.Fatal(err)
	}

	if string(raw) != "updated" {
		t.Fatalf("got %q, want %q", string(raw), "updated")
	}
}

// TestOpenStorageRejectsNegativeOffset verifies that invalid offsets are rejected before opening the file.
func TestOpenStorageRejectsNegativeOffset(t *testing.T) {
	_, err := openStorage("does-not-matter", -1, false)
	if err == nil {
		t.Fatal("expected error")
	}
}
