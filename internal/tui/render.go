// Package tui renders read-only panel models and runs the synchronous local
// interaction loop. It does not calculate game rules.
package tui

import (
	"fmt"
	"io"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/lshfx/seeking_the_way_to_immortality/internal/panel"
)

const (
	MinWidth  = 48
	MinHeight = 16
)

// Size is a terminal's current character-cell dimensions.
type Size struct {
	Width  int
	Height int
}

// RenderOptions controls terminal presentation, never domain output.
type RenderOptions struct {
	Size       Size
	ColorMode  string
	Theme      string
	ColorOK    bool
	ColorLevel int
	Clear      bool
	InputHint  string
}

type screenLine struct {
	text     string
	role     string
	priority int // lower priority lines are dropped first when height is scarce
}

// Render writes one complete screen. Choices and the save state are protected
// lines: when space is short it drops history and optional detail first.
func Render(out io.Writer, model panel.Model, options RenderOptions) error {
	if out == nil {
		return fmt.Errorf("TUI output is nil")
	}
	size := options.Size
	if size.Width <= 0 {
		size.Width = 80
	}
	if size.Height <= 0 {
		size.Height = 24
	}
	if options.Clear {
		if _, err := io.WriteString(out, "\x1b[2J\x1b[H"); err != nil {
			return err
		}
	}

	if size.Width < MinWidth || size.Height < MinHeight {
		exitHint := "q：退出"
		resizeHint := "请扩大终端窗口后继续；进度已经安全保存。"
		if strings.Contains(options.InputHint, "q是普通文本") {
			exitHint = "文本输入中：输入 :cancel 取消；q是普通文本。"
			resizeHint = "请扩大终端窗口；当前字段需按回车后保存。"
		}
		lines := []string{
			"问道长生",
			fmt.Sprintf("当前窗口 %d×%d，小于建议最小尺寸 48×16。", size.Width, size.Height),
			resizeHint,
			exitHint,
		}
		return writeLines(out, lines, options, size.Width)
	}

	lines := buildLines(model, size.Width)
	maxRows := size.Height - 1 // reserve one row for the input hint
	for len(lines) > maxRows {
		remove := -1
		lowest := int(^uint(0) >> 1)
		for i, line := range lines {
			if line.priority < lowest {
				lowest, remove = line.priority, i
			}
		}
		if remove < 0 || lowest >= 90 {
			break
		}
		lines = append(lines[:remove], lines[remove+1:]...)
	}
	if len(lines) > maxRows {
		// This only happens if a caller supplies more protected choices than a
		// supported 16-row screen can display. Keep every key and make the size
		// limitation explicit instead of silently dropping a choice.
		lines = []screenLine{{text: model.Title, role: "title", priority: 100}}
		lines = append(lines, screenLine{text: "选项较多，扩大终端窗口后继续。", role: "warning", priority: 100})
		lines = append(lines, protectedOptions(model, size.Width-2)...)
		lines = append(lines, screenLine{text: model.Save.Text, role: "save", priority: 100})
	}
	plain := make([]string, 0, len(lines)+1)
	for _, line := range lines {
		plain = append(plain, paint(line.text, line.role, options))
	}
	inputHint := options.InputHint
	if inputHint == "" {
		inputHint = "输入快捷键后按回车；q 退出并保留进度"
	}
	plain = append(plain, paint(inputHint, "hint", options))
	return writeLines(out, plain, options, size.Width)
}

func buildLines(model panel.Model, width int) []screenLine {
	contentWidth := width - 2
	if contentWidth < 12 {
		contentWidth = 12
	}
	var lines []screenLine
	header := safeText(model.Title)
	if model.Calendar.Text != "" {
		header += "    " + safeText(model.Calendar.Text)
	}
	lines = append(lines, screenLine{text: clipCells(header, contentWidth), role: "title", priority: 100})

	if model.Creation != nil {
		creation := model.Creation
		lines = append(lines, screenLine{text: safeText(creation.Author) + "    步骤 " + fmt.Sprintf("%d/%d", creation.Step, creation.TotalSteps), role: "muted", priority: 100})
		if creation.SelectedPresetID != "" {
			selected := creation.SelectedPresetID
			for _, preset := range creation.Presets {
				if preset.ID == creation.SelectedPresetID {
					selected = preset.Label
					break
				}
			}
			lines = append(lines, screenLine{text: "当前预设：" + safeText(selected), role: "body", priority: 70})
		}
		if creation.BasePointsTotal > 0 {
			lines = append(lines, screenLine{text: fmt.Sprintf("基础属性点：%d/%d", creation.BasePointsSpent, creation.BasePointsTotal), role: "body", priority: 70})
		}
		for _, field := range creation.Fields {
			if field.Value == "" {
				continue
			}
			lines = appendWrapped(lines, field.Label+"："+field.Value, contentWidth, "body", 45)
		}
		for _, row := range creation.Summary {
			lines = appendWrapped(lines, row.Label+"："+row.Value, contentWidth, "body", 45)
		}
		if creation.Step == 1 {
			for i, preset := range creation.Presets {
				lines = appendWrapped(lines, fmt.Sprintf("预设 %d %s：%s", i+1, preset.Label, preset.SummaryText), contentWidth, "muted", 20)
			}
		}
	} else if model.Calendar.Text != "" {
		status1 := "境界 " + safeText(model.Status.RealmText) + "·" + safeText(model.Status.TierText)
		if model.Status.HP.Text != "" {
			status1 += "    气血 " + safeText(model.Status.HP.Text)
		}
		if model.Status.MP.Text != "" {
			status1 += "    灵力 " + safeText(model.Status.MP.Text)
		}
		lines = appendWrapped(lines, status1, contentWidth, "status", 95)
		status2 := "修为 " + safeText(model.Status.XP.Text) + " " + progressBar(model.Status.XP.RatioPercent, 8)
		for _, resource := range model.Status.Resources {
			status2 += "    " + safeText(resource.Label) + " " + fmt.Sprint(resource.Value)
		}
		if model.Status.Mood.Text != "" {
			status2 += "    心境 " + safeText(model.Status.Mood.Text)
		}
		lines = appendWrapped(lines, status2, contentWidth, "status", 90)
		status3 := strings.TrimSpace(safeText(model.Calendar.AgeText) + "    " + safeText(model.Calendar.LifespanRemainingText))
		if status3 != "" {
			lines = appendWrapped(lines, status3, contentWidth, "status", 80)
		}
		if model.Status.XP.Full {
			lines = append(lines, screenLine{text: "当前小阶修为已满；继续修炼会浪费月份，请先处理突破。", role: "warning", priority: 100})
		}
	}

	if model.Scene != "" {
		lines = append(lines, screenLine{text: "场景：" + safeText(model.Scene), role: "heading", priority: 90})
	}
	if model.Event != nil {
		lines = append(lines, screenLine{text: safeText(model.Event.Title), role: "heading", priority: 100})
		lines = appendWrapped(lines, model.Event.Narration, contentWidth, "body", 35)
	}
	if model.Breakthrough != nil {
		lines = append(lines, screenLine{text: safeText(model.Breakthrough.Title), role: "heading", priority: 100})
		lines = appendWrapped(lines, model.Breakthrough.Narration, contentWidth, "body", 35)
		if model.Breakthrough.SpentText != "" {
			lines = appendWrapped(lines, "已消耗："+model.Breakthrough.SpentText, contentWidth, "warning", 100)
		}
	}
	if model.Confirmation != nil {
		lines = append(lines, screenLine{text: safeText(model.Confirmation.Title), role: "heading", priority: 100})
		lines = appendWrapped(lines, model.Confirmation.Summary, contentWidth, "body", 60)
		if model.Confirmation.CostText != "" {
			lines = appendWrapped(lines, "代价："+model.Confirmation.CostText, contentWidth, "warning", 100)
		}
		if model.Confirmation.RiskText != "" {
			lines = appendWrapped(lines, "风险："+model.Confirmation.RiskText, contentWidth, "warning", 100)
		}
	}
	if model.Combat != nil {
		lines = append(lines, screenLine{text: "战斗第 " + fmt.Sprint(model.Combat.Round) + " 轮　" + safeText(model.Combat.TurnText), role: "heading", priority: 100})
		for _, entry := range model.Combat.Log {
			lines = appendWrapped(lines, entry, contentWidth, "body", 35)
		}
	}
	if model.Error != nil {
		lines = appendWrapped(lines, "错误："+model.Error.Text, contentWidth, "warning", 100)
	}
	for _, section := range model.Details {
		lines = append(lines, screenLine{text: safeText(section.Title), role: "heading", priority: 70})
		for _, row := range section.Rows {
			text := safeText(row.Label) + "：" + safeText(row.Value)
			if row.Hint != "" {
				text += "（" + safeText(row.Hint) + "）"
			}
			lines = appendWrapped(lines, text, contentWidth, "body", 15)
		}
		for _, text := range section.Lines {
			lines = appendWrapped(lines, text, contentWidth, "body", 15)
		}
	}
	for i, change := range model.Changes {
		if i > 1 {
			break
		}
		lines = appendWrapped(lines, "最近变化："+change.Text, contentWidth, "muted", 5)
	}
	if model.Save.Text != "" {
		role := "save"
		if model.Save.State == "failed" {
			role = "warning"
		}
		lines = appendWrapped(lines, "保存："+model.Save.Text, contentWidth, role, 100)
	}
	if model.Notice != nil && model.Notice.Text != "" {
		role := model.Notice.Kind
		if role != "error" && role != "warn" {
			role = "notice"
		}
		lines = appendWrapped(lines, safeText(model.Notice.Text), contentWidth, role, 75)
	}
	lines = append(lines, protectedOptions(model, contentWidth)...)
	return lines
}

func protectedOptions(model panel.Model, width int) []screenLine {
	var options []panel.Option
	if model.Event != nil && len(model.Event.Choices) > 0 {
		for i, choice := range model.Event.Choices {
			label := choice.Text
			if choice.CostText != "" {
				label += "；" + choice.CostText
			}
			if choice.RiskText != "" {
				label += "；风险：" + choice.RiskText
			}
			options = append(options, panel.Option{Key: fmt.Sprint(i + 1), Label: label})
		}
	} else if model.Breakthrough != nil && len(model.Breakthrough.Choices) > 0 {
		for i, choice := range model.Breakthrough.Choices {
			label := choice.Text
			if choice.CostText != "" {
				label += "；" + choice.CostText
			}
			if choice.RiskText != "" {
				label += "；风险：" + choice.RiskText
			}
			options = append(options, panel.Option{Key: fmt.Sprint(i + 1), Label: label})
		}
	} else {
		options = model.Options
	}
	lines := make([]screenLine, 0, len(options))
	for _, option := range options {
		label := safeText(option.Label)
		if option.Disabled {
			label += "（不可用：" + safeText(option.DisabledReason) + "）"
		}
		if option.CostText != "" {
			label += "；" + safeText(option.CostText)
		}
		lines = appendWrapped(lines, "["+safeText(option.Key)+"] "+label, width, optionRole(option), 100)
	}
	return lines
}

func optionRole(option panel.Option) string {
	if option.Disabled {
		return "muted"
	}
	return "option"
}

func progressBar(percent, width int) string {
	if width <= 0 {
		return ""
	}
	if percent < 0 {
		percent = 0
	}
	if percent > 100 {
		percent = 100
	}
	filled := percent * width / 100
	return "[" + strings.Repeat("#", filled) + strings.Repeat("-", width-filled) + "]"
}

func appendWrapped(dst []screenLine, value string, width int, role string, priority int) []screenLine {
	value = safeText(value)
	if value == "" {
		return dst
	}
	for _, line := range wrapCells(value, width) {
		dst = append(dst, screenLine{text: line, role: role, priority: priority})
	}
	return dst
}

func writeLines(out io.Writer, lines []string, options RenderOptions, width int) error {
	for _, line := range lines {
		if _, err := io.WriteString(out, clipCells(line, width)+"\r\n"); err != nil {
			return err
		}
	}
	return nil
}

func paint(text, role string, options RenderOptions) string {
	if !options.ColorOK || options.ColorMode == "none" {
		return text
	}
	if options.ColorLevel >= 256 {
		return fmt.Sprintf("\x1b[38;5;%dm%s\x1b[0m", indexedColor(role, options.Theme), text)
	}
	if options.ColorLevel >= 24 {
		r, g, b := rgbColor(role, options.Theme)
		return fmt.Sprintf("\x1b[38;2;%d;%d;%dm%s\x1b[0m", r, g, b, text)
	}
	code := ""
	if options.Theme == "paper" {
		switch role {
		case "title":
			code = "1;34"
		case "heading", "option":
			code = "33"
		case "warning", "error", "warn":
			code = "1;31"
		case "save", "notice":
			code = "32"
		case "muted", "hint":
			code = "90"
		default:
			code = "37"
		}
	} else {
		switch role {
		case "title":
			code = "1;36"
		case "heading", "option":
			code = "32"
		case "warning", "error", "warn":
			code = "1;31"
		case "save", "notice":
			code = "36"
		case "muted", "hint":
			code = "90"
		default:
			code = "37"
		}
	}
	return "\x1b[" + code + "m" + text + "\x1b[0m"
}

func rgbColor(role, theme string) (int, int, int) {
	if theme == "paper" {
		switch role {
		case "title":
			return 67, 94, 152
		case "heading", "option":
			return 145, 100, 30
		case "warning", "error", "warn":
			return 174, 54, 48
		case "save", "notice":
			return 45, 120, 76
		case "muted", "hint":
			return 115, 116, 118
		default:
			return 50, 48, 44
		}
	}
	switch role {
	case "title":
		return 69, 183, 157
	case "heading", "option":
		return 101, 181, 125
	case "warning", "error", "warn":
		return 235, 97, 87
	case "save", "notice":
		return 87, 181, 194
	case "muted", "hint":
		return 130, 144, 148
	default:
		return 220, 227, 226
	}
}

func indexedColor(role, theme string) int {
	if theme == "paper" {
		switch role {
		case "title":
			return 68
		case "heading", "option":
			return 136
		case "warning", "error", "warn":
			return 160
		case "save", "notice":
			return 71
		case "muted", "hint":
			return 245
		default:
			return 237
		}
	}
	switch role {
	case "title":
		return 43
	case "heading", "option":
		return 78
	case "warning", "error", "warn":
		return 203
	case "save", "notice":
		return 80
	case "muted", "hint":
		return 245
	default:
		return 252
	}
}

func safeText(value string) string {
	var b strings.Builder
	for _, r := range value {
		if unicode.IsControl(r) || r == '\u2028' || r == '\u2029' {
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

func wrapCells(value string, width int) []string {
	if width <= 0 {
		return []string{value}
	}
	var lines []string
	var line strings.Builder
	used := 0
	for _, r := range value {
		w := runeCells(r)
		if used > 0 && used+w > width {
			lines = append(lines, line.String())
			line.Reset()
			used = 0
		}
		line.WriteRune(r)
		used += w
	}
	if line.Len() > 0 || len(lines) == 0 {
		lines = append(lines, line.String())
	}
	return lines
}

func clipCells(value string, width int) string {
	if width <= 0 {
		return ""
	}
	var b strings.Builder
	used := 0
	for _, r := range value {
		w := runeCells(r)
		if used+w > width {
			break
		}
		b.WriteRune(r)
		used += w
	}
	return b.String()
}

func runeCells(r rune) int {
	if !utf8.ValidRune(r) || unicode.Is(unicode.Mn, r) || unicode.Is(unicode.Me, r) || r == '\u200d' {
		return 0
	}
	if r < 0x20 || (r >= 0x7f && r < 0xa0) {
		return 0
	}
	if isWideRune(r) {
		return 2
	}
	return 1
}

func isWideRune(r rune) bool {
	return r >= 0x1100 && r <= 0x115f ||
		r == 0x2329 || r == 0x232a ||
		r >= 0x2e80 && r <= 0xa4cf ||
		r >= 0xac00 && r <= 0xd7a3 ||
		r >= 0xf900 && r <= 0xfaff ||
		r >= 0xfe10 && r <= 0xfe6f ||
		r >= 0xff00 && r <= 0xff60 ||
		r >= 0xffe0 && r <= 0xffe6 ||
		r >= 0x1f300 && r <= 0x1faff ||
		r >= 0x20000 && r <= 0x3fffd
}
