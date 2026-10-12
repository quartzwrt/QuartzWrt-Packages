// Package daemonfwd 代理 quartz-daemon 回环 HTTP 快照（DESIGN §4.1/§4.4）。
//
// App 数据面唯一通路：GET <DaemonURL>（默认 127.0.0.1:8899/api/v1/status），
// 1s 超时；响应必须是 JSON 对象（即契约 v1 快照），原样内嵌进信封。
package daemonfwd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Timeout 上游取数超时（DESIGN §4.4）。
const Timeout = 1 * time.Second

// MaxBody 快照硬上限（契约 §1：整文件 ≤32KB；留一倍余量防异常体）。
const MaxBody = 64 * 1024

// Client 上游客户端。
type Client struct {
	URL string
	HC  *http.Client
}

// New 创建客户端。
func New(url string) *Client {
	return &Client{
		URL: url,
		HC:  &http.Client{Timeout: Timeout},
	}
}

// Status 取回快照 JSON（校验为对象后原样返回），失败返回错误。
func (c *Client) Status(ctx context.Context) (json.RawMessage, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.URL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.HC.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("daemon http %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, MaxBody+1))
	if err != nil {
		return nil, err
	}
	if len(body) > MaxBody {
		return nil, errors.New("daemon payload too large")
	}
	trimmed := strings.TrimSpace(string(body))
	if !strings.HasPrefix(trimmed, "{") || !json.Valid([]byte(trimmed)) {
		return nil, errors.New("daemon payload not a json object")
	}
	return json.RawMessage(trimmed), nil
}
