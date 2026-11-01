package discovery

import (
	"context"
	"net/http"
)

// FetchImageURL 通过同一配置代理读取订阅封面等绝对图片 URL。
func (c *Client) FetchImageURL(ctx context.Context, imageURL string) ([]byte, error) {
	config, err := c.config(ctx)
	if err != nil {
		return nil, err
	}
	_, data, err := c.requestText(ctx, config, http.MethodGet, imageURL, nil, nil)
	return data, err
}
