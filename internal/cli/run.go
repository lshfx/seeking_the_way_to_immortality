// Package cli owns process arguments and text streams. It must not contain
// game rules or persistence logic.
package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"runtime"

	"github.com/lshfx/seeking_the_way_to_immortality/internal/session"
	"github.com/lshfx/seeking_the_way_to_immortality/internal/tui"
)

const usage = `问道长生 - 本地终端修仙游戏

用法:
  wendao [选项]

选项:
  --help, -h       显示帮助
  --version, -v    显示版本
  --diagnose       显示离线运行环境诊断

首轮可玩范围：创角、普通修炼、详情查看、自动保存与恢复。
运行 wendao 进入游戏；无开发环境的用户只需运行发布的可执行文件。`

// Run executes the process-level command and returns an exit code.
func Run(args []string, stdout, stderr io.Writer, version string) int {
	if len(args) == 0 {
		output, ok := stdout.(*os.File)
		if !ok || !tui.IsInteractive(os.Stdin, output) {
			fmt.Fprintln(stderr, "《问道长生》需要交互式终端。请在 Windows Terminal、VS Code 终端或支持的控制台中运行。")
			return 2
		}
		app, err := session.OpenDefault()
		if err != nil {
			fmt.Fprintf(stderr, "无法打开本地游戏会话：%v\n", err)
			return 1
		}
		defer func() { _ = app.Close() }()
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
		defer stop()
		if err := tui.Run(ctx, os.Stdin, output, app, tui.NativeConsole{Input: os.Stdin, Output: output}); err != nil {
			fmt.Fprintf(stderr, "终端界面启动失败：%v\n", err)
			return 1
		}
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
		fmt.Fprintln(stdout, "game_state=short_loop_implemented")
		return 0
	default:
		fmt.Fprintf(stderr, "未知参数：%s\n", args[0])
		fmt.Fprintln(stderr, "运行 wendao --help 查看用法。")
		return 2
	}
}
