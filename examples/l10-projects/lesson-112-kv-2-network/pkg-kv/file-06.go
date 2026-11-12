package kv

import (
	"errors"
	"io"
	"log"
	"net"
	"sync"
	"time"
)

type Server struct {
	ln      net.Listener
	st      Storage
	metrics NetMetrics
	mu      sync.Mutex
	conns   map[net.Conn]struct{}
	wg      sync.WaitGroup
	quit    chan struct{}
	once    sync.Once
	readTO  time.Duration
	writeTO time.Duration
}

func NewServer(addr string, st Storage) (*Server, error) {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, err
	}
	return &Server{
		ln:      ln,
		st:      st,
		conns:   make(map[net.Conn]struct{}),
		quit:    make(chan struct{}),
		readTO:  30 * time.Second,
		writeTO: 5 * time.Second,
	}, nil
}

func (s *Server) Addr() string { return s.ln.Addr().String() }

func (s *Server) Serve() error {
	for {
		conn, err := s.ln.Accept()
		if err != nil {
			select {
			case <-s.quit:
				s.wg.Wait()
				return nil
			default:
				return err
			}
		}
		s.mu.Lock()
		s.conns[conn] = struct{}{}
		s.mu.Unlock()
		s.metrics.incConn()
		s.wg.Add(1)
		go func(c net.Conn) {
			defer s.wg.Done()
			defer s.metrics.decConn()
			defer func() {
				_ = c.Close()
				s.mu.Lock()
				delete(s.conns, c)
				s.mu.Unlock()
			}()
			s.handleConn(c)
		}(conn)
	}
}

func (s *Server) handleConn(c net.Conn) {
	for {
		_ = c.SetReadDeadline(time.Now().Add(s.readTO))
		start := time.Now()
		fr, err := readFrame(c)
		if err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, net.ErrClosed) {
				return // 客户端正常关闭或服务端主动收尾
			}
			// Close 会给活动连接设立即过期 Deadline 来唤醒读循环，
			// 这类超时是主动收尾而非故障，不能按错误日志打出去。
			select {
			case <-s.quit:
				return
			default:
			}
			s.metrics.Record(err, time.Since(start))
			log.Printf("read frame: %v", err)
			return
		}
		s.metrics.Record(nil, time.Since(start))
		if fr.Status == statusPing {
			s.metrics.pings.Add(1)
			_ = c.SetWriteDeadline(time.Now().Add(s.writeTO))
			if err := writeFrame(c, Frame{Status: statusPong, ReqID: fr.ReqID}); err != nil {
				return
			}
			continue
		}
		res, applyErr := s.st.Apply(Command{Op: fr.Op, Key: fr.Key, Value: fr.Value})
		out := Frame{Op: fr.Op, Status: statusOK, ReqID: fr.ReqID, Key: fr.Key}
		if applyErr != nil {
			out.Status = statusError
			out.ErrCode = encodeErr(applyErr)
		} else {
			out.Value = res.Value
			out.Found = res.Found
		}
		_ = c.SetWriteDeadline(time.Now().Add(s.writeTO))
		if err := writeFrame(c, out); err != nil {
			return
		}
	}
}

// Close 先关监听器拒绝新连接，再给所有活动连接设立即过期 Deadline
// 强制读循环退出，最后等待全部 goroutine 收尾。
func (s *Server) Close() error {
	var err error
	s.once.Do(func() {
		close(s.quit)
		err = s.ln.Close()
		s.mu.Lock()
		for c := range s.conns {
			_ = c.SetDeadline(time.Now())
		}
		s.mu.Unlock()
	})
	s.wg.Wait()
	return err
}
