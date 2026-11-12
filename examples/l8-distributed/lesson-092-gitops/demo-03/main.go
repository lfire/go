package main

import "fmt"

// State 是镜像与副本数构成的最小状态
type State struct {
	ImageTag string
	Replicas int
}

// Drift 比较 Git 声明与集群实际状态
func Drift(desired, actual State) bool {
	return desired.ImageTag != actual.ImageTag || desired.Replicas != actual.Replicas
}

// Sync 把实际状态对齐回声明状态，模拟 Argo CD 的自愈
func Sync(desired, actual State) State {
	fmt.Println("检测到漂移，同步回声明状态:", desired.ImageTag)
	return desired
}

func main() {
	declared := State{ImageTag: "v1.2.3", Replicas: 5}
	running := State{ImageTag: "v1.2.3", Replicas: 5}
	fmt.Println("一致时存在漂移:", Drift(declared, running))

	// 模拟有人手工 kubectl edit 改了镜像
	running.ImageTag = "v1.3.0-hotfix"
	fmt.Println("手工改动后存在漂移:", Drift(declared, running))

	// 回滚的正确做法：把 Git 声明改回旧版本，控制器自动同步
	rolled := State{ImageTag: "v1.2.2", Replicas: 5}
	running = Sync(rolled, running)
	fmt.Println("回滚后镜像:", running.ImageTag, "，与声明一致:", !Drift(rolled, running))
}
