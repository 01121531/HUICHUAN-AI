package datasetcapture

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"sync"
	"time"
)

type contextKey struct{}

type Attempt struct {
	RequestBody  []byte
	ResponseBody []byte
	Path         string
	ContentType  string
	Model        string
	Route        string
	Complete     bool
}

type Session struct {
	mu sync.Mutex

	capture              Capture
	attempt              Attempt
	requestBuffer        *SegmentedBuffer
	requestComplete      bool
	clientResponse       *SegmentedBuffer
	clientComplete       bool
	newRequestBuffer     func() *SegmentedBuffer
	reservationPool      *BufferPool
	reservedRequestBytes int64
	success              bool
}

func NewSession(capture Capture) *Session {
	if capture.CreatedAt.IsZero() {
		capture.CreatedAt = time.Now()
	}
	return &Session{capture: capture}
}

func WithSession(ctx context.Context, session *Session) context.Context {
	return context.WithValue(ctx, contextKey{}, session)
}

func FromContext(ctx context.Context) *Session {
	if ctx == nil {
		return nil
	}
	session, _ := ctx.Value(contextKey{}).(*Session)
	return session
}

func (s *Session) BeginAttempt(model, route string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	previous := s.requestBuffer
	s.attempt = Attempt{Model: model, Route: route}
	s.requestBuffer = nil
	s.requestComplete = false
	s.success = false
	s.mu.Unlock()
	previous.Release()
}

func (s *Session) SetRequestBufferFactory(factory func() *SegmentedBuffer) {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.newRequestBuffer = factory
	s.mu.Unlock()
}

func (s *Session) UpdateMetadata(userID, tokenID, model, route string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	if userID != "" {
		s.capture.UserID = userID
	}
	if tokenID != "" {
		s.capture.TokenID = tokenID
	}
	if model != "" {
		s.attempt.Model = model
	}
	if route != "" {
		s.attempt.Route = route
	}
	s.mu.Unlock()
}

func (s *Session) UpdateStorageMetadata(userID, tokenID, userGroup, requestedModel string, channelID int) {
	if s == nil {
		return
	}
	s.mu.Lock()
	if userID != "" {
		s.capture.UserID = userID
	}
	if tokenID != "" {
		s.capture.TokenID = tokenID
	}
	s.capture.UserGroup = userGroup
	s.capture.RequestedModel = requestedModel
	s.capture.ChannelID = channelID
	s.mu.Unlock()
}

func (s *Session) FailAttempt() {
	if s == nil {
		return
	}
	s.mu.Lock()
	buffer := s.requestBuffer
	s.requestBuffer = nil
	s.requestComplete = false
	s.attempt = Attempt{}
	s.success = false
	s.mu.Unlock()
	buffer.Release()
}

func (s *Session) SucceedAttempt() {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.success = true
	s.mu.Unlock()
}

func (s *Session) CaptureUpstreamRequest(req *http.Request) error {
	if s == nil || req == nil || req.Body == nil {
		return nil
	}
	s.mu.Lock()
	s.attempt.Path = req.URL.Path
	s.attempt.ContentType = req.Header.Get("Content-Type")
	buffer := s.requestBuffer
	if buffer == nil && s.newRequestBuffer != nil {
		buffer = s.newRequestBuffer()
		s.requestBuffer = buffer
	}
	s.requestComplete = false
	s.mu.Unlock()
	if buffer == nil {
		return nil
	}
	req.Body = &captureRequestReadCloser{
		ReadCloser: req.Body,
		buffer:     buffer,
		expected:   req.ContentLength,
		onDone: func(complete bool) {
			s.mu.Lock()
			s.requestComplete = complete
			s.mu.Unlock()
		},
	}
	return nil
}

func (s *Session) SetClientResponse(body []byte, complete bool) {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.attempt.ResponseBody = append([]byte(nil), body...)
	s.attempt.Complete = complete
	s.mu.Unlock()
}

func (s *Session) SetClientResponseBuffer(buffer *SegmentedBuffer, complete bool) {
	if s == nil {
		buffer.Release()
		return
	}
	s.mu.Lock()
	previous := s.clientResponse
	s.clientResponse = buffer
	s.clientComplete = complete
	s.mu.Unlock()
	previous.Release()
}

func (s *Session) Release() {
	if s == nil {
		return
	}
	s.mu.Lock()
	requestBuffer := s.requestBuffer
	clientBuffer := s.clientResponse
	s.clientResponse = nil
	s.requestBuffer = nil
	reservationPool := s.reservationPool
	reservedBytes := s.reservedRequestBytes
	s.reservationPool = nil
	s.reservedRequestBytes = 0
	s.mu.Unlock()
	requestBuffer.Release()
	clientBuffer.Release()
	reservationPool.releaseReserved(reservedBytes)
}

func (s *Session) reserveRetainedMemory(pool *BufferPool, maxSampleBytes int64) error {
	if s == nil || pool == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.reservationPool != nil {
		return nil
	}
	if s.requestBuffer != nil && s.requestBuffer.Err() != nil {
		return s.requestBuffer.Err()
	}
	if s.clientResponse != nil && s.clientResponse.Err() != nil {
		return s.clientResponse.Err()
	}
	requestBytes := int64(len(s.capture.RequestBody) + len(s.attempt.RequestBody))
	totalBytes := requestBytes
	if s.requestBuffer != nil {
		totalBytes += s.requestBuffer.Len()
	}
	if s.clientResponse != nil {
		totalBytes += s.clientResponse.Len()
	}
	if maxSampleBytes > 0 && totalBytes > maxSampleBytes {
		pool.droppedSampleTooLarge.Add(1)
		return ErrSampleTooLarge
	}
	if !pool.tryReserve(requestBytes) {
		pool.droppedInFlightLimit.Add(1)
		return ErrInFlightLimitReached
	}
	s.reservationPool = pool
	s.reservedRequestBytes = requestBytes
	return nil
}

func (s *Session) SetSpoolThreshold(bytes int64) {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.capture.SpoolThresholdBytes = bytes
	s.mu.Unlock()
}

func (s *Session) BuildRecord() (Record, error) {
	s.mu.Lock()
	capture := s.capture
	attempt := s.attempt
	requestBuffer := s.requestBuffer
	requestComplete := s.requestComplete
	clientResponse := s.clientResponse
	clientComplete := s.clientComplete
	s.clientResponse = nil
	s.requestBuffer = nil
	success := s.success
	s.mu.Unlock()
	defer requestBuffer.Release()
	defer clientResponse.Release()
	if capture.SessionSource == "" {
		// Session identity extraction is intentionally deferred to the worker so
		// the request goroutine only performs the policy metadata scan.
		capture.SessionSource = sessionSourceFromBody(capture.RequestBody)
	}
	if clientResponse != nil {
		body, err := materializeClientResponse(clientResponse, capture.SpoolThresholdBytes)
		if err != nil {
			return Record{}, err
		}
		attempt.ResponseBody = body
		attempt.Complete = clientComplete
	}
	if requestBuffer != nil {
		if !requestComplete {
			return Record{}, ErrIncompleteCapture
		}
		body, err := materializeClientResponse(requestBuffer, capture.SpoolThresholdBytes)
		if err != nil {
			return Record{}, err
		}
		attempt.RequestBody = body
	}
	if !success || !attempt.Complete {
		return Record{}, ErrIncompleteCapture
	}
	if len(attempt.RequestBody) == 0 {
		attempt.RequestBody = capture.RequestBody
	}
	if attempt.Path == "" {
		attempt.Path = capture.Path
	}
	if attempt.Model == "" {
		attempt.Model = capture.Model
	}
	if attempt.Route == "" {
		attempt.Route = capture.Route
	}
	capture.RequestBody = attempt.RequestBody
	capture.ResponseBody = attempt.ResponseBody
	responsePath := capture.Path
	requestPath := attempt.Path
	if requestPath == "" {
		requestPath = capture.Path
	}
	capture.Path = requestPath
	capture.ContentType = attempt.ContentType
	capture.Model = attempt.Model
	capture.Route = attempt.Route
	return normalizeProtocols(capture, requestPath, responsePath)
}

type captureRequestReadCloser struct {
	io.ReadCloser
	buffer   *SegmentedBuffer
	expected int64
	onDone   func(bool)

	mu   sync.Mutex
	read int64
	done bool
}

func (r *captureRequestReadCloser) Read(p []byte) (int, error) {
	n, err := r.ReadCloser.Read(p)
	if n > 0 {
		// Capture failure is retained in the buffer but never replaces the real
		// request-body read result seen by net/http.
		_ = r.buffer.TryAppend(p[:n])
		r.mu.Lock()
		r.read += int64(n)
		r.mu.Unlock()
	}
	if err == io.EOF {
		r.finish(true)
	}
	return n, err
}

func (r *captureRequestReadCloser) Close() error {
	r.mu.Lock()
	complete := r.expected >= 0 && r.read >= r.expected
	r.mu.Unlock()
	r.finish(complete)
	return r.ReadCloser.Close()
}

func (r *captureRequestReadCloser) finish(complete bool) {
	r.mu.Lock()
	if r.done {
		r.mu.Unlock()
		return
	}
	r.done = true
	r.mu.Unlock()
	if r.onDone != nil {
		r.onDone(complete)
	}
}

func materializeClientResponse(buffer *SegmentedBuffer, spoolThreshold int64) ([]byte, error) {
	if buffer == nil || spoolThreshold <= 0 || buffer.Len() < spoolThreshold {
		return buffer.Bytes()
	}
	file, err := os.CreateTemp("", "huichuan-dataset-response-*.spool")
	if err != nil {
		return nil, fmt.Errorf("%w: create: %v", ErrSpoolWriteFailed, err)
	}
	path := file.Name()
	defer os.Remove(path)
	if err := file.Chmod(0o600); err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("%w: chmod: %v", ErrSpoolWriteFailed, err)
	}
	if _, err := buffer.WriteTo(file); err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("%w: write: %v", ErrSpoolWriteFailed, err)
	}
	if err := file.Close(); err != nil {
		return nil, fmt.Errorf("%w: close: %v", ErrSpoolWriteFailed, err)
	}
	buffer.Release()
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("%w: read: %v", ErrSpoolWriteFailed, err)
	}
	return data, nil
}
