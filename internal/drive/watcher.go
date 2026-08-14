package drive

import (
	"context"
	"log"
	"time"
)

const taskStatusCompleted int32 = 1000
const watchInterval = 20 * time.Second
const watchTimeout = 3 * time.Hour

func (c *Client) startWatcher(identity, savePath string) {
	go func() {
		ctx := context.Background()
		deadline := time.Now().Add(watchTimeout)
		ticker := time.NewTicker(watchInterval)
		defer ticker.Stop()
		log.Printf("watcher: start identity=%q savePath=%q", identity, savePath)
		for range ticker.C {
			if time.Now().After(deadline) {
				log.Printf("watcher: timeout identity=%q (用户可手动整理)", identity)
				return
			}
			done, err := c.isTaskCompleted(ctx, identity)
			if err != nil {
				log.Printf("watcher: check failed identity=%q err=%v", identity, err)
				continue
			}
			if done {
				log.Printf("watcher: task completed, organizing savePath=%q", savePath)
				time.Sleep(5 * time.Second)
				res, err := c.OrganizeTask(ctx, savePath)
				if err != nil {
					log.Printf("watcher: organize failed savePath=%q err=%v", savePath, err)
					return
				}
				log.Printf("watcher: organized savePath=%q deleted=%d renamed=%d skipped=%d",
					savePath, len(res.Deleted), len(res.Renamed), len(res.Skipped))
				return
			}
		}
	}()
}

func (c *Client) isTaskCompleted(ctx context.Context, identity string) (bool, error) {
	tasks, err := c.ListTasks(ctx)
	if err != nil {
		return false, err
	}
	for _, t := range tasks {
		if t.Identity != identity {
			continue
		}
		if t.Status == taskStatusCompleted {
			return true, nil
		}
		if t.BytesTotal > 0 && t.BytesProcessed >= t.BytesTotal {
			return true, nil
		}
		return false, nil
	}
	return false, nil
}
