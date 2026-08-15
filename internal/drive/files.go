package drive

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type fileListResp struct {
	basicResp
	Count int             `json:"count"`
	CID   json.RawMessage `json:"cid"`
	Data  []fileInfoRaw   `json:"data"`
}

type fileInfoRaw struct {
	FID   string
	CID   json.RawMessage
	PID   string
	Name  string
	Size  json.RawMessage
	T     string
	TP    json.RawMessage
	FC    string
	Files json.RawMessage
	Dirs  json.RawMessage
}

func (r *fileInfoRaw) UnmarshalJSON(b []byte) error {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(b, &m); err != nil {
		return err
	}
	pick := func(keys ...string) json.RawMessage {
		for _, k := range keys {
			if v, ok := m[k]; ok {
				return v
			}
		}
		return nil
	}
	r.FID = rawString(pick("fid", "file_id"))
	r.CID = pick("cid", "category_id")
	r.PID = rawString(pick("pid", "parent_id"))
	r.Name = rawString(pick("n", "fn", "file_name", "name"))
	r.Size = pick("s", "file_size", "fs")
	r.T = rawString(pick("t", "utime", "user_utime"))
	r.TP = pick("tp", "te", "tu")
	r.FC = rawString(pick("fc", "file_category"))
	// natsort 列表里 ns 是文件夹名，不是数量；计数走 category/get。
	r.Files = pick("files", "file_count", "file_cnt")
	r.Dirs = pick("dirs", "dir_count", "folder_count")
	return nil
}

func rawString(b json.RawMessage) string {
	if len(b) == 0 {
		return ""
	}
	var s string
	if json.Unmarshal(b, &s) == nil {
		return s
	}
	var n json.Number
	if json.Unmarshal(b, &n) == nil {
		return n.String()
	}
	return strings.Trim(string(b), `"`)
}

func rawInt64(b json.RawMessage) int64 {
	s := rawString(b)
	if s == "" {
		return 0
	}
	n, _ := strconv.ParseInt(s, 10, 64)
	return n
}

func (r fileInfoRaw) isDir() bool {
	if r.FC == "0" {
		return true
	}
	if r.FC == "1" {
		return false
	}
	return r.FID == ""
}

func (r fileInfoRaw) id() string {
	if r.FID != "" {
		return r.FID
	}
	return rawString(r.CID)
}

func parseUpdateTs(t string, tp json.RawMessage) int64 {
	if n := rawInt64(tp); n > 1e9 {
		return n
	}
	if n, err := strconv.ParseInt(t, 10, 64); err == nil && n > 1e9 {
		return n
	}
	if tm, err := time.ParseInLocation("2006-01-02 15:04", t, time.FixedZone("CST", 8*3600)); err == nil {
		return tm.Unix()
	}
	return 0
}

// resolveCID 把显示路径解析成 115 目录 cid（根为 0）。
// getid 接口在部分网络会 405，失败则从根目录逐级列出。
func (c *Client) resolveCID(ctx context.Context, parentPath string) (string, error) {
	parentPath = normalizePathKey(parentPath)
	if parentPath == "/" {
		return "0", nil
	}
	if id := c.cachedCID(parentPath); id != "" {
		return id, nil
	}
	rel := strings.TrimPrefix(parentPath, "/")
	q := url.Values{"path": {rel}}
	if b, err := c.do(ctx, http.MethodGet, apiDirGetID+"?"+q.Encode(), nil); err == nil {
		var out struct {
			basicResp
			ID json.RawMessage `json:"id"`
		}
		if json.Unmarshal(b, &out) == nil {
			id := rawString(out.ID)
			if id != "" && id != "0" {
				c.rememberCID(parentPath, id)
				return id, nil
			}
		}
	}
	return c.walkCID(ctx, parentPath)
}

func (c *Client) walkCID(ctx context.Context, fullPath string) (string, error) {
	parts := strings.Split(strings.Trim(fullPath, "/"), "/")
	cid := "0"
	cur := ""
	for _, name := range parts {
		if name == "" {
			continue
		}
		parent := cur
		if parent == "" {
			parent = "/"
		}
		cur = joinPath(parent, name)
		if id := c.cachedCID(cur); id != "" {
			cid = id
			continue
		}
		files, err := c.listByCID(ctx, cid, parent)
		if err != nil {
			return "", fmt.Errorf("解析目录 %s: %w", cur, err)
		}
		found := ""
		for _, f := range files {
			if f.Dir && f.Name == name {
				found = f.Identity
				break
			}
		}
		if found == "" {
			return "", fmt.Errorf("目录不存在: %s", cur)
		}
		c.rememberCID(cur, found)
		cid = found
	}
	return cid, nil
}

// ListFiles 列出 parentPath 下全部项。
func (c *Client) ListFiles(ctx context.Context, parentPath string) ([]*File, error) {
	if c.cookieHeader() == "" {
		return nil, fmt.Errorf("未登录 115")
	}
	cid, err := c.resolveCID(ctx, parentPath)
	if err != nil {
		return nil, err
	}
	all, err := c.listByCID(ctx, cid, parentPath)
	if err != nil {
		return nil, err
	}
	c.fillDirCounts(ctx, all)
	return all, nil
}

func (c *Client) listByCID(ctx context.Context, cid, parentPath string) ([]*File, error) {
	var all []*File
	offset := 0
	const limit = 1150
	for page := 0; page < 50; page++ {
		q := url.Values{
			"aid":              {"1"},
			"cid":              {cid},
			"o":                {"user_ptime"},
			"asc":              {"0"},
			"offset":           {strconv.Itoa(offset)},
			"show_dir":         {"1"},
			"limit":            {strconv.Itoa(limit)},
			"snap":             {"0"},
			"natsort":          {"0"},
			"record_open_time": {"1"},
			"format":           {"json"},
			"fc_mix":           {"0"},
		}
		var resp fileListResp
		if err := c.doJSON(ctx, http.MethodGet, apiFiles+"?"+q.Encode(), nil, &resp); err != nil {
			if page == 0 {
				return nil, err
			}
			break
		}
		if !resp.ok() && page == 0 && len(resp.Data) == 0 {
			return nil, fmt.Errorf("%s", resp.errMsg())
		}
		for _, it := range resp.Data {
			f := &File{
				Identity:    it.id(),
				Name:        it.Name,
				Path:        joinPath(parentPath, it.Name),
				Dir:         it.isDir(),
				Size:        rawInt64(it.Size),
				UpdateTs:    parseUpdateTs(it.T, it.TP),
				Files:       rawInt64(it.Files),
				Dirs:        rawInt64(it.Dirs),
				Direcotries: rawInt64(it.Dirs),
			}
			if f.Dir {
				c.rememberCID(f.Path, f.Identity)
			}
			all = append(all, f)
		}
		offset += len(resp.Data)
		if offset >= resp.Count || len(resp.Data) == 0 {
			break
		}
	}
	return all, nil
}

// fillDirCounts 用 category/get 补目录内文件数/子目录数（natsort 列表没有这两个字段）。
func (c *Client) fillDirCounts(ctx context.Context, files []*File) {
	for _, f := range files {
		if f == nil || !f.Dir || f.Identity == "" {
			continue
		}
		filesN, dirsN, err := c.dirCounts(ctx, f.Identity)
		if err != nil {
			log.Printf("fillDirCounts: %s (%s): %v", f.Name, f.Identity, err)
			continue
		}
		f.Files = filesN
		f.Dirs = dirsN
		f.Direcotries = dirsN
	}
}

func (c *Client) dirCounts(ctx context.Context, cid string) (filesN, dirsN int64, err error) {
	q := url.Values{"cid": {cid}}
	b, err := c.doFallback(ctx, http.MethodGet, []string{
		apiFileStat + "?" + q.Encode(),
		apiFileStatAlt + "?" + q.Encode(),
	}, nil)
	if err != nil {
		return 0, 0, err
	}
	var out struct {
		basicResp
		Count       json.RawMessage `json:"count"`
		FolderCount json.RawMessage `json:"folder_count"`
	}
	if err := json.Unmarshal(b, &out); err != nil {
		return 0, 0, err
	}
	return rawInt64(out.Count), rawInt64(out.FolderCount), nil
}

// Mkdir 在 parentPath 下建目录。
func (c *Client) Mkdir(ctx context.Context, parentPath, name string) (*File, error) {
	pid, err := c.resolveCID(ctx, parentPath)
	if err != nil {
		return nil, fmt.Errorf("父目录 %q 不存在: %w", parentPath, err)
	}
	form := url.Values{"pid": {pid}, "cname": {name}}
	b, err := c.doFallback(ctx, http.MethodPost, []string{apiDirAdd, apiDirAddAlt}, form)
	if err != nil {
		return nil, err
	}
	if err := checkState(b); err != nil {
		msg := err.Error()
		if files, e2 := c.ListFiles(ctx, parentPath); e2 == nil {
			for _, f := range files {
				if f.Dir && f.Name == name {
					return f, nil
				}
			}
		}
		if strings.Contains(msg, "已存在") || strings.Contains(msg, "exist") {
			p := joinPath(parentPath, name)
			return &File{Name: name, Path: p, Dir: true}, nil
		}
		return nil, err
	}
	var out struct {
		basicResp
		CID    json.RawMessage `json:"cid"`
		FileID json.RawMessage `json:"file_id"`
	}
	_ = json.Unmarshal(b, &out)
	id := rawString(out.CID)
	if id == "" {
		id = rawString(out.FileID)
	}
	p := joinPath(parentPath, name)
	if id != "" {
		c.rememberCID(p, id)
	}
	return &File{Identity: id, Name: name, Path: p, Dir: true}, nil
}

// Rename 重命名。
func (c *Client) Rename(ctx context.Context, identity, newName string) error {
	form := url.Values{
		"fid": {identity},
		"file_name": {newName},
		fmt.Sprintf("files_new_name[%s]", identity): {newName},
	}
	b, err := c.do(ctx, http.MethodPost, apiFileRename, form)
	if err != nil {
		return err
	}
	return checkState(b)
}

// Move 移动到 destPath。
func (c *Client) Move(ctx context.Context, identities []string, destPath string) error {
	if len(identities) == 0 {
		return errEmptyIdentity
	}
	pid, err := c.resolveCID(ctx, destPath)
	if err != nil {
		return fmt.Errorf("移动目标目录 %q 不存在: %w", destPath, err)
	}
	form := url.Values{"pid": {pid}}
	for i, id := range identities {
		form.Set(fmt.Sprintf("fid[%d]", i), id)
	}
	b, err := c.doFallback(ctx, http.MethodPost, []string{apiFileMove, apiFileMoveAlt}, form)
	if err != nil {
		return err
	}
	return checkState(b)
}

// DeleteFiles 移到回收站。
func (c *Client) DeleteFiles(ctx context.Context, identities []string) error {
	if len(identities) == 0 {
		return errEmptyIdentity
	}
	form := url.Values{"ignore_warn": {"1"}}
	for i, id := range identities {
		form.Set(fmt.Sprintf("fid[%d]", i), id)
	}
	urls := []string{apiFileDelete, apiFileDeleteAlt, apiFileDeleteV2}
	b, err := c.doFallback(ctx, http.MethodPost, urls, form)
	if err != nil {
		// 部分账号只认 fid= / file_id=
		var last error
		last = err
		for _, key := range []string{"fid", "file_id"} {
			one := url.Values{"ignore_warn": {"1"}}
			for _, id := range identities {
				one.Add(key, id)
			}
			bb, e2 := c.doFallback(ctx, http.MethodPost, urls, one)
			if e2 != nil {
				last = e2
				continue
			}
			if e3 := checkState(bb); e3 != nil {
				last = e3
				continue
			}
			return nil
		}
		return last
	}
	return checkState(b)
}

// ListTrash 回收站。
func (c *Client) ListTrash(ctx context.Context) ([]*File, error) {
	var all []*File
	offset := 0
	for page := 0; page < 20; page++ {
		q := url.Values{
			"aid":    {"7"},
			"cid":    {"0"},
			"format": {"json"},
			"offset": {strconv.Itoa(offset)},
			"limit":  {"1150"},
		}
		var resp struct {
			basicResp
			Data []struct {
				ID       string          `json:"id"`
				FileName string          `json:"file_name"`
				FileSize json.RawMessage `json:"file_size"`
				DTime    json.RawMessage `json:"dtime"`
			} `json:"data"`
			Count int `json:"count"`
		}
		if err := c.doJSON(ctx, http.MethodGet, apiRecycleList+"?"+q.Encode(), nil, &resp); err != nil {
			if page == 0 {
				return nil, err
			}
			break
		}
		for _, it := range resp.Data {
			all = append(all, &File{
				Identity: it.ID,
				Name:     it.FileName,
				Path:     "/回收站/" + it.FileName,
				Size:     rawInt64(it.FileSize),
				UpdateTs: rawInt64(it.DTime),
			})
		}
		offset += len(resp.Data)
		if len(resp.Data) == 0 || (resp.Count > 0 && offset >= resp.Count) {
			break
		}
	}
	return all, nil
}

// Recover 从回收站恢复。
func (c *Client) Recover(ctx context.Context, identities []string) error {
	if len(identities) == 0 {
		return errEmptyIdentity
	}
	form := url.Values{}
	for i, id := range identities {
		form.Set(fmt.Sprintf("rid[%d]", i), id)
	}
	b, err := c.do(ctx, http.MethodPost, apiRecycleRev, form)
	if err != nil {
		return err
	}
	return checkState(b)
}

// ListRecentFiles 最近文件（根目录按时间）。
func (c *Client) ListRecentFiles(ctx context.Context) ([]*File, error) {
	q := url.Values{
		"aid":      {"1"},
		"cid":      {"0"},
		"offset":   {"0"},
		"limit":    {"115"},
		"format":   {"json"},
		"o":        {"user_ptime"},
		"asc":      {"0"},
		"show_dir": {"0"},
	}
	var resp fileListResp
	if err := c.doJSON(ctx, http.MethodGet, apiFiles+"?"+q.Encode(), nil, &resp); err != nil {
		return nil, err
	}
	if !resp.ok() {
		return nil, fmt.Errorf("%s", resp.errMsg())
	}
	var all []*File
	for _, it := range resp.Data {
		if it.isDir() {
			continue
		}
		all = append(all, &File{
			Identity: it.id(),
			Name:     it.Name,
			Path:     "/" + it.Name,
			Size:     rawInt64(it.Size),
			UpdateTs: parseUpdateTs(it.T, it.TP),
		})
	}
	return all, nil
}

// resolvePathByCID 用 category/get 把 cid 还原成路径。
func (c *Client) resolvePathByCID(ctx context.Context, cid string) string {
	if cid == "" || cid == "0" {
		return "/"
	}
	q := url.Values{"cid": {cid}}
	var out struct {
		basicResp
		Paths []struct {
			FileID   json.RawMessage `json:"file_id"`
			FileName string          `json:"file_name"`
		} `json:"paths"`
		FileName string `json:"file_name"`
	}
	if err := c.doJSON(ctx, http.MethodGet, apiFileStat+"?"+q.Encode(), nil, &out); err != nil {
		return ""
	}
	var parts []string
	for _, p := range out.Paths {
		if p.FileName == "" || p.FileName == "根目录" {
			continue
		}
		parts = append(parts, p.FileName)
	}
	if out.FileName != "" && out.FileName != "根目录" {
		parts = append(parts, out.FileName)
	}
	if len(parts) == 0 {
		return ""
	}
	return "/" + strings.Join(parts, "/")
}
