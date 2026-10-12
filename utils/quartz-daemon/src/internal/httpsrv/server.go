// Package httpsrv 提供只读回环 HTTP 接口（契约 §1 HTTP 通道、DESIGN §5）。
//
// 语义：
//   - GET /api/v1/status → 最近成功快照字节（与文件通道严格同字节，§2 单写者）
//   - 非 GET → 405；未知路径 → 404；尚无快照 → 503（均 JSON 短体）
//   - 无认证 / 仅回环（对外认证归 WP-5）；本端口永不承载写操作（DESIGN §12.4）
//   - 监听失败非致命：每 60s 重试，文件通道不受影响（DESIGN §2）
package httpsrv

import (
	"log"
	"net"
	"net/http"
	"sync/atomic"
	"time"
)

// Server 只读快照服务。
type Server struct {
	Addr string // host:port
	data atomic.Value
}

// New 创建服务。
func New(addr string) *Server { return &Server{Addr: addr} }

// Set 发布最新快照字节（单写者调用；读路径无锁）。
func (s *Server) Set(b []byte) {
	s.data.Store(b)
}

// Handler 返回 HTTP 处理器（测试可直接调用）。
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/status", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeErr(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		b, _ := s.data.Load().([]byte)
		if b == nil {
			writeErr(w, http.StatusServiceUnavailable, "no snapshot yet")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusOK)
		w.Write(b)
	})
	return mux
}

func writeErr(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	w.Write([]byte(`{"error":"` + msg + `"}`))
}

// Run 启动监听并在失败时重试（阻塞；调用方以 goroutine 运行）。
func (s *Server) Run() {
	srv := &http.Server{
		Handler:           s.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	for {
		ln, err := net.Listen("tcp", s.Addr)
		if err != nil {
			log.Printf("http: listen %s failed: %v (retry in 60s)", s.Addr, err)
			time.Sleep(60 * time.Second)
			continue
		}
		log.Printf("http: listening on %s", s.Addr)
		if err := srv.Serve(ln); err != nil {
			log.Printf("http: serve stopped: %v (retry in 5s)", err)
			time.Sleep(5 * time.Second)
		}
	}
}
