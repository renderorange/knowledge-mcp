package search

import (
	"path/filepath"
	"testing"
	"time"
)

// TestOpenBackgroundDoesNotBlockWhileLocked covers the startup race: a second
// server instance must not wedge the MCP handshake when the first instance
// holds the single-writer index lock (bbolt flocks block indefinitely).
func TestOpenBackgroundDoesNotBlockWhileLocked(t *testing.T) {
	dir := t.TempDir()
	indexPath := filepath.Join(dir, "shared.index")

	// Instance A creates the index and holds the bbolt lock.
	a := newIndexReady(t, indexPath, []string{"test"})
	if err := a.Add("test/conv-001", SearchDocument{Summary: "persisted doc", Project: "test"}); err != nil {
		t.Fatalf("Add() to instance A: %v", err)
	}

	// Instance B attempts an async open; it must not block this goroutine.
	b := NewLazyIndex(indexPath, []string{"test"})
	b.OpenBackground(nil)
	defer b.Close()

	if err := b.WaitOpen(250 * time.Millisecond); err == nil {
		t.Fatal("WaitOpen() resolved while the index lock was still held, want unresolved")
	}

	// Once A releases the lock, B's pending open must complete on its own.
	if err := a.Close(); err != nil {
		t.Fatalf("closing instance A: %v", err)
	}
	if err := b.WaitOpen(10 * time.Second); err != nil {
		t.Fatalf("WaitOpen() after lock release: %v", err)
	}

	results, err := b.Query("test", "persisted", "", 10)
	if err != nil {
		t.Fatalf("Query() after recovery: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("Query() returned %d results after recovery, want 1", len(results))
	}
}

// TestQueryDoesNotHangWhileLocked covers tool calls: a query against a
// locked index must return an error promptly instead of blocking forever.
func TestQueryDoesNotHangWhileLocked(t *testing.T) {
	dir := t.TempDir()
	indexPath := filepath.Join(dir, "shared.index")

	a := newIndexReady(t, indexPath, []string{"test"})
	if err := a.Add("test/conv-001", SearchDocument{Summary: "locked doc", Project: "test"}); err != nil {
		t.Fatalf("Add() to instance A: %v", err)
	}

	b := NewLazyIndex(indexPath, []string{"test"})
	b.OpenBackground(nil)
	defer b.Close()

	done := make(chan error, 1)
	go func() {
		_, err := b.Query("test", "locked", "", 10)
		done <- err
	}()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("Query() succeeded against a locked index, want error")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Query() hung while the index was locked, want prompt error")
	}

	// After the lock is released the same query must succeed, proving the
	// instance recovers without a restart.
	if err := a.Close(); err != nil {
		t.Fatalf("closing instance A: %v", err)
	}
	if err := b.WaitOpen(10 * time.Second); err != nil {
		t.Fatalf("WaitOpen() after lock release: %v", err)
	}
	results, err := b.Query("test", "locked", "", 10)
	if err != nil {
		t.Fatalf("Query() after recovery: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("Query() returned %d results after recovery, want 1", len(results))
	}
}

// TestAddDoesNotPanicWhileLocked covers writes: Add must surface the open
// state as an error, never dereference a nil index.
func TestAddDoesNotPanicWhileLocked(t *testing.T) {
	dir := t.TempDir()
	indexPath := filepath.Join(dir, "shared.index")

	a := newIndexReady(t, indexPath, []string{"test"})

	b := NewLazyIndex(indexPath, []string{"test"})
	b.OpenBackground(nil)
	defer b.Close()

	errCh := make(chan error, 1)
	go func() {
		errCh <- b.Add("test/conv-002", SearchDocument{Summary: "new doc", Project: "test"})
	}()

	select {
	case err := <-errCh:
		if err == nil {
			t.Fatal("Add() succeeded against a locked index, want error")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Add() hung while the index was locked, want prompt error")
	}

	if err := a.Close(); err != nil {
		t.Fatalf("closing instance A: %v", err)
	}
	_ = b.WaitOpen(10 * time.Second)
}
