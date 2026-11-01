package discovery

import (
	"context"
	"strings"
	"sync"
	"time"
)

// 列表补全最多并行四个请求，给公开数据源留出连接余量。
func enrichPublicMetadata(ctx context.Context, count int, enrich func(context.Context, int)) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	jobs := make(chan int)
	var workers sync.WaitGroup
	for range min(4, count) {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for index := range jobs {
				enrich(ctx, index)
			}
		}()
	}
	for i := 0; i < count; i++ {
		select {
		case jobs <- i:
		case <-ctx.Done():
			close(jobs)
			workers.Wait()
			return
		}
	}
	close(jobs)
	workers.Wait()
}

func publicScore(info bgmInfo) float64 {
	if info.Rating != nil {
		return info.Rating.Score
	}
	return 0
}

func publicCover(info bgmInfo) string {
	for _, image := range []string{info.Images.Small, info.Images.Common, info.Images.Medium, info.Images.Large, info.Images.Grid} {
		if image != "" {
			return image
		}
	}
	return ""
}

type publicMetadata struct {
	Score float64
	Cover string
}

func (c *Client) publicSubject(ctx context.Context, config map[string]any, id string) (publicMetadata, error) {
	value, err := c.cached("public-subject:"+bgmBase(config)+":"+id, time.Hour, func() (any, error) {
		info, err := c.bgmSubject(ctx, config, id, false)
		// 列表只用评分和封面，完整标签与 infobox 留给详情读取。
		return publicMetadata{Score: publicScore(info), Cover: publicCover(info)}, err
	})
	if err != nil {
		return publicMetadata{}, err
	}
	return value.(publicMetadata), nil
}

func (c *Client) enrichAnimeGarden(ctx context.Context, config map[string]any, subjects []animeGardenSubject) {
	enrichPublicMetadata(ctx, len(subjects), func(ctx context.Context, index int) {
		subject := &subjects[index]
		info, err := c.publicSubject(ctx, config, subject.ID)
		if err != nil {
			return
		}
		subject.Score = info.Score
		if cover := info.Cover; cover != "" {
			subject.Cover = cover
		}
	})
}

func (c *Client) enrichMikan(ctx context.Context, config map[string]any, result *mikan, subscribed map[string]bool) {
	var items []*mikanInfo
	for wi := range result.Weeks {
		for ii := range result.Weeks[wi].Items {
			items = append(items, &result.Weeks[wi].Items[ii])
		}
	}
	enrichPublicMetadata(ctx, len(items), func(ctx context.Context, index int) {
		item := items[index]
		bgmID := subjectIDFromURL(item.BgmURL)
		if bgmID == "" {
			id := strings.TrimRight(mikanIDPattern.FindString(item.URL), "/")
			if id == "" {
				return
			}
			value, err := c.cached("public-mikan:"+c.get(ctx, config, "mikanHost", "https://mikanani.me")+":"+id, 24*time.Hour, func() (any, error) {
				info, err := c.mikanInfo(ctx, config, id)
				if err != nil {
					return nil, err
				}
				// 列表只缓存站点关联，不保留完整字幕组与资源列表。
				return strings.Clone(info.BgmURL), nil
			})
			if err != nil {
				return
			}
			item.BgmURL = value.(string)
			bgmID = subjectIDFromURL(item.BgmURL)
		}
		item.BgmID = bgmID
		if subscribed[bgmID] {
			item.Exists = true
		}
		if bgmID == "" {
			return
		}
		info, err := c.publicSubject(ctx, config, bgmID)
		if err != nil {
			return
		}
		score := info.Score
		item.Score = &score
	})
}
