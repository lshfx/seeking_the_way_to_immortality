package tui

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/lshfx/seeking_the_way_to_immortality/internal/panel"
	"github.com/lshfx/seeking_the_way_to_immortality/internal/session"
	"github.com/lshfx/seeking_the_way_to_immortality/internal/storage"
)

type fakeConsole struct {
	info         ConsoleInfo
	size         Size
	sizes        []Size
	sizeCalls    int
	restored     bool
	discardCnt   int
	discardErr   error
	discardErrAt int
}

func (c *fakeConsole) Prepare() (ConsoleInfo, func() error, error) {
	return c.info, func() error { c.restored = true; return nil }, nil
}
func (c *fakeConsole) Size() Size {
	if len(c.sizes) > 0 {
		index := c.sizeCalls
		c.sizeCalls++
		if index >= len(c.sizes) {
			index = len(c.sizes) - 1
		}
		return c.sizes[index]
	}
	return c.size
}
func (c *fakeConsole) DiscardPendingInput() error {
	c.discardCnt++
	if c.discardErrAt > 0 && c.discardCnt != c.discardErrAt {
		return nil
	}
	return c.discardErr
}

func TestRendererKeepsEveryRiskAndChoiceVisibleAtSupportedSizes(t *testing.T) {
	model := panel.Model{
		SchemaVersion: panel.SchemaVersion,
		Title:         "问道长生",
		Calendar:      panel.Calendar{Text: "天玄历 387 年 正月", AgeText: "21 岁"},
		Scene:         "青崖洞府",
		Status: panel.StatusBlock{
			RealmText: "炼气", TierText: "初期",
			HP: panel.VitalsView{Text: "40 / 40"}, MP: panel.VitalsView{Text: "20 / 20"},
			XP:        panel.ProgressView{Text: "0 / 100"},
			Resources: []panel.ResourceView{{Label: "灵石", Value: 100}},
			Mood:      panel.TextValue{Label: "心境", Text: "50 / 100"},
		},
		Event: &panel.EventBlock{
			Title:     "山道遇险",
			Narration: "山石滚落，前路狭窄。你可以谨慎绕行，也可以冒险穿过。",
			Choices: []panel.EventChoiceView{
				{Text: "绕行", CostText: "耗时 1 月"},
				{Text: "穿过碎石", CostText: "耗时 1 月", RiskText: "受伤 30%"},
				{Text: "停下观察", CostText: "不耗时"},
				{Text: "原路退回", CostText: "不耗时", RiskText: "损失草药 10%"},
			},
		},
		Save:    panel.SaveBlock{State: "durable", Text: "进度已安全保存。"},
		Changes: []panel.ChangeLine{{Text: "修为 +19.5"}},
	}
	for _, size := range []Size{{Width: 80, Height: 24}, {Width: 64, Height: 20}, {Width: 48, Height: 16}} {
		t.Run(fmt.Sprintf("%dx%d", size.Width, size.Height), func(t *testing.T) {
			var out bytes.Buffer
			if err := Render(&out, model, RenderOptions{Size: size, ColorMode: "none", Theme: "jade"}); err != nil {
				t.Fatal(err)
			}
			got := out.String()
			for _, want := range []string{"[1] 绕行", "[2] 穿过碎石", "[3] 停下观察", "[4] 原路退回", "风险：受伤 30%", "风险：损失草药 10%", "进度已安全保存"} {
				if !strings.Contains(got, want) {
					t.Errorf("%dx%d screen missing %q:\n%s", size.Width, size.Height, want, got)
				}
			}
			if strings.ContainsRune(got, '\x1b') || strings.ContainsRune(got, '\a') {
				t.Fatalf("plain-color render leaked a terminal control sequence: %q", got)
			}
		})
	}
}

func TestRendererSanitizesControlTextAndUsesRealANSIStyles(t *testing.T) {
	model := panel.Model{
		Title: "问道长生",
		Event: &panel.EventBlock{Title: "山道", Narration: "安全文本\x1b]52;c;clipboard\a继续", Choices: []panel.EventChoiceView{{Text: "离开", RiskText: "无"}}},
		Save:  panel.SaveBlock{State: "durable", Text: "已保存"},
	}
	var out bytes.Buffer
	if err := Render(&out, model, RenderOptions{Size: Size{Width: 80, Height: 24}, ColorMode: "basic", Theme: "jade", ColorOK: true, Clear: true}); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	if !strings.Contains(got, "\x1b[1;36m") {
		t.Fatalf("color-enabled mode did not emit a basic ANSI style: %q", got)
	}
	if strings.Contains(got, "\x1b]52") || strings.ContainsRune(got, '\a') {
		t.Fatalf("untrusted text reached terminal control channel: %q", got)
	}
}

func TestRendererUsesDetected256AndTruecolorLevels(t *testing.T) {
	model := panel.Model{Title: "问道长生"}
	for _, tc := range []struct {
		name  string
		level int
		want  string
	}{{"256-color", 256, "\x1b[38;5;43m"}, {"truecolor", 24, "\x1b[38;2;69;183;157m"}} {
		t.Run(tc.name, func(t *testing.T) {
			var out bytes.Buffer
			if err := Render(&out, model, RenderOptions{
				Size: Size{Width: 80, Height: 24}, ColorMode: "auto", Theme: "jade",
				ColorOK: true, ColorLevel: tc.level,
			}); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(out.String(), tc.want) {
				t.Fatalf("color level %d did not emit %q: %q", tc.level, tc.want, out.String())
			}
		})
	}
}

func TestSmallTerminalRequestsResizeAndOffersOnlySafeExit(t *testing.T) {
	var out bytes.Buffer
	model := panel.Model{Title: "问道长生", Options: []panel.Option{{Key: "1", Label: "修炼"}}}
	if err := Render(&out, model, RenderOptions{Size: Size{Width: 40, Height: 12}}); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	if !strings.Contains(got, "小于建议最小尺寸") || !strings.Contains(got, "q：退出") || strings.Contains(got, "[1] 修炼") {
		t.Fatalf("undersized terminal should prompt resize and avoid hiding active choices: %q", got)
	}
	out.Reset()
	if err := Render(&out, model, RenderOptions{
		Size: Size{Width: 40, Height: 12}, InputHint: "文本输入中：q是普通文本；输入 :cancel 取消",
	}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), "q：退出") || !strings.Contains(out.String(), ":cancel") {
		t.Fatalf("undersized text editor must explain literal q and the cancel input: %q", out.String())
	}
}

func TestWrapCellsPreservesUTF8AndWideRunes(t *testing.T) {
	lines := wrapCells("修炼abc", 5)
	if len(lines) != 2 || lines[0] != "修炼a" || lines[1] != "bc" {
		t.Fatalf("wide-cell wrapping = %#v", lines)
	}
	if got := clipCells("修炼abc", 3); got != "修" {
		t.Fatalf("cell clipping split or overflowed a wide rune: %q", got)
	}
}

func TestParseKeyRejectsPastedCommands(t *testing.T) {
	for _, input := range []string{"1 2", "12", "1\x1b[A", "hello"} {
		if _, err := ParseKey(input); err == nil {
			t.Errorf("ParseKey(%q) accepted more than one printable key", input)
		}
	}
	if key, err := ParseKey(" C "); err != nil || key != "c" {
		t.Fatalf("single key normalization = %q, %v", key, err)
	}
	if key, err := ParseKey("  "); err != nil || key != "" {
		t.Fatalf("empty input = %q, %v", key, err)
	}
}

func TestTUIRunsTheSavedShortLoopAndRestoresConsole(t *testing.T) {
	app, err := session.OpenAt(storage.LayoutFor(t.TempDir()), session.DefaultGameID)
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()
	console := &fakeConsole{info: ConsoleInfo{Interactive: true}, size: Size{Width: 80, Height: 24}}
	var output bytes.Buffer
	if err := Run(context.Background(), strings.NewReader("1\ne\n1\n云\nb\nc\n1\nq\n"), &output, app, console); err != nil {
		t.Fatal(err)
	}
	state := app.State()
	if state.Player == nil || state.Player.XP != 195000 || state.Counters.WorldMonth != 1 || state.Player.Identity.Surname != "云" {
		t.Fatalf("TUI input did not finish the creation/cultivation loop: %#v", state.Counters)
	}
	if !console.restored {
		t.Fatal("normal q exit must run terminal cleanup")
	}
	if strings.Contains(output.String(), "\x1b") {
		t.Fatal("fake non-ANSI console received escape sequences")
	}
	if !strings.Contains(output.String(), "预估 +19.5") || !strings.Contains(output.String(), "自动保存") || !strings.Contains(output.String(), "q是普通文本") {
		t.Fatalf("screen lacks the cultivation preview or save status: %q", output.String())
	}
}

func TestInputQueueClearFailureStopsBeforeReadingAnotherAction(t *testing.T) {
	app, err := session.OpenAt(storage.LayoutFor(t.TempDir()), session.DefaultGameID)
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()
	console := &fakeConsole{
		info:         ConsoleInfo{Interactive: true},
		size:         Size{Width: 80, Height: 24},
		discardErr:   errors.New("input queue unavailable"),
		discardErrAt: 2, // startup drain succeeds; the post-action drain fails.
	}
	var output bytes.Buffer
	err = Run(context.Background(), strings.NewReader("1\nc\n"), &output, app, console)
	if err == nil || !strings.Contains(err.Error(), "clear pending terminal input") {
		t.Fatalf("queue-clear failure should stop the UI, got %v", err)
	}
	if app.State().Phase != "CREATION" || app.State().Counters.WorldMonth != 0 {
		t.Fatalf("UI accepted another queued action after queue-clear failure: %#v", app.State())
	}
	if !console.restored {
		t.Fatal("queue-clear failure did not restore terminal state")
	}
}

func TestInputReaderWaitsForActionAcknowledgementBeforeReadingAhead(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	reader := bufio.NewReaderSize(singleByteReader{reader: strings.NewReader("1\n2\n")}, 1)
	lines, resume := readInputLines(ctx, reader)
	first := <-lines
	if first.err != nil || first.line != "1\n" {
		t.Fatalf("first input line = %#v", first)
	}
	select {
	case second := <-lines:
		t.Fatalf("reader passed the action barrier: %#v", second)
	case <-time.After(20 * time.Millisecond):
	}
	resumeInputReader(ctx, resume)
	select {
	case second := <-lines:
		if second.err != nil || second.line != "2\n" {
			t.Fatalf("second input line = %#v", second)
		}
	case <-time.After(time.Second):
		t.Fatal("reader did not resume after the previous action completed")
	}
}

func TestEOFAndCancellationRestoreConsoleWithoutRunningPartialCommand(t *testing.T) {
	t.Run("EOF", func(t *testing.T) {
		app, err := session.OpenAt(storage.LayoutFor(t.TempDir()), session.DefaultGameID)
		if err != nil {
			t.Fatal(err)
		}
		defer app.Close()
		before := app.State()
		console := &fakeConsole{info: ConsoleInfo{Interactive: true}, size: Size{Width: 80, Height: 24}}
		if err := Run(context.Background(), strings.NewReader("1"), io.Discard, app, console); err != nil {
			t.Fatal(err)
		}
		after := app.State()
		if after.Revision != before.Revision || !console.restored {
			t.Fatalf("EOF ran a partial command or skipped cleanup: rev=%d/%d restored=%t", before.Revision, after.Revision, console.restored)
		}
	})

	t.Run("signal context", func(t *testing.T) {
		app, err := session.OpenAt(storage.LayoutFor(t.TempDir()), session.DefaultGameID)
		if err != nil {
			t.Fatal(err)
		}
		defer app.Close()
		reader, writer := io.Pipe()
		defer reader.Close()
		defer writer.Close()
		ctx, cancel := context.WithCancel(context.Background())
		console := &fakeConsole{info: ConsoleInfo{Interactive: true}, size: Size{Width: 80, Height: 24}}
		done := make(chan error, 1)
		go func() { done <- Run(ctx, reader, io.Discard, app, console) }()
		time.AfterFunc(10*time.Millisecond, cancel)
		select {
		case err := <-done:
			if err != nil || !console.restored {
				t.Fatalf("cancellation cleanup err=%v restored=%t", err, console.restored)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("TUI did not respond to cancellation")
		}
	})
}

func TestResizeRedrawsWhileWaitingWithoutAdvancingGameTime(t *testing.T) {
	app, err := session.OpenAt(storage.LayoutFor(t.TempDir()), session.DefaultGameID)
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()
	before := app.State()
	reader, writer := io.Pipe()
	defer reader.Close()
	console := &fakeConsole{
		info:  ConsoleInfo{Interactive: true},
		sizes: []Size{{Width: 80, Height: 24}, {Width: 80, Height: 24}, {Width: 64, Height: 20}},
	}
	var output bytes.Buffer
	done := make(chan error, 1)
	go func() { done <- Run(context.Background(), reader, &output, app, console) }()
	time.AfterFunc(1100*time.Millisecond, func() { _ = writer.Close() })
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("TUI did not finish after resize-test input reached EOF")
	}
	if got := strings.Count(output.String(), "创建角色"); got < 2 {
		t.Fatalf("resizing while idle did not redraw the screen; title count=%d", got)
	}
	after := app.State()
	if after.Revision != before.Revision || after.Counters.WorldMonth != before.Counters.WorldMonth {
		t.Fatalf("resize advanced game state: revision %d to %d, month %d to %d", before.Revision, after.Revision, before.Counters.WorldMonth, after.Counters.WorldMonth)
	}
}
