package drive

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"6v-to-2dland/internal/tmdb"
)

// PushItem 是待推送的一条磁力链及其所属资源信息。
type PushItem struct {
	Name     string `json:"name"`
	Magnet   string `json:"magnet"`
	Category string `json:"category"`
	Title    string `json:"title"`
}

// PushResultItem 是单条磁力链推送的结果。
type PushResultItem struct {
	Name     string `json:"name"`
	Magnet   string `json:"magnet"`
	Category string `json:"category"`
	Folder   string `json:"folder"`
	SavePath string `json:"save_path"`
	Season   string `json:"season,omitempty"`
	OK       bool   `json:"ok"`
	Identity string `json:"identity,omitempty"`
	Error    string `json:"error,omitempty"`
}

// PushResult 是批量推送的汇总。
type PushResult struct {
	Items []PushResultItem `json:"items"`
}

// Push 按 分类/标题/[季] 建目录并添加 115 离线任务。
func (c *Client) Push(ctx context.Context, items []PushItem) (*PushResult, error) {
	s := c.snap()
	res := &PushResult{}
	cache := map[string]string{}
	log.Printf("Push: start items=%d logged_in=%v baseDir=%q", len(items), c.LoggedIn(), s.baseDir)
	for i, it := range items {
		ri := PushResultItem{Name: it.Name, Magnet: it.Magnet, Category: it.Category}
		titleName := normalizeFolderName(ctx, s.tmdb, it.Title, it.Category)
		ri.Folder = titleName

		seasonName := ""
		key := it.Category + "|" + titleName
		if isTVCategory(it.Category) {
			n := seasonFromMagnet(it.Magnet, it.Name)
			seasonName = "第" + strconv.Itoa(n) + "季"
			ri.Season = seasonName
			key += "|" + seasonName
		}

		savePath, cached := cache[key]
		if !cached {
			sp, err := ensureFolderByCategory(ctx, c, it.Category, titleName, seasonName)
			if err != nil {
				ri.Error = "创建文件夹失败: " + err.Error()
				cache[key] = ""
				res.Items = append(res.Items, ri)
				log.Printf("Push[%d]: ensureFolder failed: %v", i, err)
				continue
			}
			savePath = sp
			cache[key] = savePath
		}
		if savePath == "" {
			ri.Error = "创建文件夹失败"
			res.Items = append(res.Items, ri)
			continue
		}
		ri.SavePath = savePath

		hash, err := c.addOffline(ctx, it.Magnet, savePath)
		if err != nil {
			ri.Error = err.Error()
			log.Printf("Push[%d]: offline.Add failed: %v", i, err)
		} else {
			ri.OK = true
			ri.Identity = hash
			log.Printf("Push[%d]: offline.Add ok identity=%s path=%s", i, hash, savePath)
			c.startWatcher(hash, savePath)
		}
		res.Items = append(res.Items, ri)
	}
	return res, nil
}

func (c *Client) addOffline(ctx context.Context, magnet, savePath string) (string, error) {
	cid, err := c.resolveCID(ctx, savePath)
	if err != nil {
		return "", fmt.Errorf("保存目录无效: %w", err)
	}
	form := url.Values{
		"url":        {magnet},
		"savepath":   {""},
		"wp_path_id": {cid},
	}
	b, err := c.do(ctx, http.MethodPost, apiLixianAdd, form)
	if err != nil {
		return "", err
	}
	var out struct {
		basicResp
		InfoHash string `json:"info_hash"`
		Name     string `json:"name"`
	}
	if err := json.Unmarshal(b, &out); err != nil {
		return "", fmt.Errorf("解析离线结果: %w (%s)", err, truncate(string(b), 120))
	}
	if !out.ok() {
		return "", fmt.Errorf("%s", out.errMsg())
	}
	if out.InfoHash == "" {
		return "ok", nil
	}
	return out.InfoHash, nil
}

var titleRe = regexp.MustCompile(`(?:([0-9]{4}))?[^\d《]*《([^》]+)》`)

func parseTitle(t string) (name, year string) {
	if m := titleRe.FindStringSubmatch(t); m != nil {
		year = m[1]
		name = m[2]
		return
	}
	name = t
	return
}

func normalizeFolderName(ctx context.Context, t *tmdb.Client, title, category string) string {
	if t != nil {
		name, year := parseTitle(title)
		if name == "" {
			name = title
		}
		mediaType := "movie"
		if isTVCategory(category) {
			mediaType = "tv"
		}
		if r, err := t.Search(ctx, name, mediaType); err == nil && r != nil && r.Title != "" {
			y := tmdb.YearFromDate(r.Date)
			if y == "" {
				y = year
			}
			if y != "" {
				return sanitize(r.Title + " (" + y + ")")
			}
			return sanitize(r.Title)
		}
	}
	return sanitize(title)
}

type lixianTask struct {
	InfoHash string  `json:"info_hash"`
	Name     string  `json:"name"`
	Size     int64   `json:"size"`
	URL      string  `json:"url"`
	Status   int     `json:"status"`
	Percent  float64 `json:"percentDone"`
	FileID   string  `json:"file_id"`
	DirID    string  `json:"wp_path_id"`
}

// ListTasks 列出 115 离线任务。
func (c *Client) ListTasks(ctx context.Context) ([]*UserTask, error) {
	var all []*UserTask
	for page := 1; page <= 50; page++ {
		// URL 本身已有 ?ct=&ac=，分页必须用 &page=，不能再拼 ?
		u := apiLixianList + "&page=" + strconv.Itoa(page)
		b, err := c.do(ctx, http.MethodPost, u, nil)
		if err != nil {
			if page == 1 {
				return nil, err
			}
			break
		}
		if err := checkState(b); err != nil && page == 1 {
			// task_lists 成功时也带 state=true；失败时可能是纯文本
			if len(b) > 0 && b[0] != '{' && b[0] != '[' {
				return nil, fmt.Errorf("115 任务列表异常: %s", truncate(string(b), 160))
			}
		}
		var resp struct {
			basicResp
			Tasks     []*lixianTask `json:"tasks"`
			PageCount int           `json:"page_count"`
			Page      int           `json:"page"`
		}
		if err := json.Unmarshal(b, &resp); err != nil {
			if page == 1 {
				return nil, fmt.Errorf("解析任务列表: %w (%s)", err, truncate(string(b), 120))
			}
			break
		}
		if !resp.ok() && page == 1 && len(resp.Tasks) == 0 && resp.State != nil {
			return nil, fmt.Errorf("%s", resp.errMsg())
		}
		for _, t := range resp.Tasks {
			all = append(all, map115Task(ctx, c, t))
		}
		if len(resp.Tasks) == 0 {
			break
		}
		if resp.PageCount > 0 && page >= resp.PageCount {
			break
		}
	}
	for _, t := range all {
		if t != nil && t.SavePath != "" && c.isOrganized(t.SavePath) {
			t.Organized = true
		}
	}
	log.Printf("ListTasks: total %d tasks", len(all))
	return all, nil
}

func map115Task(ctx context.Context, c *Client, t *lixianTask) *UserTask {
	st := int32(0)
	switch t.Status {
	case 0:
		st = 0
	case 1:
		st = 2
	case 2:
		st = 1000
	case -1:
		st = 3
	default:
		st = int32(t.Status)
	}
	pct := t.Percent
	if pct > 100 {
		pct = 100
	}
	if st == 1000 && pct == 0 {
		pct = 100
	}
	save := ""
	if t.DirID != "" {
		save = c.resolvePathByCID(ctx, t.DirID)
	}
	return &UserTask{
		Identity:       t.InfoHash,
		Name:           t.Name,
		Url:            t.URL,
		SavePath:       save,
		Status:         st,
		Progress:       pct,
		BytesTotal:     t.Size,
		BytesProcessed: int64(float64(t.Size) * pct / 100),
	}
}

// DeleteTask 删除 115 离线任务。115 网页接口要 hash[0]、hash[1]，不能重复传 hash=。
func (c *Client) DeleteTask(ctx context.Context, identities []string, deleteFiles bool) error {
	_, err := c.DeleteTaskResult(ctx, identities, deleteFiles)
	return err
}

type DeleteTaskResult struct {
	Requested int      `json:"requested"`
	Deleted   int      `json:"deleted"`
	Failed    []string `json:"failed,omitempty"`
}

func (c *Client) DeleteTaskResult(ctx context.Context, identities []string, deleteFiles bool) (*DeleteTaskResult, error) {
	seen := map[string]bool{}
	var hashes []string
	for _, h := range identities {
		h = strings.TrimSpace(h)
		if h == "" || seen[h] {
			continue
		}
		seen[h] = true
		hashes = append(hashes, h)
	}
	out := &DeleteTaskResult{Requested: len(hashes)}
	if len(hashes) == 0 {
		return out, errEmptyIdentity
	}
	flag := "0"
	if deleteFiles {
		flag = "1"
	}
	const chunk = 20
	var lastErr error
	for i := 0; i < len(hashes); i += chunk {
		end := i + chunk
		if end > len(hashes) {
			end = len(hashes)
		}
		part := hashes[i:end]
		form := url.Values{"flag": {flag}}
		for j, h := range part {
			form.Set("hash["+strconv.Itoa(j)+"]", h)
		}
		b, err := c.do(ctx, http.MethodPost, apiLixianDel, form)
		if err != nil {
			lastErr = err
			out.Failed = append(out.Failed, part...)
			log.Printf("DeleteTask: chunk err=%v body=%s", err, truncate(string(b), 160))
			continue
		}
		if err := checkState(b); err != nil {
			// 部分账号只认逐条 hash=
			okN := 0
			for _, h := range part {
				one := url.Values{"flag": {flag}, "hash[0]": {h}, "hash": {h}}
				bb, e2 := c.do(ctx, http.MethodPost, apiLixianDel, one)
				if e2 != nil || checkState(bb) != nil {
					out.Failed = append(out.Failed, h)
					if e2 != nil {
						lastErr = e2
					} else {
						lastErr = checkState(bb)
					}
					continue
				}
				okN++
			}
			out.Deleted += okN
			log.Printf("DeleteTask: chunk fallback ok=%d fail=%d last=%v raw=%s", okN, len(part)-okN, lastErr, truncate(string(b), 160))
			continue
		}
		out.Deleted += len(part)
	}
	log.Printf("DeleteTask: requested=%d deleted=%d failed=%d delete_files=%v last=%v", out.Requested, out.Deleted, len(out.Failed), deleteFiles, lastErr)
	if out.Deleted == 0 && lastErr != nil {
		return out, lastErr
	}
	return out, nil
}

var errEmptyIdentity = fmt.Errorf("identity 不能为空")
