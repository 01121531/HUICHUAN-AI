package datasetcapture

import (
	"errors"
	"io"
	"sync"
	"sync/atomic"
)

var (
	ErrSampleTooLarge       = errors.New("dataset capture sample exceeds the per-sample memory limit")
	ErrInFlightLimitReached = errors.New("dataset capture in-flight memory limit reached")
)

const defaultBufferSegmentSize = 64 << 10

type BufferPool struct {
	segmentSize           int
	maxInFlight           int64
	inFlight              atomic.Int64
	segments              sync.Pool
	droppedSampleTooLarge atomic.Int64
	droppedInFlightLimit  atomic.Int64
}

func NewBufferPool(segmentSize int, maxInFlight int64) *BufferPool {
	if segmentSize <= 0 {
		segmentSize = defaultBufferSegmentSize
	}
	pool := &BufferPool{segmentSize: segmentSize, maxInFlight: maxInFlight}
	pool.segments.New = func() any {
		return make([]byte, segmentSize)
	}
	return pool
}

func (p *BufferPool) NewBuffer(maxBytes int64) *SegmentedBuffer {
	return &SegmentedBuffer{pool: p, maxBytes: maxBytes}
}

func (p *BufferPool) InFlightBytes() int64 {
	if p == nil {
		return 0
	}
	return p.inFlight.Load()
}

func (p *BufferPool) acquire() ([]byte, bool) {
	if p == nil {
		return nil, false
	}
	reserved := int64(p.segmentSize)
	if !p.tryReserve(reserved) {
		return nil, false
	}
	segment := p.segments.Get().([]byte)
	return segment[:p.segmentSize], true
}

func (p *BufferPool) tryReserve(bytes int64) bool {
	if p == nil || bytes <= 0 {
		return true
	}
	if p.maxInFlight <= 0 {
		p.inFlight.Add(bytes)
		return true
	}
	for {
		current := p.inFlight.Load()
		if current > p.maxInFlight-bytes {
			return false
		}
		if p.inFlight.CompareAndSwap(current, current+bytes) {
			return true
		}
	}
}

func (p *BufferPool) releaseReserved(bytes int64) {
	if p == nil || bytes <= 0 {
		return
	}
	p.inFlight.Add(-bytes)
}

func (p *BufferPool) release(segment []byte) {
	if p == nil || segment == nil {
		return
	}
	p.releaseReserved(int64(p.segmentSize))
	p.segments.Put(segment[:p.segmentSize])
}

type SegmentedBuffer struct {
	pool     *BufferPool
	segments [][]byte
	size     int64
	maxBytes int64
	err      error
	released bool
}

func (b *SegmentedBuffer) TryAppend(data []byte) error {
	return b.appendBytes(data)
}

func (b *SegmentedBuffer) TryAppendString(data string) error {
	if b == nil || len(data) == 0 {
		return nil
	}
	if err := b.prepareAppend(len(data)); err != nil {
		return err
	}
	remaining := data
	for len(remaining) > 0 {
		segment, offset, err := b.writableSegment()
		if err != nil {
			return err
		}
		written := copy(segment[offset:], remaining)
		b.size += int64(written)
		remaining = remaining[written:]
	}
	return nil
}

func (b *SegmentedBuffer) appendBytes(data []byte) error {
	if b == nil || len(data) == 0 {
		return nil
	}
	if err := b.prepareAppend(len(data)); err != nil {
		return err
	}
	remaining := data
	for len(remaining) > 0 {
		segment, offset, err := b.writableSegment()
		if err != nil {
			return err
		}
		written := copy(segment[offset:], remaining)
		b.size += int64(written)
		remaining = remaining[written:]
	}
	return nil
}

func (b *SegmentedBuffer) prepareAppend(length int) error {
	if b.released {
		return ErrWriterClosed
	}
	if b.err != nil {
		return b.err
	}
	if b.maxBytes > 0 && int64(length) > b.maxBytes-b.size {
		b.discard(ErrSampleTooLarge)
		return b.err
	}
	return nil
}

func (b *SegmentedBuffer) writableSegment() ([]byte, int, error) {
	offset := int(b.size % int64(b.pool.segmentSize))
	if len(b.segments) == 0 || offset == 0 {
		segment, ok := b.pool.acquire()
		if !ok {
			b.discard(ErrInFlightLimitReached)
			return nil, 0, b.err
		}
		b.segments = append(b.segments, segment)
		offset = 0
	}
	return b.segments[len(b.segments)-1], offset, nil
}

func (b *SegmentedBuffer) Len() int64 {
	if b == nil {
		return 0
	}
	return b.size
}

func (b *SegmentedBuffer) Err() error {
	if b == nil {
		return nil
	}
	return b.err
}

func (b *SegmentedBuffer) Bytes() ([]byte, error) {
	if b == nil {
		return nil, nil
	}
	if b.err != nil {
		return nil, b.err
	}
	result := make([]byte, b.size)
	remaining := result
	for _, segment := range b.segments {
		written := copy(remaining, segment)
		remaining = remaining[written:]
		if len(remaining) == 0 {
			break
		}
	}
	return result, nil
}

func (b *SegmentedBuffer) WriteTo(writer io.Writer) (int64, error) {
	if b == nil {
		return 0, nil
	}
	if b.err != nil {
		return 0, b.err
	}
	remaining := b.size
	var total int64
	for _, segment := range b.segments {
		length := int64(len(segment))
		if length > remaining {
			length = remaining
		}
		written, err := writer.Write(segment[:length])
		total += int64(written)
		remaining -= int64(written)
		if err != nil {
			return total, err
		}
		if int64(written) != length {
			return total, io.ErrShortWrite
		}
		if remaining == 0 {
			break
		}
	}
	return total, nil
}

func (b *SegmentedBuffer) Release() {
	if b == nil || b.released {
		return
	}
	b.released = true
	b.releaseSegments()
}

func (b *SegmentedBuffer) discard(err error) {
	if b.err == nil && b.pool != nil {
		switch {
		case errors.Is(err, ErrSampleTooLarge):
			b.pool.droppedSampleTooLarge.Add(1)
		case errors.Is(err, ErrInFlightLimitReached):
			b.pool.droppedInFlightLimit.Add(1)
		}
	}
	b.err = err
	b.size = 0
	b.releaseSegments()
}

func (b *SegmentedBuffer) releaseSegments() {
	for _, segment := range b.segments {
		b.pool.release(segment)
	}
	b.segments = nil
}
