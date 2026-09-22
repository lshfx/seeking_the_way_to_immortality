// Package cli owns process arguments and text streams. It must not contain
// game rules or persistence logic.
package cli

import (
	"fmt"
	"io"
	"runtime"
)

const usage = `问道长生 - 本地终端修仙游戏

用法:
  wendao [选项]

选项:
  --help, -h       显示帮助
  --version, -v    显示版本
  --diagnose       显示离线运行环境诊断

当前工程只完成技术骨架，游戏功能尚未开放。`

// Run executes the process-level command and returns an exit code.
func Run(args []string, stdout, stderr io.Writer, version string) int {
	if len(args) == 0 {
		fmt.Fprintln(stdout, "《问道长生》工程骨架已启动；游戏功能尚未开放。")
		fmt.Fprintln(stdout, "运行 wendao --help 查看当前可用命令。")
		return 0
	}

	if len(args) > 1 {
		fmt.Fprintln(stderr, "参数过多；运行 wendao --help 查看用法。")
		return 2
	}

	switch args[0] {
	case "--help", "-h":
		fmt.Fprintln(stdout, usage)
		return 0
	case "--version", "-v":
		fmt.Fprintf(stdout, "wendao %s (%s/%s)\n", version, runtime.GOOS, runtime.GOARCH)
		return 0
	case "--diagnose":
		fmt.Fprintln(stdout, "product=wendao")
		fmt.Fprintf(stdout, "version=%s\n", version)
		fmt.Fprintf(stdout, "platform=%s/%s\n", runtime.GOOS, runtime.GOARCH)
		fmt.Fprintf(stdout, "go_runtime=%s\n", runtime.Version())
		fmt.Fprintln(stdout, "network=disabled")
		fmt.Fprintln(stdout, "game_state=not_implemented")
		return 0
	default:
		fmt.Fprintf(stderr, "未知参数：%s\n", args[0])
		fmt.Fprintln(stderr, "运行 wendao --help 查看用法。")
		return 2
	}
}
