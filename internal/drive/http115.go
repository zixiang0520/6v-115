package drive

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	ua115          = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/122.0.0.0 Safari/537.36 115Browser/27.0.5.7"
	// NAS 出口：https://webapi.115.com 与 lixian.115.com 会被 WAF 直接 405。
	// 列表走 natsort（字段与网页一致），其余走 proapi android。
	apiFiles       = "https://aps.115.com/natsort/files.php"
	apiDirAdd      = "https://proapi.115.com/android/files/add"
	apiDirGetID    = "https://proapi.115.com/android/files/getid"
	apiFileMove    = "https://proapi.115.com/android/files/move"
	apiFileRename  = "https://proapi.115.com/android/files/batch_rename"
	apiFileDelete  = "https://proapi.115.com/android/rb/delete"
	apiFileSearch  = "https://proapi.115.com/android/files/search"
	apiFileStat    = "https://proapi.115.com/android/2.0/category/get"
	apiRecycleList = "https://proapi.115.com/android/rb"
	apiRecycleRev  = "https://proapi.115.com/android/rb/revert"
	apiUserNav     = "https://my.115.com/?ct=ajax&ac=nav"
	apiStatusCheck = "https://my.115.com/?ct=guide&ac=status"
	apiLixianAdd   = "https://115.com/web/lixian/?ct=lixian&ac=add_task_url"
	apiLixianList  = "https://115.com/web/lixian/?ct=lixian&ac=task_lists"
	apiLixianDel   = "https://115.com/web/lixian/?ct=lixian&ac=task_del"
	apiQRToken     = "https://qrcodeapi.115.com/api/1.0/web/1.0/token"
	apiQRStatus    = "https://qrcodeapi.115.com/get/status/"
	apiQRImage     = "https://qrcodeapi.115.com/api/1.0/web/1.0/qrcode?uid=%s"
	apiQRLogin     = "https://passportapi.115.com/app/1.0/%s/1.0/login/qrcode"
)

func newHTTP() *http.Client {
	return &http.Client{Timeout: 30 * time.Second}
}

func (c *Client) cookieHeader() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.cookie
}

func rewrite115Host(rawURL string) string {
	repls := [][2]string{
		{"https://lixian.115.com/lixian/", "https://115.com/web/lixian/"},
		{"http://lixian.115.com/lixian/", "https://115.com/web/lixian/"},
		{"https://webapi.115.com", "https://proapi.115.com/android"},
		{"http://web.api.115.com", "https://proapi.115.com/android"},
	}
	out := rawURL
	for _, p := range repls {
		out = strings.Replace(out, p[0], p[1], 1)
	}
	// 列表接口单独走 natsort，不要把 /files/add 等子路径一起改掉。
	out = strings.Replace(out, "https://proapi.115.com/android/files?", "https://aps.115.com/natsort/files.php?", 1)
	if strings.HasSuffix(out, "https://proapi.115.com/android/files") {
		out = "https://aps.115.com/natsort/files.php"
	}
	if out == rawURL {
		return ""
	}
	return out
}

func (c *Client) do(ctx context.Context, method, rawURL string, form url.Values) ([]byte, error) {
	return c.doOnce(ctx, method, rawURL, form, true)
}

func (c *Client) doOnce(ctx context.Context, method, rawURL string, form url.Values, allowRewrite bool) ([]byte, error) {
	var body io.Reader
	if form != nil && method != http.MethodGet {
		body = strings.NewReader(form.Encode())
	}
	if form != nil && method == http.MethodGet {
		u, err := url.Parse(rawURL)
		if err != nil {
			return nil, err
		}
		q := u.Query()
		for k, vs := range form {
			for _, v := range vs {
				q.Add(k, v)
			}
		}
		u.RawQuery = q.Encode()
		rawURL = u.String()
	}
	req, err := http.NewRequestWithContext(ctx, method, rawURL, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", ua115)
	req.Header.Set("Accept", "application/json, text/javascript, */*; q=0.01")
	if ck := c.cookieHeader(); ck != "" {
		req.Header.Set("Cookie", ck)
	}
	if form != nil && method != http.MethodGet {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	req.Header.Set("Referer", "https://115.com/?tab=offline&mode=wangpan")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		if allowRewrite && resp.StatusCode == 405 {
			if alt := rewrite115Host(rawURL); alt != "" {
				log.Printf("115 http 405, retry %s", alt)
				return c.doOnce(ctx, method, alt, form, false)
			}
		}
		return nil, fmt.Errorf("http %d: %s", resp.StatusCode, truncate(string(b), 200))
	}
	return b, nil
}

func (c *Client) doJSON(ctx context.Context, method, rawURL string, form url.Values, out any) error {
	b, err := c.do(ctx, method, rawURL, form)
	if err != nil {
		return err
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(b, out); err != nil {
		return fmt.Errorf("json: %w (%s)", err, truncate(string(b), 180))
	}
	return nil
}

type basicResp struct {
	State   any    `json:"state"`
	Errno   any    `json:"errno"`
	ErrNo   int    `json:"errNo"`
	Error   string `json:"error"`
	Message string `json:"message"`
	Msg     string `json:"msg"`
	Code    int    `json:"code"`
}

func (r basicResp) ok() bool {
	switch v := r.State.(type) {
	case bool:
		return v
	case float64:
		return v != 0
	case string:
		return v == "1" || strings.EqualFold(v, "true")
	default:
		return false
	}
}

func (r basicResp) errMsg() string {
	for _, s := range []string{r.Error, r.Message, r.Msg} {
		if strings.TrimSpace(s) != "" {
			return s
		}
	}
	return fmt.Sprintf("115 请求失败 code=%d", r.Code)
}

func checkState(b []byte) error {
	var r basicResp
	if err := json.Unmarshal(b, &r); err != nil {
		return nil // 部分接口无 state
	}
	if r.State == nil {
		return nil
	}
	if r.ok() {
		return nil
	}
	return fmt.Errorf("%s", r.errMsg())
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
