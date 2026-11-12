package errloop

import (
	"errors"
	"testing"
)

type loopErr struct{ self error }

func (e *loopErr) Error() string { return "loop" }
func (e *loopErr) Unwrap() error { return e.self }

func TestIsOnCycle(t *testing.T) {
	t.Skip("教学反面示例：自引用 Unwrap 会让 errors.Is 永不返回；请按正文用 go test -run TestIsOnCycle -timeout=2s 手动观察")
	e := &loopErr{}
	e.self = e // 自引用成环
	_ = errors.Is(e, errors.New("target"))
}
