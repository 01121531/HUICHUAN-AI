package datasetcapture

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
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

	capture Capture
	attempt Attempt
	success bool
}

func NewSession(capture Capture) *Session {
	if capture.CreatedAt.IsZero() {
		capture.CreatedAt = time.Now()
	}
	if capture.SessionSource == "" {
		capture.SessionSource = sessionSourceFromBody(capture.RequestBody)
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
	s.attempt = Attempt{Model: model, Route: route}
	s.success = false
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
	s.attempt = Attempt{}
	s.success = false
	s.mu.Unlock()
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
	body, err := io.ReadAll(req.Body)
	if err != nil {
		return err
	}
	_ = req.Body.Close()
	req.Body = io.NopCloser(bytes.NewReader(body))

	s.mu.Lock()
	s.attempt.RequestBody = append([]byte(nil), body...)
	s.attempt.Path = req.URL.Path
	s.attempt.ContentType = req.Header.Get("Content-Type")
	s.mu.Unlock()
	return nil
}

func (s *Session) WrapUpstreamResponse(resp *http.Response) {
	if s == nil || resp == nil || resp.Body == nil {
		return
	}
	resp.Body = &captureReadCloser{
		ReadCloser: resp.Body,
		onDone: func(body []byte, complete bool) {
			s.mu.Lock()
			s.attempt.ResponseBody = body
			s.attempt.Complete = complete
			s.mu.Unlock()
		},
	}
}

func (s *Session) SetClientResponse(body []byte, complete bool) {
	if s == nil {
		return
	}
	s.mu.Lock()
	if len(s.attempt.ResponseBody) == 0 || !s.attempt.Complete {
		s.attempt.ResponseBody = append([]byte(nil), body...)
		s.attempt.Complete = complete
		s.attempt.RequestBody = append([]byte(nil), s.capture.RequestBody...)
		s.attempt.Path = s.capture.Path
		s.attempt.ContentType = s.capture.ContentType
	}
	s.mu.Unlock()
}

func (s *Session) BuildRecord() (Record, error) {
	s.mu.Lock()
	capture := s.capture
	attempt := s.attempt
	success := s.success
	s.mu.Unlock()
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
	capture.Path = attempt.Path
	capture.ContentType = attempt.ContentType
	capture.Model = attempt.Model
	capture.Route = attempt.Route
	return Normalize(capture)
}

type captureReadCloser struct {
	io.ReadCloser
	mu       sync.Mutex
	buffer   bytes.Buffer
	complete bool
	done     bool
	onDone   func([]byte, bool)
}

func (r *captureReadCloser) Read(p []byte) (int, error) {
	n, err := r.ReadCloser.Read(p)
	if n > 0 {
		r.mu.Lock()
		_, _ = r.buffer.Write(p[:n])
		r.mu.Unlock()
	}
	if err == io.EOF {
		r.finish(true)
	}
	return n, err
}

func (r *captureReadCloser) Close() error {
	r.mu.Lock()
	complete := r.complete || payloadLooksComplete(r.buffer.Bytes())
	r.mu.Unlock()
	r.finish(complete)
	return r.ReadCloser.Close()
}

func (r *captureReadCloser) finish(complete bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.done {
		return
	}
	r.done = true
	r.complete = complete
	body := append([]byte(nil), r.buffer.Bytes()...)
	if r.onDone != nil {
		r.onDone(body, complete)
	}
}

func payloadLooksComplete(body []byte) bool {
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) == 0 {
		return false
	}
	if bytes.HasPrefix(trimmed, []byte("{")) || bytes.HasPrefix(trimmed, []byte("[")) {
		return json.Valid(trimmed)
	}
	compact := bytes.ReplaceAll(trimmed, []byte(" "), nil)
	return bytes.Contains(compact, []byte("data:[DONE]")) ||
		bytes.Contains(compact, []byte(`"type":"message_stop"`)) ||
		bytes.Contains(compact, []byte(`"type":"response.completed"`)) ||
		bytes.Contains(compact, []byte(`"type":"response.done"`)) ||
		bytes.Contains(compact, []byte(`"finishReason":`))
}
