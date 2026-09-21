package openai

import (
	"crypto/sha1"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/Elysia-SHY/one-api-plus/common/logger"
)

// oneAPI 的 tiktoken-go 默认 BPE 加载器用无超时的 http.Get 直连
// openaipublic.blob.core.windows.net，在国内网络下极慢甚至永久阻塞，
// 导致进程在 "initializing token encoders" 处卡死、服务起不来。
//
// 这里用一个带超时 + 本地缓存 + 可配置镜像源 的加载器替换它：
//   - 命中缓存（TIKTOKEN_CACHE_DIR / DATA_GYM_CACHE_DIR / 默认 temp）直接读本地；
//   - 未命中则按 TIKTOKEN_DOWNLOAD_TIMEOUT（秒，默认 30）超时下载，超时即失败；
//   - 可用 TIKTOKEN_BPE_BASE_URL 指向镜像源，规避直连不可达。
type timeoutBpeLoader struct {
	client  *http.Client
	baseURL string
}

func cacheDir() string {
	if v := os.Getenv("TIKTOKEN_CACHE_DIR"); v != "" {
		return v
	}
	if v := os.Getenv("DATA_GYM_CACHE_DIR"); v != "" {
		return v
	}
	return filepath.Join(os.TempDir(), "data-gym-cache")
}

func (l *timeoutBpeLoader) cachePath(blobpath string) string {
	dir := cacheDir()
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, fmt.Sprintf("%x", sha1.Sum([]byte(blobpath))))
}

func (l *timeoutBpeLoader) readLocal(blobpath string) ([]byte, bool) {
	cp := l.cachePath(blobpath)
	if cp == "" {
		return nil, false
	}
	b, err := os.ReadFile(cp)
	if err != nil {
		return nil, false
	}
	return b, true
}

func (l *timeoutBpeLoader) download(blobpath string) ([]byte, error) {
	url := blobpath
	if l.baseURL != "" {
		// 保留原始路径（encodings/xxx.tiktoken），把 host 换成镜像
		if idx := strings.Index(blobpath, "://"); idx >= 0 {
			if slash := strings.Index(blobpath[idx+3:], "/"); slash >= 0 {
				url = strings.TrimSuffix(l.baseURL, "/") + blobpath[idx+3+slash:]
			}
		}
	}
	resp, err := l.client.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("下载 BPE 词表返回状态码 %d", resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
}

// LoadTiktokenBpe 实现 tiktoken.BpeLoader
func (l *timeoutBpeLoader) LoadTiktokenBpe(tiktokenBpeFile string) (map[string]int, error) {
	var contents []byte
	var err error
	if b, ok := l.readLocal(tiktokenBpeFile); ok {
		contents = b
	} else {
		contents, err = l.download(tiktokenBpeFile)
		if err != nil {
			return nil, err
		}
		// 写缓存，下次启动直接用
		if cp := l.cachePath(tiktokenBpeFile); cp != "" {
			if mkErr := os.MkdirAll(filepath.Dir(cp), os.ModePerm); mkErr == nil {
				_ = os.WriteFile(cp, contents, os.ModePerm)
			}
		}
	}
	ranks := make(map[string]int)
	for _, line := range strings.Split(string(contents), "\n") {
		if line == "" {
			continue
		}
		parts := strings.Split(line, " ")
		if len(parts) != 2 {
			continue
		}
		token, decErr := base64.StdEncoding.DecodeString(parts[0])
		if decErr != nil {
			return nil, decErr
		}
		rank, atoiErr := strconv.Atoi(parts[1])
		if atoiErr != nil {
			return nil, atoiErr
		}
		ranks[string(token)] = rank
	}
	return ranks, nil
}

// newTimeoutBpeLoader 根据环境变量构造加载器
func newTimeoutBpeLoader() *timeoutBpeLoader {
	timeout := 30
	if v := os.Getenv("TIKTOKEN_DOWNLOAD_TIMEOUT"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			timeout = n
		}
	}
	base := os.Getenv("TIKTOKEN_BPE_BASE_URL")
	if base != "" {
		logger.SysLog("tiktoken BPE 使用镜像源: " + base)
	}
	l := &timeoutBpeLoader{
		client:  &http.Client{Timeout: time.Duration(timeout) * time.Second},
		baseURL: base,
	}
	if cp := l.cachePath("https://openaipublic.blob.core.windows.net/encodings/cl100k_base.tiktoken"); cp != "" {
		if _, err := os.Stat(cp); err == nil {
			logger.SysLog("tiktoken BPE 命中本地缓存: " + cp)
		}
	}
	return l
}
