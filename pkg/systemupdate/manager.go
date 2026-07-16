package systemupdate

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	maxReleaseResponseBytes = 4 << 20
	maxChecksumBytes        = 1 << 20
	maxBinaryBytes          = 512 << 20
	stateDirectoryName      = ".huichuan-update"
	stateFileName           = "state.json"
)

var (
	managerMu       sync.Mutex
	shutdownRequest = make(chan string, 1)
)

func ShutdownRequests() <-chan string {
	return shutdownRequest
}

func CapabilityStatus() Capability {
	capability := Capability{Platform: runtime.GOOS, Arch: runtime.GOARCH}
	if value := strings.TrimSpace(os.Getenv("SYSTEM_UPDATE_ENABLED")); value != "" {
		enabled, err := strconv.ParseBool(value)
		if err != nil || !enabled {
			capability.Reason = "disabled_by_environment"
			return capability
		}
	}
	if runtime.GOOS != "windows" || runtime.GOARCH != "amd64" {
		capability.Reason = "unsupported_platform"
		return capability
	}
	if strings.TrimSpace(os.Getenv("VERSION")) != "" {
		capability.Reason = "version_overridden_by_environment"
		return capability
	}
	if os.Getenv("KUBERNETES_SERVICE_HOST") != "" || fileExists("/.dockerenv") {
		capability.Reason = "container_managed_externally"
		return capability
	}
	executable, err := os.Executable()
	if err != nil {
		capability.Reason = "executable_path_unavailable"
		return capability
	}
	executable, err = filepath.EvalSymlinks(executable)
	if err != nil {
		capability.Reason = "executable_path_unavailable"
		return capability
	}
	if strings.Contains(strings.ToLower(executable), "go-build") {
		capability.Reason = "development_build"
		return capability
	}
	info, err := os.Stat(executable)
	if err != nil || !info.Mode().IsRegular() {
		capability.Reason = "executable_not_replaceable"
		return capability
	}
	capability.Supported = true
	return capability
}

func CheckLatest(ctx context.Context, currentVersion string) (ReleaseInfo, error) {
	release, err := fetchLatestRelease(ctx)
	if err != nil {
		return ReleaseInfo{}, err
	}
	info := publicReleaseInfo(release, currentVersion)
	if !isSemanticVersion(release.TagName) {
		info.Reason = "release_tag_not_semver"
		return info, nil
	}
	if release.Draft || release.Prerelease {
		info.Reason = "release_not_stable"
		return info, nil
	}
	if !CapabilityStatus().Supported {
		info.Reason = CapabilityStatus().Reason
		return info, nil
	}
	binaryAsset, checksumAsset, err := selectAssets(release)
	if err != nil {
		info.Reason = "matching_asset_missing"
		return info, nil
	}
	if binaryAsset.Size <= 0 || binaryAsset.Size > maxBinaryBytes || checksumAsset.Size <= 0 || checksumAsset.Size > maxChecksumBytes {
		info.Reason = "release_asset_size_invalid"
		return info, nil
	}
	info.AssetName = binaryAsset.Name
	info.Installable = info.UpdateAvailable
	if !info.UpdateAvailable {
		info.Reason = "already_latest"
	}
	return info, nil
}

func GetState() UpdateState {
	managerMu.Lock()
	defer managerMu.Unlock()
	state, err := loadState(statePath())
	if err != nil {
		return UpdateState{Phase: PhaseIdle, ErrorCode: "state_unreadable"}
	}
	return state
}

func BeginUpdate(ctx context.Context, releaseID int64, currentVersion string, healthPort string) (UpdateState, error) {
	capability := CapabilityStatus()
	if !capability.Supported {
		return UpdateState{}, fmt.Errorf("online update is unavailable: %s", capability.Reason)
	}
	if releaseID <= 0 {
		return UpdateState{}, errors.New("release_id is required")
	}

	managerMu.Lock()
	defer managerMu.Unlock()
	previous, err := loadState(statePath())
	if err != nil {
		return UpdateState{}, fmt.Errorf("read update state: %w", err)
	}
	if previous.Active() {
		return previous, errors.New("an update is already in progress")
	}

	release, err := fetchLatestRelease(ctx)
	if err != nil {
		return UpdateState{}, err
	}
	info := publicReleaseInfo(release, currentVersion)
	if release.ID != releaseID {
		return UpdateState{}, errors.New("the selected release is no longer latest")
	}
	if !isSemanticVersion(release.TagName) || release.Draft || release.Prerelease {
		return UpdateState{}, errors.New("the latest release is not an installable stable SemVer release")
	}
	if !info.UpdateAvailable {
		return UpdateState{}, errors.New("the current version is already up to date")
	}
	binaryAsset, checksumAsset, err := selectAssets(release)
	if err != nil {
		return UpdateState{}, err
	}

	taskID, err := newTaskID()
	if err != nil {
		return UpdateState{}, err
	}
	timestamp := now().Unix()
	state := UpdateState{
		TaskID:         taskID,
		Phase:          PhaseDownloading,
		Progress:       1,
		CurrentVersion: currentVersion,
		TargetVersion:  release.TagName,
		ReleaseID:      release.ID,
		MessageCode:    "downloading",
		StartedAt:      timestamp,
		UpdatedAt:      timestamp,
	}
	if err := saveState(statePath(), state); err != nil {
		return UpdateState{}, fmt.Errorf("persist update state: %w", err)
	}

	go prepareUpdate(release, binaryAsset, checksumAsset, state, healthPort)
	return state, nil
}

func prepareUpdate(release githubRelease, binaryAsset githubAsset, checksumAsset githubAsset, state UpdateState, healthPort string) {
	fail := func(code string, err error) {
		state.Phase = PhaseFailed
		state.Progress = 0
		state.ErrorCode = code
		state.MessageCode = "failed"
		state.UpdatedAt = now().Unix()
		state.CompletedAt = state.UpdatedAt
		_ = persistState(state)
		fmt.Fprintf(os.Stderr, "system update %s failed (%s): %v\n", state.TaskID, code, err)
	}

	executable, err := os.Executable()
	if err != nil {
		fail("executable_path_unavailable", err)
		return
	}
	executable, err = filepath.EvalSymlinks(executable)
	if err != nil {
		fail("executable_path_unavailable", err)
		return
	}
	root := filepath.Join(filepath.Dir(executable), stateDirectoryName)
	taskDir := filepath.Join(root, state.TaskID)
	if err := os.MkdirAll(taskDir, 0700); err != nil {
		fail("staging_directory_unavailable", err)
		return
	}

	checksumPath := filepath.Join(taskDir, "checksums-windows.txt")
	if _, err := downloadAsset(context.Background(), checksumAsset, checksumPath, maxChecksumBytes, nil); err != nil {
		fail("checksum_download_failed", err)
		return
	}
	expectedHash, err := checksumForFile(checksumPath, binaryAsset.Name)
	if err != nil {
		fail("checksum_entry_missing", err)
		return
	}

	stagedPath := filepath.Join(taskDir, "new-version.exe")
	progress := func(downloaded, total int64) {
		if total <= 0 {
			total = binaryAsset.Size
		}
		percent := 5
		if total > 0 {
			percent = 5 + int(float64(downloaded)/float64(total)*65)
		}
		if percent > 70 {
			percent = 70
		}
		if percent >= state.Progress+2 {
			state.Progress = percent
			state.UpdatedAt = now().Unix()
			_ = persistState(state)
		}
	}
	actualHash, err := downloadAsset(context.Background(), binaryAsset, stagedPath, maxBinaryBytes, progress)
	if err != nil {
		fail("binary_download_failed", err)
		return
	}

	state.Phase = PhaseVerifying
	state.Progress = 78
	state.MessageCode = "verifying"
	state.UpdatedAt = now().Unix()
	_ = persistState(state)
	if !strings.EqualFold(actualHash, expectedHash) {
		fail("checksum_mismatch", fmt.Errorf("expected %s, got %s", expectedHash, actualHash))
		return
	}
	if binaryAsset.Digest != "" {
		digest := strings.TrimPrefix(strings.ToLower(binaryAsset.Digest), "sha256:")
		if len(digest) == sha256.Size*2 && !strings.EqualFold(actualHash, digest) {
			fail("github_digest_mismatch", errors.New("GitHub asset digest does not match downloaded binary"))
			return
		}
	}

	// Avoid installer-like filenames (update/setup/install) because Windows
	// application compatibility heuristics may require elevation for them.
	helperPath := filepath.Join(taskDir, "huichuan-swap.exe")
	if err := copyFile(executable, helperPath, 0700); err != nil {
		fail("helper_prepare_failed", err)
		return
	}
	workingDir, err := os.Getwd()
	if err != nil {
		fail("working_directory_unavailable", err)
		return
	}
	if healthPort == "" {
		healthPort = "3000"
	}
	plan := updatePlan{
		TaskID:               state.TaskID,
		ParentPID:            os.Getpid(),
		TargetPath:           executable,
		StagedPath:           stagedPath,
		BackupPath:           filepath.Join(taskDir, "previous-version.exe"),
		StatePath:            statePath(),
		ReadyPath:            filepath.Join(taskDir, "helper.ready"),
		WorkingDir:           workingDir,
		Args:                 append([]string(nil), os.Args[1:]...),
		HealthURL:            "http://127.0.0.1:" + healthPort + "/api/status",
		HealthTimeoutSeconds: 120,
		CurrentVersion:       state.CurrentVersion,
		TargetVersion:        release.TagName,
		ReleaseID:            release.ID,
		StartedAt:            state.StartedAt,
	}
	planPath := filepath.Join(taskDir, "plan.json")
	if err := writeJSONAtomic(planPath, plan, 0600); err != nil {
		fail("plan_write_failed", err)
		return
	}

	state.Phase = PhaseStaged
	state.Progress = 90
	state.MessageCode = "staged"
	state.RestartRequired = true
	state.UpdatedAt = now().Unix()
	if err := persistState(state); err != nil {
		fail("state_write_failed", err)
		return
	}

	command := exec.Command(helperPath, helperCommand, planPath)
	command.Dir = workingDir
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	if err := command.Start(); err != nil {
		fail("helper_start_failed", err)
		return
	}
	readyDeadline := now().Add(10 * time.Second)
	for !fileExists(plan.ReadyPath) && now().Before(readyDeadline) {
		time.Sleep(100 * time.Millisecond)
	}
	if !fileExists(plan.ReadyPath) {
		_ = command.Process.Kill()
		_, _ = command.Process.Wait()
		fail("helper_ready_timeout", errors.New("update helper did not acknowledge the plan"))
		return
	}
	state.Phase = PhaseRestarting
	state.Progress = 94
	state.MessageCode = "restarting"
	state.UpdatedAt = now().Unix()
	_ = persistState(state)
	select {
	case shutdownRequest <- "online update " + state.TaskID:
	default:
	}
}

func fetchLatestRelease(ctx context.Context) (githubRelease, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, LatestReleaseURL, nil)
	if err != nil {
		return githubRelease{}, err
	}
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	request.Header.Set("User-Agent", "HUICHUAN-AI-system-updater")
	if token := strings.TrimSpace(os.Getenv("SYSTEM_UPDATE_GITHUB_TOKEN")); token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	client := restrictedHTTPClient(20 * time.Second)
	response, err := client.Do(request)
	if err != nil {
		return githubRelease{}, fmt.Errorf("contact GitHub releases API: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusNotFound {
		return githubRelease{}, errors.New("no GitHub release has been published")
	}
	if response.StatusCode != http.StatusOK {
		return githubRelease{}, fmt.Errorf("GitHub releases API returned status %d", response.StatusCode)
	}
	var release githubRelease
	decoder := json.NewDecoder(io.LimitReader(response.Body, maxReleaseResponseBytes))
	if err := decoder.Decode(&release); err != nil {
		return githubRelease{}, fmt.Errorf("decode GitHub release: %w", err)
	}
	if release.ID <= 0 || release.TagName == "" {
		return githubRelease{}, errors.New("GitHub release payload is incomplete")
	}
	return release, nil
}

func publicReleaseInfo(release githubRelease, currentVersion string) ReleaseInfo {
	updateAvailable := compareVersions(release.TagName, currentVersion) > 0
	if !isSemanticVersion(release.TagName) {
		updateAvailable = release.TagName != currentVersion
	}
	return ReleaseInfo{
		ID:              release.ID,
		TagName:         release.TagName,
		Name:            release.Name,
		Body:            release.Body,
		HTMLURL:         release.HTMLURL,
		PublishedAt:     release.PublishedAt,
		CurrentVersion:  currentVersion,
		UpdateAvailable: updateAvailable,
	}
}

func selectAssets(release githubRelease) (githubAsset, githubAsset, error) {
	preferredNames := []string{
		fmt.Sprintf("huichuan-ai-%s-windows-amd64.exe", release.TagName),
		fmt.Sprintf("huichuan-%s.exe", release.TagName),
	}
	var binary githubAsset
	var checksum githubAsset
	for _, asset := range release.Assets {
		if asset.Name == "checksums-windows.txt" {
			checksum = asset
		}
		for _, name := range preferredNames {
			if asset.Name == name {
				binary = asset
			}
		}
	}
	if binary.ID == 0 || checksum.ID == 0 {
		return githubAsset{}, githubAsset{}, errors.New("matching Windows release assets are missing")
	}
	return binary, checksum, nil
}

func downloadAsset(ctx context.Context, asset githubAsset, destination string, limit int64, progress func(int64, int64)) (string, error) {
	if asset.Size <= 0 || asset.Size > limit {
		return "", fmt.Errorf("asset %s has invalid size %d", asset.Name, asset.Size)
	}
	if err := validateDownloadURL(asset.BrowserDownloadURL); err != nil {
		return "", err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, asset.BrowserDownloadURL, nil)
	if err != nil {
		return "", err
	}
	request.Header.Set("Accept", "application/octet-stream")
	request.Header.Set("User-Agent", "HUICHUAN-AI-system-updater")
	response, err := restrictedHTTPClient(10 * time.Minute).Do(request)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("asset download returned status %d", response.StatusCode)
	}
	if response.ContentLength > limit {
		return "", errors.New("asset exceeds download size limit")
	}

	temporary := destination + ".part"
	file, err := os.OpenFile(temporary, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		return "", err
	}
	hash := sha256.New()
	reader := &progressReader{reader: io.LimitReader(response.Body, limit+1), total: asset.Size, callback: progress}
	written, copyErr := io.Copy(io.MultiWriter(file, hash), reader)
	closeErr := file.Close()
	if copyErr != nil {
		_ = os.Remove(temporary)
		return "", copyErr
	}
	if closeErr != nil {
		_ = os.Remove(temporary)
		return "", closeErr
	}
	if written > limit || written != asset.Size {
		_ = os.Remove(temporary)
		return "", fmt.Errorf("asset size mismatch: expected %d, got %d", asset.Size, written)
	}
	if err := os.Rename(temporary, destination); err != nil {
		_ = os.Remove(temporary)
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

type progressReader struct {
	reader   io.Reader
	total    int64
	read     int64
	callback func(int64, int64)
}

func (reader *progressReader) Read(buffer []byte) (int, error) {
	n, err := reader.reader.Read(buffer)
	reader.read += int64(n)
	if reader.callback != nil {
		reader.callback(reader.read, reader.total)
	}
	return n, err
}

func checksumForFile(path string, filename string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	scanner := bufio.NewScanner(io.LimitReader(file, maxChecksumBytes))
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 2 {
			continue
		}
		listedName := strings.TrimPrefix(fields[len(fields)-1], "*")
		if filepath.Base(listedName) == filename && len(fields[0]) == sha256.Size*2 {
			if _, err := hex.DecodeString(fields[0]); err == nil {
				return strings.ToLower(fields[0]), nil
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return "", err
	}
	return "", fmt.Errorf("checksum for %s not found", filename)
}

func restrictedHTTPClient(timeout time.Duration) *http.Client {
	return &http.Client{
		Timeout: timeout,
		CheckRedirect: func(request *http.Request, via []*http.Request) error {
			if len(via) >= 8 {
				return errors.New("too many redirects")
			}
			return validateDownloadURL(request.URL.String())
		},
	}
}

func validateDownloadURL(rawURL string) error {
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Scheme != "https" || parsed.User != nil {
		return errors.New("release asset URL is invalid")
	}
	host := strings.ToLower(parsed.Hostname())
	allowed := host == "github.com" || host == "api.github.com" || host == "objects.githubusercontent.com" || strings.HasSuffix(host, ".githubusercontent.com") || strings.HasSuffix(host, ".githubassets.com")
	if !allowed {
		return fmt.Errorf("release asset host %q is not allowed", host)
	}
	return nil
}

func persistState(state UpdateState) error {
	managerMu.Lock()
	defer managerMu.Unlock()
	return saveState(statePath(), state)
}

func loadState(path string) (UpdateState, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return UpdateState{Phase: PhaseIdle}, nil
	}
	if err != nil {
		return UpdateState{}, err
	}
	var state UpdateState
	if err := json.Unmarshal(data, &state); err != nil {
		return UpdateState{}, err
	}
	if state.Phase == "" {
		state.Phase = PhaseIdle
	}
	return state, nil
}

func saveState(path string, state UpdateState) error {
	if state.Progress < 0 {
		state.Progress = 0
	}
	if state.Progress > 100 {
		state.Progress = 100
	}
	return writeJSONAtomic(path, state, 0600)
}

func writeJSONAtomic(path string, value any, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	temporary := path + ".tmp"
	if err := os.WriteFile(temporary, append(data, '\n'), mode); err != nil {
		return err
	}
	if err := replaceFile(temporary, path); err != nil {
		_ = os.Remove(temporary)
		return err
	}
	return nil
}

func statePath() string {
	executable, err := os.Executable()
	if err != nil {
		return filepath.Join(".", stateDirectoryName, stateFileName)
	}
	executable, err = filepath.EvalSymlinks(executable)
	if err != nil {
		return filepath.Join(filepath.Dir(executable), stateDirectoryName, stateFileName)
	}
	return filepath.Join(filepath.Dir(executable), stateDirectoryName, stateFileName)
}

func copyFile(source string, destination string, mode os.FileMode) error {
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	output, err := os.OpenFile(destination, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(output, input)
	closeErr := output.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}

func newTaskID() (string, error) {
	buffer := make([]byte, 16)
	if _, err := rand.Read(buffer); err != nil {
		return "", err
	}
	return "update_" + hex.EncodeToString(buffer), nil
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
