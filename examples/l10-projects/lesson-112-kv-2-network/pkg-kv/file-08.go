package kv

import "sync/atomic"

var _ Storage = (*Pool)(nil)

// Pool 用 N 条独立连接把单连接串行的 Client 拼成可并行的远程 Storage。
// 借用与归还走有界 channel，池空时阻塞形成背压，不无限建连。
type Pool struct {
	addr    string
	clients chan *Client
	closed  atomic.Bool
}

func NewPool(addr string, size int) (*Pool, error) {
	if size <= 0 {
		size = 8
	}
	p := &Pool{addr: addr, clients: make(chan *Client, size)}
	for i := 0; i < size; i++ {
		c, err := Dial(addr)
		if err != nil {
			for j := 0; j < i; j++ {
				cc := <-p.clients
				_ = cc.Close()
			}
			return nil, err
		}
		p.clients <- c
	}
	return p, nil
}

func (p *Pool) Apply(cmd Command) (Result, error) {
	if p.closed.Load() {
		return Result{}, ErrClosed
	}
	c := <-p.clients
	res, err := c.Apply(cmd)
	// 传输层错误（超时、坏帧、断连）说明这条连接已不可信：
	// 关掉并同步补一条新的，绝不让毒连接回到池里。
	if err != nil && !isBusinessErr(err) {
		_ = c.Close()
		if nc, derr := Dial(p.addr); derr == nil {
			p.clients <- nc
		} else {
			p.clients <- c // 补连失败时保留已关闭连接，使后续调用快速失败
		}
		return res, err
	}
	p.clients <- c
	return res, err
}

func (p *Pool) Close() error {
	if !p.closed.CompareAndSwap(false, true) {
		return nil
	}
	var first error
	for i := 0; i < cap(p.clients); i++ {
		c := <-p.clients
		if c != nil {
			if err := c.Close(); err != nil && first == nil {
				first = err
			}
		}
	}
	return first
}
