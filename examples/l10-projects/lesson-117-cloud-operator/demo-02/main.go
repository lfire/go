// 文件：api/types.go
package main

import (
	"errors"
	"fmt"
	"regexp"
)

// Phase 是 CR 的生命周期相位，取值集合与 CRD 的 enum 一一对应。
type Phase string

const (
	PhasePending   Phase = "Pending"
	PhaseRunning   Phase = "Running"
	PhaseUpgrading Phase = "Upgrading"
	PhaseDegraded  Phase = "Degraded"
)

// Condition 是一条状态条件，同 type 只保留最新一条，便于机器判定。
type Condition struct {
	Type    string
	Status  string
	Reason  string
	Message string
}

// Spec 是用户声明的期望，Status 是控制器回写的实际，二者严格分离。
type Spec struct {
	Replicas int
	Version  string
	Image    string
}

type Status struct {
	Phase              Phase
	ObservedGeneration int64
	ReadyReplicas      int
	Conditions         []Condition
}

// KVApp 是完整的自定义资源；DeletionTimestamp 非空表示进入删除流程。
type KVApp struct {
	Name              string
	Generation        int64
	ResourceVersion   string
	DeletionTimestamp bool
	Finalizers        []string
	Spec              Spec
	Status            Status
}

var semver = regexp.MustCompile(`^v[0-9]+\.[0-9]+\.[0-9]+$`)

// Validate 在对象落库前挡住非法期望，纯函数、无 IO、可表驱动测试。
func Validate(s Spec) error {
	if s.Replicas < 1 || s.Replicas > 100 {
		return errors.New("replicas out of range [1,100]")
	}
	if !semver.MatchString(s.Version) {
		return errors.New("version must match vMAJOR.MINOR.PATCH")
	}
	if s.Image == "" {
		return errors.New("image is required")
	}
	return nil
}

// SetCondition 按 type 覆盖写入，重复调用不产生重复条件，天然幂等。
func SetCondition(st *Status, c Condition) {
	for i := range st.Conditions {
		if st.Conditions[i].Type == c.Type {
			st.Conditions[i] = c
			return
		}
	}
	st.Conditions = append(st.Conditions, c)
}

func main() {
	good := Spec{Replicas: 3, Version: "v1.4.0", Image: "kv:v1.4.0"}
	bad := Spec{Replicas: 0, Version: "latest", Image: ""}
	fmt.Println("good err is nil:", Validate(good) == nil)
	fmt.Println("bad:", Validate(bad))
	st := &Status{}
	SetCondition(st, Condition{Type: "Ready", Status: "True", Reason: "Converged"})
	SetCondition(st, Condition{Type: "Ready", Status: "False", Reason: "Progressing"})
	fmt.Println("conditions:", len(st.Conditions), st.Conditions[0].Reason)
}
