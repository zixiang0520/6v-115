package tmdb

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

// Client 通过（可选）代理访问 TMDB API；代理失败会自动直连 api.tmdb.org。
type Client struct {
	apiKey string
	lang   string
	base   string
	proxy  string
	http   *http.Client
	direct *http.Client
}

// New 创建 TMDB 客户端；proxy 为空则直连。
func New(apiKey, proxy, lang string) *Client {
	if lang == "" {
		lang = "zh-CN"
	}
	// 容器里 127.0.0.1 是容器自己，不是 NAS 宿主机上的代理
	if strings.Contains(proxy, "127.0.0.1") {
		proxy = strings.ReplaceAll(proxy, "127.0.0.1", "host.docker.internal")
	}
	if strings.Contains(proxy, "localhost") {
		proxy = strings.ReplaceAll(proxy, "localhost", "host.docker.internal")
	}
	transport := &http.Transport{}
	if proxy != "" {
		if u, err := url.Parse(proxy); err == nil {
			transport.Proxy = http.ProxyURL(u)
		}
	}
	return &Client{
		apiKey: apiKey,
		lang:   lang,
		base:   "https://api.tmdb.org",
		proxy:  proxy,
		http:   &http.Client{Transport: transport, Timeout: 15 * time.Second},
		direct: &http.Client{Timeout: 15 * time.Second},
	}
}

// Result 是一条搜索结果。
type Result struct {
	Title string
	Date  string // release_date 或 first_air_date
}

func (c *Client) searchOnce(ctx context.Context, cl *http.Client, query, mediaType string) (*Result, error) {
	u := fmt.Sprintf("%s/3/search/%s?api_key=%s&query=%s&language=%s",
		c.base, mediaType, c.apiKey, url.QueryEscape(query), c.lang)
	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return nil, err
	}
	resp, err := cl.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("tmdb http %d", resp.StatusCode)
	}
	var data struct {
		Results []struct {
			Title        string `json:"title"`
			Name         string `json:"name"`
			ReleaseDate  string `json:"release_date"`
			FirstAirDate string `json:"first_air_date"`
		} `json:"results"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, err
	}
	if len(data.Results) == 0 {
		return nil, nil
	}
	r := data.Results[0]
	title := r.Title
	if title == "" {
		title = r.Name
	}
	date := r.ReleaseDate
	if date == "" {
		date = r.FirstAirDate
	}
	return &Result{Title: title, Date: date}, nil
}

// Search 按名字搜索，mediaType 为 "movie" 或 "tv"；无结果返回 (nil, nil)。
// 有代理时先走代理，失败则直连 api.tmdb.org（容器内 127.0.0.1 代理不可达）。
func (c *Client) Search(ctx context.Context, query, mediaType string) (*Result, error) {
	if c.apiKey == "" {
		return nil, errors.New("tmdb api key 未配置")
	}
	if c.proxy != "" {
		if r, err := c.searchOnce(ctx, c.http, query, mediaType); err == nil {
			return r, nil
		}
	}
	return c.searchOnce(ctx, c.direct, query, mediaType)
}

var yearRe = regexp.MustCompile(`^(\d{4})`)

// YearFromDate 从 YYYY-MM-DD 提取年份。
func YearFromDate(s string) string {
	if m := yearRe.FindStringSubmatch(s); m != nil {
		return m[1]
	}
	return ""
}
