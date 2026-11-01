package discovery

import "time"

// 空闲时也回收过期内容；一个客户端共用一个定时器，最长延迟 30 秒。
func (c *Client) scheduleCacheExpiryLocked(ttl time.Duration) {
	if c.cacheClosed || c.cacheTimer != nil {
		return
	}
	delay := min(30*time.Second, ttl)
	c.cacheTimer = time.AfterFunc(delay, func() {
		c.mu.Lock()
		defer c.mu.Unlock()
		c.cacheTimer = nil
		c.expireCachesLocked(time.Now())
		if len(c.cache)+len(c.documents) > 0 {
			next := 30 * time.Second
			for _, entry := range c.cache {
				next = min(next, time.Until(entry.expires))
			}
			for _, entry := range c.documents {
				next = min(next, time.Until(entry.expires))
			}
			c.scheduleCacheExpiryLocked(next)
		}
	})
}

func (c *Client) expireCachesLocked(now time.Time) {
	for key, entry := range c.cache {
		if !now.Before(entry.expires) {
			delete(c.cache, key)
		}
	}
	for key, entry := range c.documents {
		if !now.Before(entry.expires) {
			delete(c.documents, key)
			c.documentBytes -= len(entry.body)
		}
	}
}

// 服务退出时释放缓存和空闲连接，避免定时器继续持有整个客户端。
func (c *Client) Close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cacheClosed = true
	if c.cacheTimer != nil {
		c.cacheTimer.Stop()
		c.cacheTimer = nil
	}
	clear(c.cache)
	clear(c.documents)
	c.documentBytes = 0
	if c.httpClient != nil {
		c.httpClient.CloseIdleConnections()
	}
}
