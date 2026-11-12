package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

func newRootCmd() *cobra.Command {
	var cfgFile string
	var asJSON bool
	root := &cobra.Command{
		Use:           "todo",
		Short:         "本地待办清单",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.PersistentFlags().StringVar(&cfgFile, "config", "", "配置文件路径")
	root.PersistentFlags().BoolVar(&asJSON, "json", false, "以 JSON 输出结果")

	add := &cobra.Command{
		Use:   "add [标题]",
		Short: "新增一条待办",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Println("新增:", args[0])
			return nil
		},
	}
	list := &cobra.Command{
		Use:   "list",
		Short: "列出待办",
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Println("列出全部待办, json =", asJSON)
			return nil
		},
	}
	done := &cobra.Command{
		Use:   "done [id]",
		Short: "标记完成",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Println("完成:", args[0])
			return nil
		},
	}
	root.AddCommand(add, list, done)
	return root
}

func main() {
	if err := newRootCmd().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "错误:", err)
		os.Exit(1)
	}
}
