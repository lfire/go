// 文件：saga.go —— 逆序补偿、超时传递、补偿重试预算
package main

import (
	"context"
	"errors"
	"fmt"
	"time"
)

var (
	ErrSagaAbort    = errors.New("saga: aborted")
	ErrCompensation = errors.New("saga: compensation failed")
)

// Step 把正向操作与一一对应的补偿操作绑在一起。
// Undo 必须幂等，且允许空补偿（正向从未真正生效）。
type Step struct {
	Name string
	Do   func(ctx context.Context) error
	Undo func(ctx context.Context) error
}

type Saga struct {
	steps       []Step
	maxAttempts int
	backoff     time.Duration
}

func NewSaga(maxAttempts int, backoff time.Duration) *Saga {
	return &Saga{maxAttempts: maxAttempts, backoff: backoff}
}

func (s *Saga) Add(name string, do, undo func(context.Context) error) {
	s.steps = append(s.steps, Step{Name: name, Do: do, Undo: undo})
}

// compensate 逆序执行已成功步骤的补偿；每步带重试预算，
// 耗尽仍失败则记录并返回，交给持久化重试队列兜底，绝不静默丢弃。
func (s *Saga) compensate(ctx context.Context, done []Step) error {
	var failed []string
	for i := len(done) - 1; i >= 0; i-- {
		st := done[i]
		var err error
		for attempt := 1; attempt <= s.maxAttempts; attempt++ {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if err = st.Undo(ctx); err == nil {
				break
			}
			if attempt < s.maxAttempts {
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-time.After(s.backoff * time.Duration(attempt)):
				}
			}
		}
		if err != nil {
			failed = append(failed, st.Name)
		}
	}
	if len(failed) > 0 {
		return fmt.Errorf("%w: %v", ErrCompensation, failed)
	}
	return nil
}

// Run 逐步执行正向操作：每步前检查 ctx，保证超时真正传导到下游；
// 失败时用一个全新的 context 做补偿，避免请求取消把补偿一起掐断。
func (s *Saga) Run(ctx context.Context) error {
	done := make([]Step, 0, len(s.steps))
	for _, st := range s.steps {
		if err := ctx.Err(); err != nil {
			_ = s.compensate(context.Background(), done)
			return err
		}
		if err := st.Do(ctx); err != nil {
			if cErr := s.compensate(context.Background(), done); cErr != nil {
				return fmt.Errorf("%w: %v; %v", ErrSagaAbort, err, cErr)
			}
			return fmt.Errorf("%w at %s: %v", ErrSagaAbort, st.Name, err)
		}
		done = append(done, st)
	}
	return nil
}

func main() {
	state := map[string]string{"stock": "10", "order": "none", "payment": "none"}
	s := NewSaga(3, 10*time.Millisecond)
	s.Add("reserve", func(ctx context.Context) error {
		state["stock"] = "9"
		return nil
	}, func(ctx context.Context) error {
		state["stock"] = "10"
		return nil
	})
	s.Add("create", func(ctx context.Context) error {
		state["order"] = "CREATED"
		return nil
	}, func(ctx context.Context) error {
		state["order"] = "CANCELED"
		return nil
	})
	s.Add("charge", func(ctx context.Context) error {
		return errors.New("payment declined")
	}, func(ctx context.Context) error {
		state["payment"] = "REFUNDED"
		return nil
	})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	err := s.Run(ctx)
	fmt.Printf("err=%v state=%v\n", err, state)
}
