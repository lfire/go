package main

import "fmt"

type provider struct {
	out string
	in  []string
}

func resolve(want string, ps []provider) ([]string, error) {
	byOut := map[string]provider{}
	for _, p := range ps {
		byOut[p.out] = p
	}
	visited, order := map[string]bool{}, []string{}
	var visit func(t string, stack []string) error
	visit = func(t string, stack []string) error {
		for _, s := range stack {
			if s == t {
				return fmt.Errorf("cycle: %v -> %s", stack, t)
			}
		}
		if visited[t] {
			return nil
		}
		p, ok := byOut[t]
		if !ok {
			return fmt.Errorf("no provider found for %s", t)
		}
		for _, dep := range p.in {
			if err := visit(dep, append(append([]string{}, stack...), t)); err != nil {
				return err
			}
		}
		visited[t] = true
		order = append(order, fmt.Sprintf("%s(%v)", p.out, p.in))
		return nil
	}
	if err := visit(want, nil); err != nil {
		return nil, err
	}
	return order, nil
}

func main() {
	good := []provider{
		{"Config", nil},
		{"*DB", []string{"Config"}},
		{"*Repo", []string{"*DB", "Config"}},
		{"*Service", []string{"*Repo"}},
	}
	o, err := resolve("*Service", good)
	fmt.Println("正常图生成顺序:", o, "err =", err)

	missing := []provider{{"Config", nil}, {"*DB", []string{"Config"}}, {"*Service", []string{"*Repo"}}}
	_, err = resolve("*Service", missing)
	fmt.Println("缺 provider:", err)

	cyc := []provider{{"A", []string{"B"}}, {"B", []string{"A"}}}
	_, err = resolve("A", cyc)
	fmt.Println("成环:", err)
}
