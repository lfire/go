package main

import (
	"bytes"
	"fmt"
	"text/template"
)

// Values 模拟 Helm 的 values.yaml，环境差异都收在这里
type Values struct {
	Env      string
	Replicas int
	ImageTag string
}

const deploymentTpl = `apiVersion: apps/v1
kind: Deployment
metadata:
  name: myservice-{{ .Env }}
spec:
  replicas: {{ .Replicas }}
  template:
    spec:
      containers:
        - name: app
          image: myservice:{{ .ImageTag }}
`

func main() {
	chart := template.Must(template.New("deployment").Parse(deploymentTpl))
	for _, v := range []Values{
		{Env: "staging", Replicas: 1, ImageTag: "v1.3.0-rc1"},
		{Env: "prod", Replicas: 5, ImageTag: "v1.2.3"},
	} {
		var buf bytes.Buffer
		if err := chart.Execute(&buf, v); err != nil {
			fmt.Println("渲染失败:", err)
			continue
		}
		fmt.Printf("--- %s 环境 ---\n%s", v.Env, buf.String())
	}
}
