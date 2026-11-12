package kv

import (
	"fmt"
	"net"
	"sync"
	"time"
)

var _ Storage = (*Client)(nil)

// Client 是一个远程 Storage：调用方拿到的仍是 Apply(Command)。
// 单连接上请求严格串行（用 mu 保证帧不交错），并行度交给连接池。
type Client struct {
	mu      sync.Mutex
	conn    net.Conn
	next    uint32
	ioTO    time.Duration
	hbEvery time.Duration
	lastAct time.Time
	closed  bool
}

func Dial(addr string) (*Client, error) {
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		return nil, err
	}
	return &Client{
		conn:    conn,
		ioTO:    3 * time.Second,
		hbEvery: 10 * time.Second,
		lastAct: time.Now(),
	}, nil
}

func (c *Client) Apply(cmd Command) (Result, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed || c.conn == nil {
		return Result{}, ErrClosed
	}
	if err := c.maybeHeartbeat(); err != nil {
		return Result{}, err
	}
	c.next++
	id := c.next
	_ = c.conn.SetDeadline(time.Now().Add(c.ioTO))
	in := Frame{Op: cmd.Op, Status: statusRequest, ReqID: id, Key: cmd.Key, Value: cmd.Value}
	if err := writeFrame(c.conn, in); err != nil {
		return Result{}, err
	}
	out, err := readFrame(c.conn)
	if err != nil {
		return Result{}, err
	}
	if out.ReqID != id || out.Status == statusRequest {
		return Result{}, fmt.Errorf("%w: mismatched response", ErrBadFrame)
	}
	if out.Status == statusError {
		return Result{}, decodeErr(out.ErrCode)
	}
	c.lastAct = time.Now()
	return Result{Value: out.Value, Found: out.Found}, nil
}

// maybeHeartbeat 在连接空闲超过阈值时先发 PING 探活，
// 避免半死连接拖到业务请求才暴露。
func (c *Client) maybeHeartbeat() error {
	if time.Since(c.lastAct) < c.hbEvery {
		return nil
	}
	c.next++
	id := c.next
	_ = c.conn.SetDeadline(time.Now().Add(c.ioTO))
	if err := writeFrame(c.conn, Frame{Status: statusPing, ReqID: id}); err != nil {
		return err
	}
	pong, err := readFrame(c.conn)
	if err != nil {
		return err
	}
	if pong.Status != statusPong || pong.ReqID != id {
		return fmt.Errorf("%w: bad pong", ErrBadFrame)
	}
	c.lastAct = time.Now()
	return nil
}

func (c *Client) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return nil
	}
	c.closed = true
	if c.conn == nil {
		return nil
	}
	err := c.conn.Close()
	c.conn = nil
	return err
}
