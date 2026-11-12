// user_test.go —— 单独一个文件即可 go test 通过
package user

import (
	"fmt"
	"testing"
)

// Repo 是业务层声明的接口：替换缝隙由使用方定义。
type Repo interface {
	NameByID(id int) (string, error)
}

// Service 只依赖接口，真实实现与测试替身都从构造函数进来。
type Service struct{ repo Repo }

func New(r Repo) Service { return Service{repo: r} }

func (s Service) Greet(id int) (string, error) {
	name, err := s.repo.NameByID(id)
	if err != nil {
		return "", fmt.Errorf("问候失败: %w", err)
	}
	return "你好, " + name, nil
}

// fakeRepo 是手写替身：只需固定返回值时，它最直观。
type fakeRepo struct {
	data map[int]string
	err  error
}

func (f fakeRepo) NameByID(id int) (string, error) {
	if f.err != nil {
		return "", f.err
	}
	return f.data[id], nil
}

func TestGreet(t *testing.T) {
	cases := []struct {
		name string
		repo Repo
		want string
	}{
		{"正常", fakeRepo{data: map[int]string{1: "ada"}}, "你好, ada"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := New(c.repo).Greet(1)
			if err != nil {
				t.Fatalf("不应出错: %v", err)
			}
			if got != c.want {
				t.Errorf("got %q, want %q", got, c.want)
			}
		})
	}
}
