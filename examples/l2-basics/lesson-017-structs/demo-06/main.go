package main

import "fmt"

type Logger struct {
	Prefix string
}

type Service struct {
	*Logger // 嵌入指针：字段提升照样生效，但可能是 nil
	Name    string
}

func logMsg(s Service, msg string) {
	if s.Logger == nil { // 真边界：忘了初始化嵌入指针
		fmt.Println("[默认]", s.Name, msg)
		return
	}
	fmt.Println("["+s.Prefix+"]", s.Name, msg)
}

func main() {
	s1 := Service{Logger: &Logger{Prefix: "INFO"}, Name: "api"}
	logMsg(s1, "started")

	s2 := Service{Name: "job"} // 忘了初始化嵌入指针
	logMsg(s2, "started")      // 走判空分支，不 panic
}
