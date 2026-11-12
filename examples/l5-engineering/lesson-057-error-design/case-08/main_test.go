package errloop

import (
	"errors"
	"testing"
)

type loopErr struct{ self error }

func (e *loopErr) Error() string { return "loop" }
func (e *loopErr) Unwrap() error { return e.self }

func TestIsOnCycle(t *testing.T) {
	e := &loopErr{}
	e.self = e // 自引用成环
	_ = errors.Is(e, errors.New("target"))
}
