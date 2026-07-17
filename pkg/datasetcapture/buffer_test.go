package datasetcapture

import (
	"errors"
	"testing"
)

func TestSegmentedBufferPreservesDataAndReleasesBudget(t *testing.T) {
	pool := NewBufferPool(4, 16)
	buffer := pool.NewBuffer(12)

	if err := buffer.TryAppend([]byte("abc")); err != nil {
		t.Fatal(err)
	}
	if err := buffer.TryAppendString("defghi"); err != nil {
		t.Fatal(err)
	}
	data, err := buffer.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "abcdefghi" {
		t.Fatalf("buffer = %q", data)
	}
	if pool.InFlightBytes() != 12 {
		t.Fatalf("in-flight bytes = %d, want 12", pool.InFlightBytes())
	}

	buffer.Release()
	if pool.InFlightBytes() != 0 {
		t.Fatalf("in-flight bytes after release = %d", pool.InFlightBytes())
	}
}

func TestSegmentedBufferDropsWholeSampleAtPerSampleLimit(t *testing.T) {
	pool := NewBufferPool(4, 16)
	buffer := pool.NewBuffer(5)
	if err := buffer.TryAppendString("1234"); err != nil {
		t.Fatal(err)
	}
	if err := buffer.TryAppendString("56"); !errors.Is(err, ErrSampleTooLarge) {
		t.Fatalf("error = %v, want ErrSampleTooLarge", err)
	}
	if buffer.Len() != 0 || pool.InFlightBytes() != 0 {
		t.Fatalf("dropped buffer retained memory: len=%d in_flight=%d", buffer.Len(), pool.InFlightBytes())
	}
}

func TestSegmentedBufferEnforcesGlobalInFlightLimit(t *testing.T) {
	pool := NewBufferPool(4, 4)
	first := pool.NewBuffer(8)
	second := pool.NewBuffer(8)
	if err := first.TryAppendString("1"); err != nil {
		t.Fatal(err)
	}
	if err := second.TryAppendString("2"); !errors.Is(err, ErrInFlightLimitReached) {
		t.Fatalf("error = %v, want ErrInFlightLimitReached", err)
	}
	first.Release()
	if pool.InFlightBytes() != 0 {
		t.Fatalf("in-flight bytes after release = %d", pool.InFlightBytes())
	}
}
