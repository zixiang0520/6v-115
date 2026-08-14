// 115 目录移动/整理冒烟测试（无需 2dland SDK）。
// 用法：在已登录的 data 目录下：go run ./cmd/movetest
package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"6v-to-2dland/internal/cfg"
	"6v-to-2dland/internal/drive"
)

func main() {
	c, err := cfg.Load("config.json")
	if err != nil {
		fmt.Println("load config:", err)
		os.Exit(1)
	}
	cl := drive.New(c)
	if !cl.LoggedIn() {
		fmt.Println("未登录 115，先在 Web 扫码或写 cookie")
		os.Exit(1)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	files, err := cl.ListFiles(ctx, "/")
	if err != nil {
		fmt.Println("list /:", err)
		os.Exit(1)
	}
	fmt.Printf("根目录 %d 项\n", len(files))
	for i, f := range files {
		if i >= 8 {
			break
		}
		kind := "file"
		if f.Dir {
			kind = "dir"
		}
		fmt.Printf("  [%s] %s  id=%s\n", kind, f.Name, f.Identity)
	}
}
