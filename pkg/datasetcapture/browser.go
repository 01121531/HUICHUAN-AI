package datasetcapture

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

var ErrCaptureFileNotFound = errors.New("dataset capture file not found")

type CaptureFile struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Size      int64     `json:"size"`
	Modified  time.Time `json:"modified"`
	Node      string    `json:"node"`
	UserKey   string    `json:"user_key,omitempty"`
	TokenKey  string    `json:"token_key,omitempty"`
	UserName  string    `json:"user_name,omitempty"`
	TokenName string    `json:"token_name,omitempty"`
	SessionID string    `json:"session_id,omitempty"`
	path      string
}

type RecordPage struct {
	Records   []json.RawMessage `json:"records"`
	Page      int               `json:"page"`
	PageSize  int               `json:"page_size"`
	HasMore   bool              `json:"has_more"`
	TotalRows int               `json:"total_rows"`
}

type ScannedRecord struct {
	Row  int64
	Raw  json.RawMessage
	Size int64
}

type RecordLocator struct {
	Key    string
	FileID string
	Row    int64
}

type Browser struct {
	pathTemplate string
	node         string
}

func NewBrowser(pathTemplate, node string) *Browser {
	return &Browser{pathTemplate: filepath.Clean(pathTemplate), node: safeNodeName(node)}
}

func (b *Browser) ListFiles() ([]CaptureFile, error) {
	pathMatcher, err := capturePathMatcher(b.pathTemplate, b.node)
	if err != nil {
		return nil, err
	}
	roots, err := b.captureRoots()
	if err != nil {
		return nil, err
	}
	files := make([]CaptureFile, 0)
	seen := map[string]struct{}{}
	for _, root := range roots {
		walkErr := filepath.Walk(root, func(candidate string, info os.FileInfo, walkErr error) error {
			if errors.Is(walkErr, os.ErrNotExist) {
				return nil
			}
			if walkErr != nil {
				return walkErr
			}
			if !info.Mode().IsRegular() || !strings.EqualFold(filepath.Ext(candidate), ".jsonl") {
				return nil
			}
			absolute, absErr := filepath.Abs(filepath.Clean(candidate))
			if absErr != nil {
				return absErr
			}
			userKey, tokenKey, sessionID, partitioned := partitionMetadata(root, absolute, b.node)
			if !partitioned && !pathMatcher.MatchString(absolute) {
				return nil
			}
			if _, exists := seen[absolute]; exists {
				return nil
			}
			seen[absolute] = struct{}{}
			files = append(files, CaptureFile{
				ID: captureFileID(absolute), Name: filepath.Base(absolute), Size: info.Size(),
				Modified: info.ModTime().UTC(), Node: b.node, UserKey: userKey,
				TokenKey: tokenKey, SessionID: sessionID, path: absolute,
			})
			return nil
		})
		if walkErr != nil {
			return nil, walkErr
		}
	}
	sort.Slice(files, func(i, j int) bool {
		return files[i].Modified.After(files[j].Modified)
	})
	return files, nil
}

func (b *Browser) captureRoots() ([]string, error) {
	directoryPattern := filepath.Dir(b.pathTemplate)
	directoryPattern = strings.ReplaceAll(directoryPattern, "{date}", "*")
	directoryPattern = strings.ReplaceAll(directoryPattern, "{node}", b.node)
	roots, err := filepath.Glob(directoryPattern)
	if err != nil {
		return nil, err
	}
	if len(roots) == 0 && !strings.ContainsAny(directoryPattern, "*?[") {
		roots = []string{directoryPattern}
	}
	result := make([]string, 0, len(roots))
	for _, root := range roots {
		absolute, absErr := filepath.Abs(filepath.Clean(root))
		if absErr != nil {
			return nil, absErr
		}
		result = append(result, absolute)
	}
	return result, nil
}

func partitionMetadata(root, candidate, node string) (string, string, string, bool) {
	relative, err := filepath.Rel(root, candidate)
	if err != nil || relative == "." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", "", "", false
	}
	parts := strings.Split(filepath.ToSlash(relative), "/")
	if len(parts) != 4 || parts[0] != "node-"+node || !strings.HasPrefix(parts[1], "user-") ||
		!strings.HasPrefix(parts[2], "token-") || !strings.HasPrefix(parts[3], "session-") ||
		!strings.HasSuffix(parts[3], ".jsonl") {
		return "", "", "", false
	}
	userKey := strings.TrimPrefix(parts[1], "user-")
	tokenKey := strings.TrimPrefix(parts[2], "token-")
	sessionID := strings.TrimSuffix(strings.TrimPrefix(parts[3], "session-"), ".jsonl")
	if safeScopeName(userKey) != userKey || safeScopeName(tokenKey) != tokenKey || safeSessionName(sessionID) != sessionID {
		return "", "", "", false
	}
	return userKey, tokenKey, sessionID, true
}

func capturePathMatcher(pathTemplate, node string) (*regexp.Regexp, error) {
	absolute, err := filepath.Abs(filepath.Clean(pathTemplate))
	if err != nil {
		return nil, err
	}
	pattern := regexp.QuoteMeta(absolute)
	pattern = strings.ReplaceAll(pattern, regexp.QuoteMeta("{date}"), `[0-9]{8}`)
	pattern = strings.ReplaceAll(pattern, regexp.QuoteMeta("{node}"), regexp.QuoteMeta(node))
	return regexp.Compile("^" + pattern + "$")
}

func (b *Browser) Resolve(fileID string) (CaptureFile, error) {
	files, err := b.ListFiles()
	if err != nil {
		return CaptureFile{}, err
	}
	for _, file := range files {
		if file.ID == fileID {
			return file, nil
		}
	}
	return CaptureFile{}, ErrCaptureFileNotFound
}

func (b *Browser) Records(fileID string, page, pageSize int) (RecordPage, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 20
	}
	if pageSize > 100 {
		pageSize = 100
	}
	file, err := b.Resolve(fileID)
	if err != nil {
		return RecordPage{}, err
	}
	handle, err := os.Open(file.path)
	if err != nil {
		return RecordPage{}, err
	}
	defer handle.Close()

	result := RecordPage{Records: make([]json.RawMessage, 0, pageSize), Page: page, PageSize: pageSize}
	reader := bufio.NewReader(handle)
	for {
		line, readErr := reader.ReadBytes('\n')
		trimmed := bytes.TrimSpace(line)
		if len(trimmed) > 0 {
			if !json.Valid(trimmed) {
				return RecordPage{}, fmt.Errorf("invalid JSONL record at row %d", result.TotalRows+1)
			}
			result.TotalRows++
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return RecordPage{}, readErr
		}
	}
	startFromNewest := (page - 1) * pageSize
	if startFromNewest >= result.TotalRows {
		return result, nil
	}
	highRow := result.TotalRows - startFromNewest
	lowRow := highRow - pageSize + 1
	if lowRow < 1 {
		lowRow = 1
	}
	result.HasMore = lowRow > 1
	if _, err := handle.Seek(0, io.SeekStart); err != nil {
		return RecordPage{}, err
	}
	reader = bufio.NewReader(handle)
	row := 0
	for {
		line, readErr := reader.ReadBytes('\n')
		trimmed := bytes.TrimSpace(line)
		if len(trimmed) > 0 {
			row++
			if row >= lowRow && row <= highRow {
				result.Records = append(result.Records, append(json.RawMessage(nil), trimmed...))
			}
		}
		if readErr == io.EOF {
			for left, right := 0, len(result.Records)-1; left < right; left, right = left+1, right-1 {
				result.Records[left], result.Records[right] = result.Records[right], result.Records[left]
			}
			return result, nil
		}
		if readErr != nil {
			return RecordPage{}, readErr
		}
	}
}

func (b *Browser) Record(fileID string, targetRow int) ([]byte, CaptureFile, error) {
	if targetRow < 1 {
		return nil, CaptureFile{}, ErrCaptureFileNotFound
	}
	file, err := b.Resolve(fileID)
	if err != nil {
		return nil, CaptureFile{}, err
	}
	handle, err := os.Open(file.path)
	if err != nil {
		return nil, CaptureFile{}, err
	}
	defer handle.Close()
	reader := bufio.NewReader(handle)
	row := 0
	for {
		line, readErr := reader.ReadBytes('\n')
		trimmed := bytes.TrimSpace(line)
		if len(trimmed) > 0 {
			row++
			if !json.Valid(trimmed) {
				return nil, CaptureFile{}, fmt.Errorf("invalid JSONL record at row %d", row)
			}
			if row == targetRow {
				return append(trimmed, '\n'), file, nil
			}
		}
		if readErr == io.EOF {
			return nil, CaptureFile{}, ErrCaptureFileNotFound
		}
		if readErr != nil {
			return nil, CaptureFile{}, readErr
		}
	}
}

func (b *Browser) ScanRecords(fileID string, visit func(ScannedRecord) error) error {
	file, err := b.Resolve(fileID)
	if err != nil {
		return err
	}
	return scanCaptureFile(file, visit)
}

func (b *Browser) ReadRecords(locators []RecordLocator) (map[string]json.RawMessage, error) {
	result := make(map[string]json.RawMessage, len(locators))
	if len(locators) == 0 {
		return result, nil
	}
	files, err := b.ListFiles()
	if err != nil {
		return nil, err
	}
	fileByID := make(map[string]CaptureFile, len(files))
	for _, file := range files {
		fileByID[file.ID] = file
	}
	byFile := make(map[string]map[int64][]string)
	for _, locator := range locators {
		file, exists := fileByID[locator.FileID]
		if !exists || locator.Row < 1 || file.path == "" {
			return nil, ErrCaptureFileNotFound
		}
		if byFile[locator.FileID] == nil {
			byFile[locator.FileID] = make(map[int64][]string)
		}
		byFile[locator.FileID][locator.Row] = append(byFile[locator.FileID][locator.Row], locator.Key)
	}
	for fileID, wantedRows := range byFile {
		if err := scanCaptureFile(fileByID[fileID], func(scanned ScannedRecord) error {
			for _, key := range wantedRows[scanned.Row] {
				result[key] = append(json.RawMessage(nil), scanned.Raw...)
			}
			return nil
		}); err != nil {
			return nil, err
		}
	}
	if len(result) != len(locators) {
		return nil, ErrCaptureFileNotFound
	}
	return result, nil
}

func scanCaptureFile(file CaptureFile, visit func(ScannedRecord) error) error {
	captureFileMu.Lock()
	defer captureFileMu.Unlock()
	handle, err := os.Open(file.path)
	if err != nil {
		return err
	}
	defer handle.Close()
	reader := bufio.NewReader(handle)
	var row int64
	for {
		line, readErr := reader.ReadBytes('\n')
		trimmed := bytes.TrimSpace(line)
		if len(trimmed) > 0 {
			row++
			if !json.Valid(trimmed) {
				return fmt.Errorf("invalid JSONL record at row %d", row)
			}
			if err := visit(ScannedRecord{
				Row:  row,
				Raw:  append(json.RawMessage(nil), trimmed...),
				Size: int64(len(line)),
			}); err != nil {
				return err
			}
		}
		if readErr == io.EOF {
			return nil
		}
		if readErr != nil {
			return readErr
		}
	}
}

func (b *Browser) Open(fileID string) (*os.File, CaptureFile, error) {
	file, err := b.Resolve(fileID)
	if err != nil {
		return nil, CaptureFile{}, err
	}
	handle, err := os.Open(file.path)
	if err != nil {
		return nil, CaptureFile{}, err
	}
	return handle, file, nil
}

func (b *Browser) Delete(fileID string) (CaptureFile, error) {
	return b.DeleteWithCallback(fileID, nil)
}

func (b *Browser) DeleteWithCallback(fileID string, afterDelete func(CaptureFile) error) (CaptureFile, error) {
	file, err := b.Resolve(fileID)
	if err != nil {
		return CaptureFile{}, err
	}
	captureFileMu.Lock()
	defer captureFileMu.Unlock()
	info, err := os.Lstat(file.path)
	if errors.Is(err, os.ErrNotExist) {
		return CaptureFile{}, ErrCaptureFileNotFound
	}
	if err != nil {
		return CaptureFile{}, err
	}
	if !info.Mode().IsRegular() || captureFileID(file.path) != fileID {
		return CaptureFile{}, ErrCaptureFileNotFound
	}
	if err := os.Remove(file.path); err != nil {
		return CaptureFile{}, err
	}
	if afterDelete != nil {
		if err := afterDelete(file); err != nil {
			return CaptureFile{}, err
		}
	}
	return file, nil
}

func captureFileID(path string) string {
	digest := sha256.Sum256([]byte(filepath.Clean(path)))
	return hex.EncodeToString(digest[:12])
}
