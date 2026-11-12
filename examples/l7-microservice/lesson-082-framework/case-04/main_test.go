package main

import (
	"testing"
	"time"
)

type Options struct {
	Addr    string
	Timeout time.Duration
}

type Option func(*Options)

func WithAddr(a string) Option {
	return func(o *Options) { o.Addr = a }
}

func WithTimeout(d time.Duration) Option {
	return func(o *Options) { o.Timeout = d }
}

func New(opts ...Option) Options {
	o := Options{Addr: ":8080", Timeout: 2 * time.Second}
	for _, apply := range opts {
		apply(&o)
	}
	return o
}

func TestOptionOrderIrrelevant(t *testing.T) {
	a := New(WithAddr(":9090"), WithTimeout(500*time.Millisecond))
	b := New(WithTimeout(500*time.Millisecond), WithAddr(":9090"))
	if a != b {
		t.Fatalf("Option 顺序不应影响结果")
	}
}
