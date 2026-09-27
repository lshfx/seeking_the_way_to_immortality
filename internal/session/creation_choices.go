package session

import (
	"fmt"
	"strings"

	"github.com/lshfx/seeking_the_way_to_immortality/internal/engine"
	"github.com/lshfx/seeking_the_way_to_immortality/internal/panel"
)

const creationFullNameField = "full_name"

// Names are presentation suggestions, not character classes. A player can
// combine any name and gender with any of the mechanically distinct presets.
var suggestedCreationNames = []struct {
	surname string
	given   string
}{
	{"沈", "云舟"},
	{"陆", "守拙"},
	{"林", "照月"},
	{"苏", "清禾"},
}

var commonCompoundSurnames = []string{
	"欧阳", "司马", "诸葛", "上官", "东方", "独孤", "南宫", "慕容", "夏侯", "公孙",
	"宇文", "轩辕", "令狐", "皇甫", "长孙", "尉迟", "司徒", "司空", "端木", "百里",
}

func splitCreationFullName(full string) (surname, given string) {
	name := []rune(full)
	for _, compound := range commonCompoundSurnames {
		if strings.HasPrefix(full, compound) && len(name) > len([]rune(compound)) {
			return compound, strings.TrimPrefix(full, compound)
		}
	}
	return string(name[0]), string(name[1:])
}

func creationGenderLabel(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case engine.GenderMale:
		return "男"
	case engine.GenderFemale:
		return "女"
	case engine.GenderUnspecified, "":
		return "未指定"
	default:
		return value
	}
}

func (s *Session) creationNameModel(m panel.Model, view engine.CreationView) panel.Model {
	m.Title = "选择姓名"
	m.Scene = "选现成姓名，或一次输入自定义姓名。"
	m.Creation = nil
	m.Details = []panel.DetailSection{{ID: "creation-name", Title: "当前角色", Rows: []panel.DetailRow{{Label: "姓名", Value: view.Surname + view.GivenName}}}}
	if s.activeTextField == creationFullNameField {
		m.Title = "自定义姓名"
		m.Scene = "输入完整姓名后按回车保存；输入 :cancel 取消。"
		m.Details[0].Lines = []string{"姓与名一次输入，例如 沈云舟。", fmt.Sprintf("限2～%d个字符；q在此处是普通文字。", creationTextLimit(creationFullNameField))}
		m.Options = []panel.Option{{Key: ":cancel", Label: "取消输入", CommandKind: "TEXT_INPUT_CANCEL"}}
		return m
	}
	for i, choice := range suggestedCreationNames {
		m.Options = append(m.Options, panel.Option{
			Key: fmt.Sprint(i + 1), Label: choice.surname + choice.given,
			CommandKind: string(engine.KindCreateEdit),
		})
	}
	m.Options = append(m.Options,
		panel.Option{Key: "5", Label: "自定义姓名", CommandKind: "TEXT_INPUT"},
		panel.Option{Key: "b", Label: "返回确认", CommandKind: "LOCAL_PAGE"},
		panel.Option{Key: "q", Label: "退出并保留创角草稿", CommandKind: "QUIT"},
	)
	return m
}

func (s *Session) creationGenderModel(m panel.Model, view engine.CreationView) panel.Model {
	m.Title = "选择性别"
	m.Scene = "直接选择，也可自行填写；选择不影响角色属性。"
	m.Creation = nil
	m.Details = []panel.DetailSection{{ID: "creation-gender", Title: "当前角色", Rows: []panel.DetailRow{{Label: "性别", Value: creationGenderLabel(view.Gender)}}}}
	if s.activeTextField == panel.FieldGender {
		m.Title = "自定义性别"
		m.Scene = "输入自定义称谓后按回车保存；输入 :cancel 取消。"
		m.Details[0].Lines = []string{fmt.Sprintf("最多%d个字符；会按输入原样保存。", creationTextLimit(panel.FieldGender))}
		m.Options = []panel.Option{{Key: ":cancel", Label: "取消输入", CommandKind: "TEXT_INPUT_CANCEL"}}
		return m
	}
	m.Options = []panel.Option{
		{Key: "1", Label: "男", CommandKind: string(engine.KindCreateEdit)},
		{Key: "2", Label: "女", CommandKind: string(engine.KindCreateEdit)},
		{Key: "3", Label: "不指定", CommandKind: string(engine.KindCreateEdit)},
		{Key: "4", Label: "自定义", CommandKind: "TEXT_INPUT"},
		{Key: "b", Label: "返回确认", CommandKind: "LOCAL_PAGE"},
		{Key: "q", Label: "退出并保留创角草稿", CommandKind: "QUIT"},
	}
	return m
}

func (s *Session) handleCreationNameKey(key string) error {
	if key == "b" {
		s.page = pageMain
		return nil
	}
	if key == "5" {
		s.activeTextField = creationFullNameField
		return nil
	}
	if len(key) == 1 && key[0] >= '1' && key[0] <= '4' {
		choice := suggestedCreationNames[int(key[0]-'1')]
		return s.submit(engine.KindCreateEdit, engine.Payload{Creation: &engine.CreationPayload{
			Selection: engine.CreationSelection{Surname: choice.surname, GivenName: choice.given},
		}}, "")
	}
	s.unknownKey()
	return nil
}

func (s *Session) handleCreationGenderKey(key string) error {
	if key == "b" {
		s.page = pageMain
		return nil
	}
	if key == "4" {
		s.activeTextField = panel.FieldGender
		return nil
	}
	gender := ""
	switch key {
	case "1":
		gender = engine.GenderMale
	case "2":
		gender = engine.GenderFemale
	case "3":
		gender = engine.GenderUnspecified
	default:
		s.unknownKey()
		return nil
	}
	return s.submit(engine.KindCreateEdit, engine.Payload{Creation: &engine.CreationPayload{
		Selection: engine.CreationSelection{Gender: gender},
	}}, "")
}
