package controller

import (
	"encoding/base64"
	"errors"
	"mime"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/01121531/HUICHUAN-AI/common"
	"github.com/gin-gonic/gin"
)

const (
	nervAssetTextReadLimit  = 512 * 1024
	nervAssetImageReadLimit = 3 * 1024 * 1024
)

var nervAssetAllowedRootDirs = map[string]bool{
	"config":  true,
	"docs":    true,
	"images":  true,
	"scripts": true,
	"skills":  true,
	"tools":   true,
}

var nervAssetAllowedRootFiles = map[string]bool{
	"README.md":        true,
	"README_EN.md":     true,
	"bridge.md":        true,
	"deploy.py":        true,
	"direct_setup.py":  true,
	"mcp_server.py":    true,
	"proxy_relay.py":   true,
	"requirements.txt": true,
	"verify.py":        true,
}

type nervAssetItem struct {
	Path        string `json:"path"`
	Name        string `json:"name"`
	Kind        string `json:"kind"`
	Size        int64  `json:"size"`
	ModifiedAt  int64  `json:"modified_at"`
	Previewable bool   `json:"previewable"`
}

type nervAssetCatalogResponse struct {
	BasePath string          `json:"base_path"`
	Count    int             `json:"count"`
	Items    []nervAssetItem `json:"items"`
}

type nervAssetFileResponse struct {
	Path          string `json:"path"`
	Name          string `json:"name"`
	Kind          string `json:"kind"`
	Size          int64  `json:"size"`
	ContentType   string `json:"content_type"`
	Text          string `json:"text,omitempty"`
	ContentBase64 string `json:"content_base64,omitempty"`
	Truncated     bool   `json:"truncated"`
}

func GetNERVAssets(c *gin.Context) {
	basePath, exists, _ := findNERVAssetBasePath()
	if !exists {
		common.ApiErrorMsg(c, "NERV 内置资产目录未找到")
		return
	}
	items, err := listNERVAssetItems(basePath)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, nervAssetCatalogResponse{
		BasePath: basePath,
		Count:    len(items),
		Items:    items,
	})
}

func GetNERVAssetFile(c *gin.Context) {
	basePath, exists, _ := findNERVAssetBasePath()
	if !exists {
		common.ApiErrorMsg(c, "NERV 内置资产目录未找到")
		return
	}
	relativePath, fullPath, err := resolveNERVAssetPath(basePath, c.Query("path"))
	if err != nil {
		common.ApiErrorMsg(c, err.Error())
		return
	}
	info, err := os.Stat(fullPath)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if info.IsDir() {
		common.ApiErrorMsg(c, "请选择具体文件")
		return
	}
	data, err := os.ReadFile(fullPath)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	truncated := false
	contentType := detectNERVAssetContentType(relativePath)
	if isNERVImageAsset(relativePath) {
		if len(data) > nervAssetImageReadLimit {
			common.ApiErrorMsg(c, "图片超过预览上限，请通过服务器文件路径查看")
			return
		}
	} else if len(data) > nervAssetTextReadLimit {
		data = data[:nervAssetTextReadLimit]
		truncated = true
	}
	response := nervAssetFileResponse{
		Path:        relativePath,
		Name:        filepath.Base(relativePath),
		Kind:        classifyNERVAssetKind(relativePath),
		Size:        info.Size(),
		ContentType: contentType,
		Truncated:   truncated,
	}
	if isNERVTextAsset(relativePath) {
		response.Text = string(data)
	} else {
		response.ContentBase64 = base64.StdEncoding.EncodeToString(data)
	}
	common.ApiSuccess(c, response)
}

func listNERVAssetItems(basePath string) ([]nervAssetItem, error) {
	items := make([]nervAssetItem, 0, 128)
	err := filepath.WalkDir(basePath, func(path string, d os.DirEntry, err error) error {
		if err != nil || d == nil {
			return err
		}
		if d.IsDir() {
			if path == basePath {
				return nil
			}
			rel, relErr := filepath.Rel(basePath, path)
			if relErr != nil {
				return relErr
			}
			root := strings.Split(filepath.ToSlash(rel), "/")[0]
			if !nervAssetAllowedRootDirs[root] {
				return filepath.SkipDir
			}
			return nil
		}
		rel, relErr := filepath.Rel(basePath, path)
		if relErr != nil {
			return relErr
		}
		rel = filepath.ToSlash(rel)
		if !isAllowedNERVAssetRelativePath(rel) {
			return nil
		}
		info, infoErr := d.Info()
		if infoErr != nil {
			return infoErr
		}
		items = append(items, nervAssetItem{
			Path:        rel,
			Name:        d.Name(),
			Kind:        classifyNERVAssetKind(rel),
			Size:        info.Size(),
			ModifiedAt:  info.ModTime().Unix(),
			Previewable: isNERVTextAsset(rel) || isNERVImageAsset(rel),
		})
		return nil
	})
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].Kind == items[j].Kind {
			return items[i].Path < items[j].Path
		}
		return items[i].Kind < items[j].Kind
	})
	return items, err
}

func resolveNERVAssetPath(basePath string, requested string) (string, string, error) {
	relativePath := filepath.ToSlash(filepath.Clean("/" + strings.TrimSpace(requested)))
	relativePath = strings.TrimPrefix(relativePath, "/")
	if relativePath == "." || relativePath == "" {
		return "", "", errors.New("缺少资产路径")
	}
	if !isAllowedNERVAssetRelativePath(relativePath) {
		return "", "", errors.New("资产路径不在允许范围内")
	}
	fullPath := filepath.Join(basePath, filepath.FromSlash(relativePath))
	absBase, err := filepath.Abs(basePath)
	if err != nil {
		return "", "", err
	}
	absFull, err := filepath.Abs(fullPath)
	if err != nil {
		return "", "", err
	}
	if absFull != absBase && !strings.HasPrefix(absFull, absBase+string(filepath.Separator)) {
		return "", "", errors.New("资产路径越界")
	}
	return relativePath, absFull, nil
}

func isAllowedNERVAssetRelativePath(relativePath string) bool {
	relativePath = filepath.ToSlash(strings.TrimSpace(relativePath))
	if relativePath == "" || strings.HasPrefix(relativePath, "../") || strings.Contains(relativePath, "/../") {
		return false
	}
	if nervAssetAllowedRootFiles[relativePath] {
		return true
	}
	root := strings.Split(relativePath, "/")[0]
	return nervAssetAllowedRootDirs[root]
}

func classifyNERVAssetKind(relativePath string) string {
	relativePath = filepath.ToSlash(relativePath)
	root := strings.Split(relativePath, "/")[0]
	switch root {
	case "docs":
		return "文档"
	case "images":
		return "图片"
	case "scripts":
		return "脚本"
	case "skills":
		return "技能"
	case "tools":
		return "工具"
	case "config":
		return "配置"
	}
	switch strings.ToLower(filepath.Ext(relativePath)) {
	case ".md", ".txt":
		return "文档"
	case ".py", ".bat", ".ps1", ".sh":
		return "脚本"
	case ".json", ".toml", ".yml", ".yaml":
		return "配置"
	default:
		return "资产"
	}
}

func isNERVTextAsset(relativePath string) bool {
	switch strings.ToLower(filepath.Ext(relativePath)) {
	case ".md", ".txt", ".py", ".json", ".bat", ".ps1", ".sh", ".toml", ".yml", ".yaml":
		return true
	default:
		return false
	}
}

func isNERVImageAsset(relativePath string) bool {
	switch strings.ToLower(filepath.Ext(relativePath)) {
	case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".svg":
		return true
	default:
		return false
	}
}

func detectNERVAssetContentType(relativePath string) string {
	if isNERVTextAsset(relativePath) {
		return "text/plain; charset=utf-8"
	}
	ext := strings.ToLower(filepath.Ext(relativePath))
	if value := mime.TypeByExtension(ext); value != "" {
		return value
	}
	return "application/octet-stream"
}

func init() {
	// 确保少数系统上 MIME 表不完整时，图片预览仍能拿到常用类型。
	_ = mime.AddExtensionType(".svg", "image/svg+xml")
	_ = mime.AddExtensionType(".webp", "image/webp")
}
