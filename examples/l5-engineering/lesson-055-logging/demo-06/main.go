package main

import (
	"context"
	"fmt"
	"log/slog"
)

type probe struct {
	groups []string
	attrs  []slog.Attr
}

func (p *probe) Enabled(context.Context, slog.Level) bool { return true }
func (p *probe) Handle(_ context.Context, r slog.Record) error {
	var out []string
	r.Attrs(func(a slog.Attr) bool { out = append(out, a.Key+"="+a.Value.String()); return true })
	fmt.Printf("Handle: groups=%v 预置=%v 本次=%v\n", p.groups, keys(p.attrs), out)
	return nil
}
func (p *probe) WithAttrs(as []slog.Attr) slog.Handler {
	fmt.Printf("WithAttrs: 进来 groups=%v attrs=%v\n", p.groups, keys(as))
	q := *p
	q.attrs = append(append([]slog.Attr{}, p.attrs...), as...)
	return &q
}
func (p *probe) WithGroup(name string) slog.Handler {
	fmt.Printf("WithGroup: name=%s 之前 groups=%v\n", name, p.groups)
	q := *p
	q.groups = append(append([]string{}, p.groups...), name)
	return &q
}
func keys(as []slog.Attr) []string {
	var r []string
	for _, a := range as {
		r = append(r, a.Key+"="+a.Value.String())
	}
	return r
}

func main() {
	log := slog.New(&probe{})
	log = log.With("svc", "order").WithGroup("req").With("trace", "t-1")
	log.Info("created", "id", 7, "phone", "13800138000")
}
