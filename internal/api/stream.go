package api

import (
	"fmt"
	"net/http"
	"sync"
	"time"
)

// 实时通道（SSE）。
//
// 此前「A 设备改了，B 设备要知道」只能等下一轮轮询：角标 5 分钟、提醒 30 秒。
// 在手机上这等于「另一个设备上做的事，这台看不见」——
// 于是干脆不看那台，改在本机重记一遍。这里给一条常驻的推送：
// 任何一端写入，其他端立刻对一次账。
//
// 刻意用 SSE 而不是 WebSocket：数据流是单向的（服务端 → 浏览器），
// SSE 走普通 HTTP，浏览器自带断线重连，反代与鉴权都不用另开一套。

// streamBroker 维护订阅者集合。
type streamBroker struct {
	mu      sync.Mutex
	clients map[chan string]struct{}
}

func newStreamBroker() *streamBroker {
	return &streamBroker{clients: map[chan string]struct{}{}}
}

func (b *streamBroker) add(c chan string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.clients[c] = struct{}{}
}

func (b *streamBroker) remove(c chan string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.clients, c)
}

// broadcast 把一条变更推给所有订阅者。慢的客户端直接丢消息 ——
// 它随后自己会拉一次快照，补不上也不影响别人。宁可丢一条，也不让一个卡住的
// 连接把整条广播堵死。
func (b *streamBroker) broadcast(msg string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for c := range b.clients {
		select {
		case c <- msg:
		default:
		}
	}
}

// streamEvents 处理 GET /api/stream：一路 SSE，直到客户端断开。
func (s *Server) streamEvents(w http.ResponseWriter, r *http.Request) error {
	// 用 ResponseController 而不是直接断言 Flusher：外层包着 statusWriter，
	// 断言会失败，controller 顺着 Unwrap 找得到底层的冲刷能力。
	rc := http.NewResponseController(w)
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	h.Set("Connection", "keep-alive")
	// Nginx 默认会缓冲响应，SSE 会被攒着不发 —— 这行是给反代看的。
	h.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	_ = rc.Flush()

	ch := make(chan string, 16)
	s.stream.add(ch)
	defer s.stream.remove(ch)

	// 心跳：空闲链路（手机息屏、切后台）容易被中间设备悄悄掐掉，
	// 定期写一行注释，链路保持活着。
	ticker := time.NewTicker(20 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-r.Context().Done():
			return nil
		case msg := <-ch:
			_, _ = fmt.Fprintf(w, "data: %s\n\n", msg)
			_ = rc.Flush()
		case <-ticker.C:
			_, _ = fmt.Fprint(w, ": ping\n\n")
			_ = rc.Flush()
		}
	}
}
