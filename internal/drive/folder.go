package drive

import (
	"context"
	"log"
	"strings"
)

var categoryNames = map[string]string{
	"dy": "电影", "gydy": "国语电影", "gq": "经典高清",
	"zydy": "动漫", "jddy": "动画电影", "3D": "3D电影",
	"dlz": "国剧", "rj": "日韩剧", "mj": "欧美剧",
	"zy": "综艺", "shoujidianyingmp4": "手机电影",
}

var tvCategories = map[string]bool{
	"dlz": true, "rj": true, "mj": true, "zy": true, "zydy": true,
}

func categoryName(cat string) string {
	if n, ok := categoryNames[cat]; ok {
		return n
	}
	if cat == "" {
		return "未分类"
	}
	return cat
}

func isTVCategory(cat string) bool { return tvCategories[cat] }

func sanitize(name string) string {
	r := strings.NewReplacer(
		"/", "_", "\\", "_", ":", "_", "：", "_",
		"*", "_", "＊", "_", "?", "_", "？", "_",
		"\"", "_", "＂", "_", "<", "_", "＜", "_",
		">", "_", "＞", "_", "|", "_", "｜", "_",
	)
	s := strings.TrimSpace(r.Replace(name))
	if s == "" {
		s = "未命名"
	}
	return s
}

func ensureFolderByCategory(ctx context.Context, c *Client, category, titleName, seasonName string) (string, error) {
	s := c.snap()
	log.Printf("ensureFolderByCategory: category=%q titleName=%q seasonName=%q baseDir=%q", category, titleName, seasonName, s.baseDir)
	base, err := ensureDir(ctx, c, "/", s.baseDir)
	if err != nil {
		return "", err
	}
	cat, err := ensureDir(ctx, c, base, categoryName(category))
	if err != nil {
		return "", err
	}
	titlePath, err := ensureDir(ctx, c, cat, titleName)
	if err != nil {
		return "", err
	}
	if isTVCategory(category) && seasonName != "" {
		sp, err := ensureDir(ctx, c, titlePath, seasonName)
		if err != nil {
			return "", err
		}
		return sp, nil
	}
	return titlePath, nil
}

func ensureDir(ctx context.Context, c *Client, parentPath, name string) (string, error) {
	want := joinPath(parentPath, name)
	if existing, _ := findDir(ctx, c, parentPath, name); existing != nil {
		c.rememberCID(want, existing.Identity)
		return want, nil
	}
	created, err := c.Mkdir(ctx, parentPath, name)
	if err != nil {
		// 并发创建时可能已存在
		if existing, _ := findDir(ctx, c, parentPath, name); existing != nil {
			c.rememberCID(want, existing.Identity)
			return want, nil
		}
		return "", err
	}
	if created != nil && created.Identity != "" {
		c.rememberCID(want, created.Identity)
	}
	return want, nil
}

func findDir(ctx context.Context, c *Client, parentPath, name string) (*File, error) {
	files, err := c.ListFiles(ctx, parentPath)
	if err != nil {
		return nil, err
	}
	for _, f := range files {
		if f.Dir && f.Name == name {
			return f, nil
		}
	}
	return nil, nil
}

func joinPath(parent, name string) string {
	if parent == "/" || parent == "" {
		return "/" + name
	}
	return strings.TrimRight(parent, "/") + "/" + name
}
