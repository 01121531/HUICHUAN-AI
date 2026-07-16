package systemupdate

import (
	"crypto/sha256"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCompareVersions(t *testing.T) {
	tests := []struct {
		left  string
		right string
		want  int
	}{
		{"v1.0.1", "v1.0.0", 1},
		{"v1.0.0", "v1.0.0", 0},
		{"v1.2.0", "v2.0.0", -1},
		{"v1.0.0", "v1.0.0-rc.1", 1},
		{"v1.0.0", "development", 1},
		{"api", "v1.0.0", 0},
	}
	for _, test := range tests {
		t.Run(test.left+"_"+test.right, func(t *testing.T) {
			require.Equal(t, test.want, compareVersions(test.left, test.right))
		})
	}
	require.True(t, isSemanticVersion("v12.34.56"))
	require.False(t, isSemanticVersion("api"))
	require.False(t, isSemanticVersion("v01.2.3"))
}

func TestSelectAssets(t *testing.T) {
	release := githubRelease{
		TagName: "v1.2.3",
		Assets: []githubAsset{
			{ID: 1, Name: "checksums-windows.txt"},
			{ID: 2, Name: "huichuan-v1.2.3.exe"},
			{ID: 3, Name: "huichuan-ai-v1.2.3-windows-amd64.exe"},
		},
	}
	binary, checksum, err := selectAssets(release)
	require.NoError(t, err)
	require.Equal(t, int64(3), binary.ID)
	require.Equal(t, int64(1), checksum.ID)

	release.Assets = release.Assets[:1]
	_, _, err = selectAssets(release)
	require.Error(t, err)
}

func TestPlatformReleaseAssetNames(t *testing.T) {
	require.True(t, platformSupported("windows", "amd64"))
	require.True(t, platformSupported("linux", "amd64"))
	require.True(t, platformSupported("linux", "arm64"))
	require.True(t, platformSupported("darwin", "amd64"))
	require.True(t, platformSupported("darwin", "arm64"))
	require.False(t, platformSupported("windows", "arm64"))
	require.False(t, platformSupported("freebsd", "amd64"))

	require.Equal(t, "windows", releaseArtifactPlatform("windows"))
	require.Equal(t, "linux", releaseArtifactPlatform("linux"))
	require.Equal(t, "macos", releaseArtifactPlatform("darwin"))
	require.Equal(t, "huichuan-ai-v1.2.3-windows-amd64.exe", releaseBinaryName("v1.2.3", "windows", "amd64"))
	require.Equal(t, "huichuan-ai-v1.2.3-linux-arm64", releaseBinaryName("v1.2.3", "linux", "arm64"))
	require.Equal(t, "huichuan-ai-v1.2.3-macos-arm64", releaseBinaryName("v1.2.3", "darwin", "arm64"))
}

func TestPublicReleaseInfoShowsInvalidDifferentTag(t *testing.T) {
	info := publicReleaseInfo(githubRelease{ID: 1, TagName: "api"}, "v1.0.0")
	require.True(t, info.UpdateAvailable)
	require.False(t, info.Installable)
}

func TestChecksumForFile(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "checksums-windows.txt")
	hash := strings.Repeat("a", sha256.Size*2)
	require.NoError(t, os.WriteFile(path, []byte(hash+"  huichuan-ai-v1.0.0-windows-amd64.exe\n"), 0600))
	actual, err := checksumForFile(path, "huichuan-ai-v1.0.0-windows-amd64.exe")
	require.NoError(t, err)
	require.Equal(t, hash, actual)
	_, err = checksumForFile(path, "other.exe")
	require.Error(t, err)
}

func TestSaveStateReplacesExistingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	require.NoError(t, saveState(path, UpdateState{Phase: PhaseDownloading, Progress: 10}))
	require.NoError(t, saveState(path, UpdateState{Phase: PhaseSucceeded, Progress: 100}))
	state, err := loadState(path)
	require.NoError(t, err)
	require.Equal(t, PhaseSucceeded, state.Phase)
	require.Equal(t, 100, state.Progress)
}

func TestValidateDownloadURL(t *testing.T) {
	require.NoError(t, validateDownloadURL("https://github.com/01121531/HUICHUAN-AI/releases/download/v1.0.0/app.exe"))
	require.NoError(t, validateDownloadURL("https://release-assets.githubusercontent.com/example"))
	require.Error(t, validateDownloadURL("http://github.com/file"))
	require.Error(t, validateDownloadURL("https://example.com/file"))
	require.Error(t, validateDownloadURL("https://github.com@example.com/file"))
}

func TestUpdateStateActive(t *testing.T) {
	require.True(t, UpdateState{Phase: PhaseRestarting}.Active())
	require.False(t, UpdateState{Phase: PhaseSucceeded}.Active())
	require.False(t, UpdateState{Phase: PhaseFailed}.Active())
}
