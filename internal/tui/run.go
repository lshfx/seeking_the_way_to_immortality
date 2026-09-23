package tui

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
	"unicode"

	"github.com/lshfx/seeking_the_way_to_immortality/internal/session"
)

// Console describes the small terminal surface the game needs. Prepare may
// enable ANSI output; its restore function must put the original mode back.
type Console interface {
	Prepare() (ConsoleInfo, func() error, error)
	Size() Size
	DiscardPendingInput() error
}

// ConsoleInfo contains detected capabilities, not guesses based on terminal
// names or color-related environment variables alone.
type ConsoleInfo struct {
	Interactive bool
	ANSI        bool
	ColorLevel  int // 16, 256 or 24 for truecolor; zero when colors are unavailable.
}

// Run renders and handles one input at a time. It leaves line input, the
// visible cursor and the normal screen buffer untouched, so EOF and Ctrl+C do
// not strand the terminal in a hidden or non-echoing mode.
func Run(ctx context.Context, input io.Reader, output io.Writer, app *session.Session, console Console) (runErr error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if input == nil || output == nil || app == nil || console == nil {
		return errors.New("TUI requires input, output, session and console")
	}
	info, restore, err := console.Prepare()
	if err != nil {
		return err
	}
	if restore != nil {
		defer func() {
			if err := restore(); err != nil && runErr == nil {
				runErr = fmt.Errorf("restore terminal mode: %w", err)
			}
		}()
	}
	if info.ANSI {
		defer func() {
			if _, err := io.WriteString(output, "\x1b[0m\r\n"); err != nil && runErr == nil {
				runErr = err
			}
		}()
	}
	if !info.Interactive {
		return errors.New("interactive terminal input and output are required")
	}
	if err := console.DiscardPendingInput(); err != nil {
		return fmt.Errorf("clear startup terminal input: %w", err)
	}

	// Restrict each underlying read to one byte. Console line discipline still
	// waits for Enter, while this prevents bufio from pre-reading later pasted
	// menu lines before the current action is committed. Keep exactly one input
	// reader alive so size changes can redraw while the player is thinking.
	readerCtx, stopReader := context.WithCancel(ctx)
	defer stopReader()
	reader := bufio.NewReaderSize(singleByteReader{reader: input}, 1)
	var inputLines <-chan inputResult
	var resumeInput chan<- struct{}
	discardQueuedInput := func() error {
		err := console.DiscardPendingInput()
		if err != nil {
			return fmt.Errorf("clear pending terminal input: %w", err)
		}
		return nil
	}
	resizeCheck := time.NewTicker(500 * time.Millisecond)
	defer resizeCheck.Stop()
	var drawnSize Size
	needsRender := true
	for {
		if err := ctx.Err(); err != nil {
			return nil
		}
		if needsRender {
			drawnSize = console.Size()
			model := app.Model()
			settings := app.Settings()
			inputHint := "输入快捷键后按回车；q 退出并保留进度"
			if app.TextInputActive() {
				inputHint = "文本输入中：q是普通文本；输入 :cancel 取消"
			}
			colorOK := info.ANSI && settings.ColorMode != "none"
			colorLevel := info.ColorLevel
			if settings.ColorMode == "basic" {
				colorLevel = 16
			}
			if err := Render(output, model, RenderOptions{
				Size: drawnSize, ColorMode: settings.ColorMode, Theme: settings.Theme,
				ColorOK: colorOK, ColorLevel: colorLevel, Clear: info.ANSI, InputHint: inputHint,
			}); err != nil {
				return fmt.Errorf("render screen: %w", err)
			}
			needsRender = false
		}
		if inputLines == nil {
			inputLines, resumeInput = readInputLines(readerCtx, reader)
		}
		select {
		case <-ctx.Done():
			return nil
		case <-resizeCheck.C:
			if nextSize := console.Size(); nextSize != drawnSize {
				needsRender = true
			}
		case result, ok := <-inputLines:
			if !ok || errors.Is(result.err, io.EOF) {
				return nil // EOF is normal; an unterminated partial key never runs.
			}
			if result.err != nil {
				return fmt.Errorf("read menu input: %w", result.err)
			}
			if app.TextInputActive() {
				if err := app.HandleText(result.line); err != nil {
					return fmt.Errorf("handle creation text: %w", err)
				}
				if err := discardQueuedInput(); err != nil {
					return err
				}
				resumeInputReader(readerCtx, resumeInput)
				needsRender = true
				continue
			}
			key, err := ParseKey(result.line)
			if err != nil {
				app.Notify("一次只接受一个菜单键；本次输入已忽略。")
				if err := discardQueuedInput(); err != nil {
					return err
				}
				resumeInputReader(readerCtx, resumeInput)
				needsRender = true
				continue
			}
			if key == "" {
				app.Notify("空输入没有推进游戏时间。")
				if err := discardQueuedInput(); err != nil {
					return err
				}
				resumeInputReader(readerCtx, resumeInput)
				needsRender = true
				continue
			}
			if console.Size() != drawnSize {
				app.Notify("窗口尺寸已变化；本次按键未执行，请按新画面重新选择。")
				if err := discardQueuedInput(); err != nil {
					return err
				}
				resumeInputReader(readerCtx, resumeInput)
				needsRender = true
				continue
			}
			if (drawnSize.Width < MinWidth || drawnSize.Height < MinHeight) && key != "q" {
				app.Notify("终端窗口过小；请先扩大窗口。本次输入没有执行。")
				if err := discardQueuedInput(); err != nil {
					return err
				}
				resumeInputReader(readerCtx, resumeInput)
				needsRender = true
				continue
			}
			exit, err := app.HandleKey(key)
			if err != nil {
				return fmt.Errorf("handle menu input: %w", err)
			}
			if exit {
				return nil
			}
			if err := discardQueuedInput(); err != nil {
				return err
			}
			resumeInputReader(readerCtx, resumeInput)
			needsRender = true
		}
	}
}

type inputResult struct {
	line string
	err  error
}

func readInputLines(ctx context.Context, reader *bufio.Reader) (<-chan inputResult, chan<- struct{}) {
	result := make(chan inputResult)
	resume := make(chan struct{})
	go func() {
		defer close(result)
		for {
			line, err := reader.ReadString('\n')
			select {
			case result <- inputResult{line: line, err: err}:
			case <-ctx.Done():
				return
			}
			if err != nil {
				return
			}
			select {
			case <-resume:
			case <-ctx.Done():
				return
			}
		}
	}()
	return result, resume
}

func resumeInputReader(ctx context.Context, resume chan<- struct{}) {
	select {
	case resume <- struct{}{}:
	case <-ctx.Done():
	}
}

// ParseKey accepts one printable character only. A pasted command string is
// rejected instead of being interpreted as several menu choices.
func ParseKey(line string) (string, error) {
	line = strings.TrimSpace(line)
	if line == "" {
		return "", nil
	}
	runes := []rune(line)
	if len(runes) != 1 || unicode.IsControl(runes[0]) {
		return "", errors.New("expected exactly one printable menu key")
	}
	return strings.ToLower(line), nil
}

type singleByteReader struct{ reader io.Reader }

func (r singleByteReader) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	return r.reader.Read(p[:1])
}
