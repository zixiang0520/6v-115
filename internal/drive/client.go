package drive

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"6v-to-2dland/internal/cfg"
	"6v-to-2dland/internal/tmdb"
)

// Client 封装 115 网页接口（Cookie / 扫码，不走开放平台）。
type Client struct {
	mu      sync.RWMutex
	http    *http.Client
	tmdb    *tmdb.Client
	baseDir string
	cookie  string
	tokFile string

	qrUID    string
	qrTime   int64
	qrSign   string
	qrExpire int64 // unix

	cidCache map[string]string // path -> cid
}

type snapshot struct {
	tmdb    *tmdb.Client
	baseDir string
	cookie  string
}

func (c *Client) snap() snapshot {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return snapshot{tmdb: c.tmdb, baseDir: c.baseDir, cookie: c.cookie}
}

// New 创建 115 客户端，Cookie 从 tokenFile 加载。
func New(cfg *cfg.Config) *Client {
	cl := &Client{
		http:     newHTTP(),
		baseDir:  cfg.BaseDir,
		tokFile:  cfg.TokenFile,
		cidCache: map[string]string{"/": "0"},
	}
	if cfg.TmdbAPIKey != "" {
		cl.tmdb = tmdb.New(cfg.TmdbAPIKey, cfg.TmdbProxy, cfg.TmdbLang)
	}
	if ck := strings.TrimSpace(cfg.Cookie); ck != "" {
		cl.cookie = normalizeCookie(ck)
	} else if saved := loadCookie(cfg.TokenFile); saved != "" {
		cl.cookie = saved
	}
	return cl
}

type tokenDisk struct {
	Cookie string `json:"cookie"`
}

func loadCookie(path string) string {
	if path == "" {
		return ""
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	var t tokenDisk
	if json.Unmarshal(b, &t) == nil && strings.TrimSpace(t.Cookie) != "" {
		return normalizeCookie(t.Cookie)
	}
	// 兼容纯文本 cookie
	s := strings.TrimSpace(string(b))
	if strings.Contains(strings.ToUpper(s), "UID=") {
		return normalizeCookie(s)
	}
	return ""
}

func (c *Client) saveCookie(cookie string) {
	cookie = normalizeCookie(cookie)
	c.mu.Lock()
	c.cookie = cookie
	c.cidCache = map[string]string{"/": "0"}
	path := c.tokFile
	c.mu.Unlock()
	if path == "" {
		return
	}
	_ = os.MkdirAll(filepath.Dir(path), 0o700)
	if filepath.Dir(path) == "." {
		// token.json 在 cwd
	}
	b, _ := json.MarshalIndent(tokenDisk{Cookie: cookie}, "", "  ")
	_ = os.WriteFile(path, b, 0o600)
}

// UpdateTMDB 热更新 TMDB。
func (c *Client) UpdateTMDB(apiKey, proxy, lang string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if apiKey == "" {
		c.tmdb = nil
		return
	}
	c.tmdb = tmdb.New(apiKey, proxy, lang)
}

// UpdateBaseDir 热更新网盘根目录名。
func (c *Client) UpdateBaseDir(baseDir string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if baseDir != "" {
		c.baseDir = baseDir
		c.cidCache = map[string]string{"/": "0"}
	}
}

// UpdateCookie 写入新 Cookie（设置页粘贴）。
func (c *Client) UpdateCookie(cookie string) {
	c.saveCookie(cookie)
}

// UpdateCredentials 兼容旧接口：忽略 2dland client_id/secret。
func (c *Client) UpdateCredentials(_, _ string) {}

// Logout 清除 115 Cookie。
func (c *Client) Logout() {
	c.mu.Lock()
	c.cookie = ""
	c.qrUID = ""
	path := c.tokFile
	c.cidCache = map[string]string{"/": "0"}
	c.mu.Unlock()
	if path != "" {
		_ = os.WriteFile(path, []byte("{}\n"), 0o600)
	}
}

func (c *Client) rememberCID(path, cid string) {
	if path == "" || cid == "" {
		return
	}
	c.mu.Lock()
	c.cidCache[normalizePathKey(path)] = cid
	c.mu.Unlock()
}

func (c *Client) cachedCID(path string) string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.cidCache[normalizePathKey(path)]
}

func normalizePathKey(p string) string {
	if p == "" {
		return "/"
	}
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	if p != "/" {
		p = strings.TrimRight(p, "/")
	}
	return p
}

func normalizeCookie(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimSuffix(s, ";")
	return s
}

// Ping 探测 Cookie 是否仍有效。
func (c *Client) Ping(ctx context.Context) bool {
	if c.cookieHeader() == "" {
		return false
	}
	var out struct {
		State bool `json:"state"`
	}
	if err := c.doJSON(ctx, http.MethodGet, apiStatusCheck, nil, &out); err != nil {
		return false
	}
	return out.State
}
