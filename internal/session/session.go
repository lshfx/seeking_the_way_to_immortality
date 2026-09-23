// Package session connects the deterministic engine, durable local storage and
// read-only panel model. It owns no terminal input or rendering.
package session

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/lshfx/seeking_the_way_to_immortality/internal/content"
	"github.com/lshfx/seeking_the_way_to_immortality/internal/engine"
	"github.com/lshfx/seeking_the_way_to_immortality/internal/panel"
	"github.com/lshfx/seeking_the_way_to_immortality/internal/storage"
	"github.com/lshfx/seeking_the_way_to_immortality/internal/wiring"
)

const (
	// DefaultGameID is the single local M1 playthrough slot. Named slots and
	// multiple characters are outside TASK-09's short-loop scope.
	DefaultGameID   = "wendao-main"
	defaultBranchID = "branch-main"
	logTailLimit    = 8
)

type page string

const (
	pageMain         page = "main"
	pageDetails      page = "details"
	pageInventory    page = "inventory"
	pageHelp         page = "help"
	pageSettings     page = "settings"
	pageCreationEdit page = "creation-edit"
)

// Session owns one exclusive local game session.
type Session struct {
	layout    storage.Layout
	gameID    string
	lock      *storage.SaveLock
	disk      *storage.SnapshotStore[engine.SaveEnvelope]
	store     *durableStore
	engine    *engine.Engine
	catalogue engine.Catalogue

	settings        storage.Settings
	page            page
	pageIndex       int
	activeTextField string
	notice          *panel.Notice

	loadNotice string
	saveFailed bool
	runID      string
	actionSeq  uint64
}

// OpenDefault opens the standard per-user local data directory.
func OpenDefault() (*Session, error) {
	layout, err := storage.EnsureLayout()
	if err != nil {
		return nil, err
	}
	return OpenAt(layout, DefaultGameID)
}

// OpenAt opens one game slot under layout. It is exported so tests and
// development tools can use an isolated temporary data root.
func OpenAt(layout storage.Layout, gameID string) (*Session, error) {
	if strings.TrimSpace(gameID) == "" {
		return nil, errors.New("game id is required")
	}
	if strings.TrimSpace(layout.Root) == "" || !filepath.IsAbs(layout.Root) {
		return nil, errors.New("game data root must be absolute; the current working directory is never used for saves")
	}
	var err error
	layout, err = storage.EnsureLayoutFor(layout)
	if err != nil {
		return nil, err
	}
	catalogue, err := content.LoadM1()
	if err != nil {
		return nil, err
	}
	lock, takenOver, staleReason, err := storage.AcquireSaveLock(layout, gameID)
	if err != nil {
		return nil, err
	}
	opened := false
	defer func() {
		if !opened {
			_ = lock.Release()
		}
	}()

	disk, err := storage.NewSnapshotStore[engine.SaveEnvelope](layout, gameID, saveCodec())
	if err != nil {
		return nil, err
	}
	settings, settingsWarning, err := storage.LoadSettings(layout)
	if err != nil {
		return nil, fmt.Errorf("load settings: %w", err)
	}

	loaded, loadErr := disk.Load()
	var state *engine.GameState
	var logTail []string
	var loadNotice string
	if errors.Is(loadErr, storage.ErrSaveNotFound) {
		state = newCreationState(gameID)
		logTail = []string{"创角草稿已自动保存；尚未消耗游戏时间。"}
		env := envelopeFor(state, logTail)
		if err := disk.Commit(env); err != nil {
			return nil, fmt.Errorf("create initial save: %w", err)
		}
	} else if loadErr != nil {
		return nil, fmt.Errorf("open save: %w", loadErr)
	} else {
		if loaded.Envelope == nil {
			return nil, errors.New("save loader returned no envelope")
		}
		if err := validateEnvelope(loaded.Envelope, gameID); err != nil {
			return nil, fmt.Errorf("loaded save is invalid: %w", err)
		}
		state = engine.CloneGameState(&loaded.Envelope.State)
		logTail = append([]string(nil), loaded.Envelope.LogTail...)
		loadNotice = "已恢复上次提交的进度；没有重新执行上次行动。"
		if loaded.Recovered {
			loadNotice = "主存档不可用，已从上一份完整快照恢复；未重复结算。"
		}
	}
	if takenOver {
		if loadNotice != "" {
			loadNotice += " "
		}
		loadNotice += "发现上次异常退出留下的锁，已确认原进程结束并接管。"
		if staleReason != "" {
			loadNotice += "（" + staleReason + "）"
		}
	}
	if settingsWarning != "" {
		if loadNotice != "" {
			loadNotice += " "
		}
		loadNotice += settingsWarning
	}
	store := &durableStore{disk: disk, current: engine.CloneGameState(state), logTail: logTail, lastDurableRevision: state.Revision}
	runID := fmt.Sprintf("run-%d-%d", os.Getpid(), time.Now().UnixNano())
	s := &Session{
		layout: layout, gameID: gameID, lock: lock, disk: disk, store: store,
		catalogue: catalogue, settings: settings, page: pageMain,
		loadNotice: loadNotice, runID: runID,
	}
	s.engine = engine.NewEngine(store, &s.catalogue, state)
	opened = true
	return s, nil
}

// Close releases the single-writer lock. All successful actions have already
// been atomically committed before they reach the next screen.
func (s *Session) Close() error {
	if s == nil || s.lock == nil {
		return nil
	}
	return s.lock.Release()
}

// State exposes a deep copy for diagnostics and integration tests.
func (s *Session) State() *engine.GameState {
	if s == nil || s.engine == nil {
		return nil
	}
	return engine.CloneGameState(s.engine.State())
}

// Settings returns the active presentation preferences.
func (s *Session) Settings() storage.Settings {
	if s == nil {
		return storage.DefaultSettings()
	}
	return s.settings
}

// TextInputActive reports whether the next terminal line is creation text,
// where letters such as q are data rather than menu commands.
func (s *Session) TextInputActive() bool {
	return s != nil && s.activeTextField != "" && !s.saveFailed
}

// HandleText commits one line into the current creation field. It never
// interprets menu keys; in particular, "q" is a literal field value here.
func (s *Session) HandleText(line string) error {
	if !s.TextInputActive() {
		return errors.New("text input is not active")
	}
	line = strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r")
	text := strings.TrimSpace(line)
	if text == ":cancel" {
		s.activeTextField = ""
		s.notice = &panel.Notice{Kind: "info", Text: "已取消字段编辑；创角草稿未改变。"}
		return nil
	}
	if text == "" {
		s.Notify("内容不能为空；输入 :cancel 返回编辑菜单。")
		return nil
	}
	if !utf8.ValidString(text) {
		s.Notify("输入不是有效UTF-8文本；请重新输入。")
		return nil
	}
	for _, r := range text {
		if unicode.IsControl(r) {
			s.Notify("文本不能包含终端控制字符；请重新输入。")
			return nil
		}
	}
	limit := creationTextLimit(s.activeTextField)
	if limit == 0 {
		return errors.New("unknown creation text field")
	}
	if count := utf8.RuneCountInString(text); count > limit {
		s.Notify(fmt.Sprintf("最多输入%d个字符；请重新输入。", limit))
		return nil
	}

	state := s.engine.State()
	if state.Phase != engine.PhaseCreation || state.Pending.Creation == nil {
		return errors.New("creation draft is no longer active")
	}
	selection := engine.SelectionFromDraft(state.Pending.Creation)
	switch s.activeTextField {
	case panel.FieldSurname:
		selection.Surname = text
	case panel.FieldGivenName:
		selection.GivenName = text
	case panel.FieldDaoName:
		selection.DaoName = text
	case panel.FieldGender:
		selection.Gender = text
	case panel.FieldAppearance:
		selection.Appearance = text
	case panel.FieldAge:
		years, err := strconv.Atoi(text)
		if err != nil || years < engine.CreationAgeMinYears || years > engine.CreationAgeMaxYears {
			s.Notify(fmt.Sprintf("年龄须为%d到%d之间的整数；请重新输入。", engine.CreationAgeMinYears, engine.CreationAgeMaxYears))
			return nil
		}
		selection.AgeYears = years
	}
	oldRevision := state.Revision
	if err := s.submit(engine.KindCreateEdit, engine.Payload{Creation: &engine.CreationPayload{Selection: selection}}, ""); err != nil {
		return err
	}
	if s.saveFailed {
		s.activeTextField = ""
		return nil
	}
	if s.engine.State().Revision != oldRevision {
		s.activeTextField = ""
		s.page = pageCreationEdit
	}
	return nil
}

// SavePath returns the active automatic-save path for help and support screens.
func (s *Session) SavePath() string {
	if s == nil || s.disk == nil {
		return ""
	}
	return s.disk.SavePath()
}

// Model projects the current committed state into a read-only panel model.
// Building a model never submits a command, draws randomness or advances time.
func (s *Session) Model() panel.Model {
	if s == nil || s.engine == nil {
		return panel.Model{SchemaVersion: panel.SchemaVersion, Title: "问道长生", Error: &panel.ErrorBlock{
			Code: "SESSION_UNAVAILABLE", Text: "游戏会话尚未打开。", Recoverable: false,
		}}
	}
	state := s.engine.State()
	m := panel.Model{
		SchemaVersion: panel.SchemaVersion,
		ViewToken:     s.engine.ViewToken(),
		Revision:      state.Revision,
		Title:         "问道长生",
		Scene:         "",
		Options:       []panel.Option{},
		Save:          panel.SaveBlock{State: "durable", Text: "每项已提交行动都会立即自动保存。", LastDurableRevision: state.Revision},
	}
	if s.saveFailed {
		m.Save = panel.SaveBlock{State: "failed", Text: "保存失败；为保护最近完整进度，已停止接受游戏行动。", LastDurableRevision: s.store.lastDurableRevision}
	}
	if s.notice != nil {
		copy := *s.notice
		m.Notice = &copy
	} else if s.loadNotice != "" {
		m.Notice = &panel.Notice{Kind: "info", Text: s.loadNotice}
	}
	m.Changes = recentChanges(s.store.logTail)

	if state.Phase == engine.PhaseCreation {
		m = s.creationModel(m, state)
		if s.saveFailed {
			m.Options = []panel.Option{{Key: "q", Label: "退出并保留最近完整进度", CommandKind: "QUIT"}}
		}
		return m
	}
	if state.Player == nil {
		m.Error = &panel.ErrorBlock{Code: "MISSING_PLAYER", Text: "存档中没有角色状态，游戏行动已禁用。", Recoverable: false}
		m.Options = []panel.Option{{Key: "q", Label: "退出", CommandKind: "QUIT"}}
		return m
	}
	s.addStatus(&m, state)
	if s.saveFailed {
		m.Title = "保存暂停"
		m.Options = []panel.Option{{Key: "q", Label: "退出并保留最近完整进度", CommandKind: "QUIT"}}
		return m
	}
	switch s.page {
	case pageDetails:
		m.Title = "角色详情"
		sections, hasPrevious, hasNext := paginateSections(s.detailSections(state), s.pageIndex, 6)
		m.Details = sections
		m.Options = pageOptions(hasPrevious, hasNext)
	case pageInventory:
		m.Title = "随身物品"
		sections, hasPrevious, hasNext := paginateSections([]panel.DetailSection{s.inventorySection(state)}, s.pageIndex, 8)
		m.Details = sections
		m.Options = pageOptions(hasPrevious, hasNext)
	case pageHelp:
		m.Title = "操作说明"
		m.Details = []panel.DetailSection{{ID: "help", Title: "快捷键", Collapsed: false, Lines: []string{
			"1：普通修炼一个月；提交后立即保存。",
			"d：角色详情　i：随身物品　s：设置　h：本页　q：退出。",
			"每次只处理一个输入；空输入不推进时间。关闭游戏不会推进游戏年月。",
			"游戏完全离线，不读取工作进程、不接管其他终端的输入或输出。",
			"存档位置：" + s.SavePath(),
		}}}
		m.Options = []panel.Option{{Key: "b", Label: "返回", CommandKind: "LOCAL_PAGE"}, {Key: "q", Label: "退出并保留进度", CommandKind: "QUIT"}}
	case pageSettings:
		m.Title = "设置"
		m.Details = []panel.DetailSection{{ID: "settings", Title: "独立保存的界面偏好", Collapsed: false, Rows: []panel.DetailRow{
			{Label: "主题", Value: themeLabel(s.settings.Theme)},
			{Label: "颜色", Value: colorModeLabel(s.settings.ColorMode)},
			{Label: "静音", Value: boolLabel(s.settings.Silent)},
			{Label: "设置文件", Value: s.layout.SettingsPath(), Hint: "与角色存档分开保存"},
		}}}
		m.Options = []panel.Option{
			{Key: "t", Label: "切换主题", CommandKind: "SETTING"},
			{Key: "c", Label: "切换颜色档", CommandKind: "SETTING"},
			{Key: "m", Label: "切换静音", CommandKind: "SETTING"},
			{Key: "b", Label: "返回", CommandKind: "LOCAL_PAGE"},
			{Key: "q", Label: "退出并保留进度", CommandKind: "QUIT"},
		}
	default:
		m.Title = "洞府清修"
		m.Scene = s.sceneName(state)
		m.Options = s.mainOptions(state)
	}
	return m
}

// HandleKey applies exactly one already-parsed menu key. It returns true when
// the caller should exit normally. Local navigation never touches the engine.
func (s *Session) HandleKey(key string) (bool, error) {
	if s == nil || s.engine == nil {
		return false, errors.New("game session is unavailable")
	}
	key = strings.ToLower(strings.TrimSpace(key))
	if key == "" {
		return false, nil
	}
	if s.activeTextField != "" {
		s.Notify("文本编辑中；输入一行内容，或输入 :cancel 取消。")
		return false, nil
	}
	if key == "q" {
		return true, nil
	}
	s.notice = nil
	if s.saveFailed {
		s.notice = &panel.Notice{Kind: "error", Text: "保存失败后不能继续游戏行动；请检查磁盘和权限，再重新启动恢复最近完整快照。"}
		return false, nil
	}
	state := s.engine.State()
	if state.Phase == engine.PhaseCreation {
		if s.page == pageCreationEdit {
			return false, s.handleCreationEditKey(key)
		}
		return false, s.handleCreationKey(key, state)
	}
	switch s.page {
	case pageSettings:
		return false, s.handleSettingsKey(key)
	case pageDetails, pageInventory:
		switch key {
		case "b":
			s.page = pageMain
		case "n":
			if _, _, hasNext := s.currentPageSections(state); hasNext {
				s.pageIndex++
			} else {
				s.notice = &panel.Notice{Kind: "info", Text: "已经是最后一页。"}
			}
		case "p":
			if s.pageIndex > 0 {
				s.pageIndex--
			}
		default:
			s.unknownKey()
		}
		return false, nil
	case pageHelp:
		if key == "b" {
			s.page = pageMain
			return false, nil
		}
		s.unknownKey()
		return false, nil
	}
	switch key {
	case "1":
		if state.Phase != engine.PhaseReady {
			s.notice = &panel.Notice{Kind: "warn", Text: "当前阶段没有可开始的普通修炼。"}
			return false, nil
		}
		if forecast, err := engine.PreviewCultivation(state.Player, state.World, &s.catalogue, engine.ActionNormal); err == nil && forecast.AtThreshold {
			s.notice = &panel.Notice{Kind: "warn", Text: "当前小阶修为已满，继续修炼会浪费一个月；需要由后续突破玩法处理。"}
			return false, nil
		}
		return false, s.submit(engine.KindCultivate, engine.Payload{ActionKind: engine.ActionNormal}, "")
	case "d":
		s.page = pageDetails
		s.pageIndex = 0
	case "i":
		s.page = pageInventory
		s.pageIndex = 0
	case "h":
		s.page = pageHelp
	case "s":
		s.page = pageSettings
	default:
		s.unknownKey()
	}
	return false, nil
}

func (s *Session) creationModel(m panel.Model, state *engine.GameState) panel.Model {
	m.Title = "创建角色"
	m.Scene = "选择预设并确认，即可开始；创角不会消耗游戏时间。"
	screen := wiring.BuildCreationScreen(&s.catalogue, state.Pending.Creation)
	m.Creation = localizedCreationBlock(screen)
	m.Calendar = panel.Calendar{}
	m.Status = panel.StatusBlock{}
	if screen.View.Step == engine.CreationStepDetails && m.Creation != nil {
		name := screen.View.Surname + screen.View.GivenName
		if name == "" {
			name = "未设置"
		}
		daoName := screen.View.DaoName
		if daoName == "" {
			daoName = "未设置"
		}
		m.Creation.Summary = []panel.DetailRow{{Label: "姓名", Value: name}, {Label: "道号", Value: daoName}}
	}
	if s.page == pageCreationEdit {
		return s.creationEditModel(m, screen)
	}
	if screen.View.Step == engine.CreationStepIdentity {
		m.Options = make([]panel.Option, 0, len(screen.View.Presets)+1)
		for i, preset := range screen.View.Presets {
			key := fmt.Sprint(i + 1)
			m.Options = append(m.Options, panel.Option{
				Key: key, Label: preset.Label,
				CommandKind: string(engine.KindCreateEdit), TargetID: preset.ID,
			})
		}
	} else {
		canConfirm := screen.View.CanConfirm
		m.Options = []panel.Option{
			{Key: "c", Label: "确认创建", CommandKind: string(engine.KindCreateConfirm), Disabled: !canConfirm,
				DisabledReason: disabledConfirmReason(screen.View)},
			{Key: "e", Label: "编辑姓名、道号等资料", CommandKind: "LOCAL_PAGE"},
			{Key: "b", Label: "返回选择预设", CommandKind: string(engine.KindCreateEdit)},
		}
	}
	m.Options = append(m.Options, panel.Option{Key: "q", Label: "退出并保留创角草稿", CommandKind: "QUIT"})
	return m
}

type creationEditField struct {
	key   string
	id    string
	label string
	value string
}

func (s *Session) creationEditModel(m panel.Model, screen wiring.CreationScreen) panel.Model {
	view := screen.View
	gender := view.Gender
	if strings.EqualFold(gender, engine.GenderUnspecified) {
		gender = "未指定"
	}
	fields := []creationEditField{
		{key: "1", id: panel.FieldSurname, label: "姓", value: view.Surname},
		{key: "2", id: panel.FieldGivenName, label: "名", value: view.GivenName},
		{key: "3", id: panel.FieldDaoName, label: "道号", value: view.DaoName},
		{key: "4", id: panel.FieldGender, label: "性别", value: gender},
		{key: "5", id: panel.FieldAppearance, label: "外观", value: view.Appearance},
		{key: "6", id: panel.FieldAge, label: "年龄", value: strconv.Itoa(view.AgeYears)},
	}
	for i := range fields {
		if fields[i].value == "" {
			fields[i].value = "未设置"
		}
	}
	m.Creation = nil
	m.Details = []panel.DetailSection{{ID: "creation-edit", Title: "创角资料", Collapsed: false}}
	if s.activeTextField != "" {
		for _, field := range fields {
			if field.id != s.activeTextField {
				continue
			}
			m.Title = "编辑" + field.label
			m.Scene = "输入新内容并按回车保存；输入 :cancel 取消。"
			m.Details[0].Lines = []string{
				"当前值：" + field.value,
				"最多" + strconv.Itoa(creationTextLimit(field.id)) + "个字符；q在此处是普通文本。",
			}
			if field.id == panel.FieldAge {
				m.Details[0].Lines = append(m.Details[0].Lines, fmt.Sprintf("年龄范围：%d～%d岁。", engine.CreationAgeMinYears, engine.CreationAgeMaxYears))
			}
			m.Options = []panel.Option{{Key: ":cancel", Label: "取消当前编辑", CommandKind: "TEXT_INPUT_CANCEL"}}
			return m
		}
	}
	m.Title = "编辑创角资料"
	m.Scene = "选择要修改的字段；每次修改都会保存创角草稿，不推进游戏月份。"
	for _, field := range fields {
		m.Details[0].Rows = append(m.Details[0].Rows, panel.DetailRow{Label: field.label, Value: field.value})
		m.Options = append(m.Options, panel.Option{Key: field.key, Label: "编辑" + field.label, CommandKind: "TEXT_INPUT"})
	}
	m.Options = append(m.Options,
		panel.Option{Key: "b", Label: "返回创角", CommandKind: "LOCAL_PAGE"},
		panel.Option{Key: "q", Label: "退出并保留创角草稿", CommandKind: "QUIT"},
	)
	return m
}

func creationTextLimit(fieldID string) int {
	switch fieldID {
	case panel.FieldSurname:
		return 6
	case panel.FieldGivenName:
		return 12
	case panel.FieldDaoName:
		return 16
	case panel.FieldGender:
		return 16
	case panel.FieldAppearance:
		return 40
	case panel.FieldAge:
		return 2
	default:
		return 0
	}
}

func (s *Session) handleCreationEditKey(key string) error {
	if s.activeTextField != "" {
		s.Notify("文本编辑中；输入一行内容，或输入 :cancel 取消。")
		return nil
	}
	switch key {
	case "1":
		s.activeTextField = panel.FieldSurname
	case "2":
		s.activeTextField = panel.FieldGivenName
	case "3":
		s.activeTextField = panel.FieldDaoName
	case "4":
		s.activeTextField = panel.FieldGender
	case "5":
		s.activeTextField = panel.FieldAppearance
	case "6":
		s.activeTextField = panel.FieldAge
	case "b":
		s.page = pageMain
	default:
		s.unknownKey()
	}
	return nil
}

func localizedCreationBlock(screen wiring.CreationScreen) *panel.CreationBlock {
	block := screen.Block
	if block == nil {
		return nil
	}
	for i := range block.Fields {
		field := &block.Fields[i]
		switch field.ID {
		case panel.FieldGender:
			if strings.EqualFold(strings.TrimSpace(field.Value), "unspecified") {
				field.Value = "未指定"
			}
		case panel.FieldPath:
			field.Value = creationOptionLabel(screen.View.PathOptions, string(screen.View.PathID))
		case panel.FieldSpiritRoot:
			field.Value = creationOptionLabel(screen.View.SpiritRootOpts, string(screen.View.SpiritRootID))
		case panel.FieldConstitution:
			field.Value = creationOptionLabel(screen.View.ConstitutionOpts, screen.View.ConstitutionID)
		case panel.FieldTalents:
			labels := make([]string, 0, len(screen.View.TalentIDs))
			for _, id := range screen.View.TalentIDs {
				labels = append(labels, creationOptionLabel(screen.View.TalentOptions, id))
			}
			field.Value = strings.Join(labels, "、")
		}
	}
	return block
}

func creationOptionLabel(options []engine.OptionView, id string) string {
	for _, option := range options {
		if option.ID == id {
			return option.Label
		}
	}
	return id
}

func disabledConfirmReason(view engine.CreationView) string {
	if view.YaoPending {
		return "仙缘判定待处理"
	}
	if !view.CanConfirm {
		return "创角信息尚未通过校验"
	}
	return ""
}

func (s *Session) mainOptions(state *engine.GameState) []panel.Option {
	label := "普通修炼（每月约 +19.5 修为）"
	option := panel.Option{Key: "1", Label: label, CommandKind: string(engine.KindCultivate), CostText: "耗时 1 月"}
	if preview, err := engine.PreviewCultivation(state.Player, state.World, &s.catalogue, engine.ActionNormal); err == nil {
		if preview.AtThreshold {
			option.Label = "普通修炼（已达当前小阶阈值）"
			option.Disabled = true
			option.DisabledReason = "需由后续突破玩法处理"
		} else {
			option.Label = "普通修炼（预估 +" + formatFixed(preview.NextGain) + " 修为）"
		}
	}
	return []panel.Option{
		option,
		{Key: "d", Label: "角色详情", CommandKind: "LOCAL_PAGE"},
		{Key: "i", Label: "随身物品", CommandKind: "LOCAL_PAGE"},
		{Key: "h", Label: "操作说明", CommandKind: "LOCAL_PAGE"},
		{Key: "s", Label: "设置", CommandKind: "LOCAL_PAGE"},
		{Key: "q", Label: "退出并保留进度", CommandKind: "QUIT"},
	}
}

func (s *Session) addStatus(m *panel.Model, state *engine.GameState) {
	p := state.Player
	month := state.Counters.WorldMonth
	if month < 0 {
		month = 0
	}
	monthOfYear := month%12 + 1
	calendarYear := 387 + month/12
	remainingMonths := int64(p.Lifespan.TotalYears())*12 - p.AgeMonths
	if remainingMonths < 0 {
		remainingMonths = 0
	}
	remainingText := fmt.Sprintf("余寿 %d 年 %d 月", remainingMonths/12, remainingMonths%12)
	m.Calendar = panel.Calendar{
		WorldMonth:            month,
		Text:                  fmt.Sprintf("天玄历 %d 年 %s", calendarYear, chineseMonth(monthOfYear)),
		AgeText:               fmt.Sprintf("%d 岁 %d 月", p.AgeMonths/12, p.AgeMonths%12),
		LifespanRemainingText: remainingText,
	}
	realmText := string(p.Realm)
	for _, realm := range s.catalogue.Realms {
		if realm.ID == p.Realm {
			realmText = realm.NameZH
			break
		}
	}
	tierText := tierLabel(p.Tier)
	threshold := int64(0)
	for _, realm := range s.catalogue.Realms {
		if realm.ID == p.Realm {
			threshold = int64(realm.TierThreshold.Value)
			break
		}
	}
	progress := panel.ProgressView{Current: p.XP, Max: threshold, Text: formatFixed(p.XP) + " / " + formatFixed(threshold)}
	if threshold > 0 {
		progress.RatioPercent = int(p.XP * 100 / threshold)
		if progress.RatioPercent > 100 {
			progress.RatioPercent = 100
		}
	}
	progress.Full = threshold > 0 && p.XP >= threshold
	if forecast, err := engine.PreviewCultivation(p, state.World, &s.catalogue, engine.ActionNormal); err == nil {
		if progress.Full {
			progress.ProjectedOverflow = forecast.Rate
		}
	}
	m.Status = panel.StatusBlock{
		RealmText: realmText,
		TierText:  tierText,
		HP:        vitalsView(p.HP),
		MP:        vitalsView(p.MP),
		XP:        progress,
		Resources: []panel.ResourceView{{ID: string(engine.ResSpiritStones), Label: "灵石", Value: p.Resources[engine.ResSpiritStones]}},
		Debt:      p.Debt,
		Mood:      panel.TextValue{ID: "mood", Label: "心境", Text: fmt.Sprintf("%d / 100", p.Condition.Mood)},
	}
}

func (s *Session) detailSections(state *engine.GameState) []panel.DetailSection {
	p := state.Player
	techniqueName := p.PrimaryTechniqueID
	for _, technique := range s.catalogue.Techniques {
		if technique.ID == p.PrimaryTechniqueID {
			techniqueName = technique.NameZH
			break
		}
	}
	return []panel.DetailSection{
		{ID: "identity", Title: "角色", Collapsed: false, Rows: []panel.DetailRow{
			{Label: "姓名", Value: p.Identity.Surname + p.Identity.GivenName},
			{Label: "道号", Value: nonEmpty(p.Identity.DaoName, "未设置")},
			{Label: "出身", Value: originLabel(&s.catalogue, p.Origin)},
			{Label: "道路", Value: "人道"},
			{Label: "灵根", Value: spiritRootLabel(&s.catalogue, p.SpiritRoot)},
			{Label: "主功法", Value: techniqueName},
		}},
		{ID: "attributes", Title: "六维", Collapsed: false, Rows: []panel.DetailRow{
			{Label: "力道", Value: fmt.Sprint(p.Attributes.Strength)},
			{Label: "身法", Value: fmt.Sprint(p.Attributes.Agility)},
			{Label: "根骨", Value: fmt.Sprint(p.Attributes.Constitution)},
			{Label: "悟性", Value: fmt.Sprint(p.Attributes.Comprehension)},
			{Label: "资质", Value: fmt.Sprint(p.Attributes.Aptitude)},
			{Label: "福缘", Value: fmt.Sprint(p.Attributes.Fortune)},
		}},
		{ID: "derived", Title: "修炼", Collapsed: false, Rows: []panel.DetailRow{
			{Label: "当前月增", Value: formatFixed(p.Derived.CultivationRate)},
			{Label: "有效资质", Value: fmt.Sprint(p.Derived.EffectiveAptitude)},
			{Label: "功法熟练度", Value: fmt.Sprint(p.Proficiencies[p.PrimaryTechniqueID])},
		}},
	}
}

func (s *Session) inventorySection(state *engine.GameState) panel.DetailSection {
	section := panel.DetailSection{ID: "inventory", Title: "物品", Collapsed: false}
	for _, stack := range state.Player.Inventory.Stacks {
		name := stack.ItemID
		for _, item := range s.catalogue.Items {
			if item.ID == stack.ItemID {
				name = item.NameZH
				break
			}
		}
		section.Rows = append(section.Rows, panel.DetailRow{Label: name, Value: fmt.Sprintf("×%d", stack.Quantity)})
	}
	if len(section.Rows) == 0 {
		section.Lines = []string{"当前没有随身物品。"}
	}
	return section
}

func (s *Session) currentPageSections(state *engine.GameState) ([]panel.DetailSection, bool, bool) {
	sections := s.detailSections(state)
	pageSize := 6
	if s.page == pageInventory {
		sections = []panel.DetailSection{s.inventorySection(state)}
		pageSize = 8
	}
	return paginateSections(sections, s.pageIndex, pageSize)
}

func paginateSections(sections []panel.DetailSection, pageIndex, pageSize int) ([]panel.DetailSection, bool, bool) {
	if pageSize <= 0 {
		pageSize = 6
	}
	var rows []panel.DetailRow
	for _, section := range sections {
		for _, row := range section.Rows {
			row.Label = section.Title + " · " + row.Label
			rows = append(rows, row)
		}
		for _, line := range section.Lines {
			rows = append(rows, panel.DetailRow{Label: section.Title, Value: line})
		}
	}
	pageCount := (len(rows) + pageSize - 1) / pageSize
	if pageCount == 0 {
		pageCount = 1
	}
	if pageIndex < 0 {
		pageIndex = 0
	}
	if pageIndex >= pageCount {
		pageIndex = pageCount - 1
	}
	start := pageIndex * pageSize
	end := start + pageSize
	if end > len(rows) {
		end = len(rows)
	}
	return []panel.DetailSection{{
		ID: "paged-details", Title: fmt.Sprintf("详情（第 %d/%d 页）", pageIndex+1, pageCount),
		Collapsed: false, Rows: append([]panel.DetailRow(nil), rows[start:end]...),
	}}, pageIndex > 0, pageIndex+1 < pageCount
}

func pageOptions(hasPrevious, hasNext bool) []panel.Option {
	options := make([]panel.Option, 0, 4)
	if hasPrevious {
		options = append(options, panel.Option{Key: "p", Label: "上一页", CommandKind: "LOCAL_PAGE"})
	}
	if hasNext {
		options = append(options, panel.Option{Key: "n", Label: "下一页", CommandKind: "LOCAL_PAGE"})
	}
	options = append(options,
		panel.Option{Key: "b", Label: "返回", CommandKind: "LOCAL_PAGE"},
		panel.Option{Key: "q", Label: "退出并保留进度", CommandKind: "QUIT"},
	)
	return options
}

func (s *Session) sceneName(state *engine.GameState) string {
	if state.World != nil {
		for _, location := range s.catalogue.Locations {
			if location.ID == state.World.CurrentLocation {
				return location.NameZH
			}
		}
	}
	return "洞府"
}

func (s *Session) handleCreationKey(key string, state *engine.GameState) error {
	draft := state.Pending.Creation
	if draft == nil {
		s.notice = &panel.Notice{Kind: "error", Text: "创角草稿缺失，无法继续；退出不会损坏最近存档。"}
		return nil
	}
	if key == "e" && draft.Step == engine.CreationStepDetails {
		s.page = pageCreationEdit
		return nil
	}
	if draft.Step == engine.CreationStepIdentity && len(key) == 1 && key[0] >= '1' && key[0] <= '3' {
		presetID := engine.M1CreationPresets()[int(key[0]-'1')].ID
		return s.submit(engine.KindCreateEdit, engine.Payload{Creation: &engine.CreationPayload{PresetID: presetID, AdvanceStep: true}}, "")
	}
	if draft.Step == engine.CreationStepDetails {
		switch key {
		case "c":
			return s.submit(engine.KindCreateConfirm, engine.Payload{Creation: &engine.CreationPayload{Confirm: true}}, "")
		case "b":
			return s.submit(engine.KindCreateEdit, engine.Payload{Creation: &engine.CreationPayload{BackStep: true}}, "")
		}
	}
	s.unknownKey()
	return nil
}

func (s *Session) handleSettingsKey(key string) error {
	next := s.settings
	switch key {
	case "t":
		if next.Theme == "jade" {
			next.Theme = "paper"
		} else {
			next.Theme = "jade"
		}
	case "c":
		switch next.ColorMode {
		case "auto":
			next.ColorMode = "basic"
		case "basic":
			next.ColorMode = "none"
		default:
			next.ColorMode = "auto"
		}
	case "m":
		next.Silent = !next.Silent
	case "b":
		s.page = pageMain
		return nil
	default:
		s.unknownKey()
		return nil
	}
	if err := storage.SaveSettings(s.layout, next); err != nil {
		s.notice = &panel.Notice{Kind: "error", Text: "界面设置保存失败；角色存档未受影响。"}
		return nil
	}
	s.settings = next
	s.notice = &panel.Notice{Kind: "info", Text: "界面设置已独立保存。"}
	return nil
}

func (s *Session) submit(kind engine.CommandKind, payload engine.Payload, target string) error {
	s.actionSeq++
	command := engine.Command{
		ActionID:         fmt.Sprintf("%s-%d", s.runID, s.actionSeq),
		SessionID:        s.runID,
		ExpectedRevision: s.engine.State().Revision,
		ViewToken:        s.engine.ViewToken(),
		Kind:             kind,
		TargetID:         target,
		Payload:          payload,
	}
	result := s.engine.Submit(command)
	if !result.OK {
		detail := "操作未执行。"
		if n := len(result.Events); n > 0 && result.Events[n-1].Detail != "" {
			detail = safeNotice(result.Events[n-1].Detail)
		}
		s.notice = &panel.Notice{Kind: "warn", Text: friendlyError(result.Code) + " " + detail}
		return nil
	}
	if result.SaveState == engine.SaveFailed {
		s.saveFailed = true
		s.notice = &panel.Notice{Kind: "error", Text: "存档未能安全写入；本次行动已撤销，不能继续游戏操作。请检查磁盘空间和文件权限。"}
		return nil
	}
	s.page = pageMain
	s.loadNotice = ""
	if result.MonthCostApplied > 0 {
		s.notice = &panel.Notice{Kind: "info", Text: "行动已结算并自动保存；游戏时间推进 1 个月。"}
	} else {
		s.notice = &panel.Notice{Kind: "info", Text: "进度已安全保存。"}
	}
	return nil
}

func (s *Session) unknownKey() {
	s.notice = &panel.Notice{Kind: "warn", Text: "这个按键当前不可用；查看屏幕底部的快捷键。"}
}

// Notify sets a presentation-only notice, for example when pasted input is
// discarded. It does not touch the engine or save.
func (s *Session) Notify(text string) {
	if s == nil {
		return
	}
	s.notice = &panel.Notice{Kind: "warn", Text: safeNotice(text)}
}

func newCreationState(gameID string) *engine.GameState {
	branchID := defaultBranchID
	return &engine.GameState{
		SchemaVersion:  engine.SchemaVersion,
		RulesVersion:   engine.RulesVersion,
		ContentVersion: engine.ContentVersion,
		GameID:         gameID,
		BranchID:       branchID,
		Revision:       1,
		Phase:          engine.PhaseCreation,
		RNG:            engine.NewRNGSet(engine.SeedFromIdentity(gameID, branchID)).Snapshot(),
		World: &engine.World{
			NPCs:             map[string]engine.NPC{},
			VisitedLocations: []string{"cave_dwelling"},
			CurrentLocation:  "cave_dwelling",
			Quests:           map[string]engine.QuestState{},
			ConsumedEvents:   []engine.ConsumedEvent{},
			Flags:            map[string]bool{},
		},
		Pending: engine.PendingState{Creation: engine.NewCreationDraft(gameID, branchID)},
		Idempotency: engine.IdempotencyTable{
			Entries: map[string]engine.IdempotencyEntry{},
			Limit:   engine.DefaultIdempotencyLimit,
		},
	}
}

type durableStore struct {
	disk                *storage.SnapshotStore[engine.SaveEnvelope]
	current             *engine.GameState
	logTail             []string
	lastDurableRevision uint64
}

func (s *durableStore) Load() (*engine.GameState, bool) {
	if s == nil || s.current == nil {
		return nil, false
	}
	return engine.CloneGameState(s.current), true
}

func (s *durableStore) Commit(state *engine.GameState) error {
	if s == nil || s.disk == nil || state == nil {
		return engine.ErrStoreCommit{Reason: "durable session store is unavailable"}
	}
	logs := append([]string(nil), s.logTail...)
	logs = append(logs, describeTransition(s.current, state))
	if len(logs) > logTailLimit {
		logs = append([]string(nil), logs[len(logs)-logTailLimit:]...)
	}
	env := envelopeFor(state, logs)
	if err := s.disk.Commit(env); err != nil {
		return engine.ErrStoreCommit{Reason: err.Error()}
	}
	s.current = engine.CloneGameState(state)
	s.logTail = logs
	s.lastDurableRevision = state.Revision
	return nil
}

func saveCodec() storage.Codec[engine.SaveEnvelope] {
	return storage.Codec[engine.SaveEnvelope]{
		New:      func() *engine.SaveEnvelope { return &engine.SaveEnvelope{} },
		Seal:     (*engine.SaveEnvelope).ComputeIntegrity,
		Verify:   (*engine.SaveEnvelope).VerifyIntegrity,
		Version:  func(env *engine.SaveEnvelope) int { return env.EnvelopeVersion },
		GameID:   func(env *engine.SaveEnvelope) string { return env.GameID },
		Revision: func(env *engine.SaveEnvelope) uint64 { return env.Revision },
		Validate: func(env *engine.SaveEnvelope) error { return validateEnvelope(env, env.GameID) },
	}
}

func envelopeFor(state *engine.GameState, logs []string) *engine.SaveEnvelope {
	stateCopy := engine.CloneGameState(state)
	env := &engine.SaveEnvelope{
		EnvelopeVersion: engine.EnvelopeVersion,
		SchemaVersion:   state.SchemaVersion,
		RulesVersion:    state.RulesVersion,
		ContentVersion:  state.ContentVersion,
		GameID:          state.GameID,
		BranchID:        state.BranchID,
		Revision:        state.Revision,
		State:           *stateCopy,
		PendingChoices:  pendingChoices(state),
		Consumed:        consumedLedger(state),
		LogTail:         append([]string(nil), logs...),
	}
	return env
}

func validateEnvelope(env *engine.SaveEnvelope, gameID string) error {
	if env == nil {
		return errors.New("save envelope is nil")
	}
	if env.EnvelopeVersion != engine.EnvelopeVersion || env.GameID != gameID || env.State.GameID != env.GameID ||
		env.BranchID == "" || env.State.BranchID != env.BranchID || env.Revision != env.State.Revision {
		return errors.New("save envelope identity or revision does not match its state")
	}
	if env.SchemaVersion != engine.SchemaVersion || env.RulesVersion != engine.RulesVersion || env.ContentVersion != engine.ContentVersion {
		return errors.New("save was produced by an unsupported schema, rules or content version")
	}
	if report := engine.ValidateState(&env.State); !report.OK() {
		return report.Error()
	}
	if report := engine.ValidateSerializeReady(&env.State); !report.OK() {
		return report.Error()
	}
	return nil
}

func pendingChoices(state *engine.GameState) []engine.FrozenChoiceSet {
	if state == nil {
		return nil
	}
	var out []engine.FrozenChoiceSet
	if state.Pending.Event != nil {
		out = append(out, engine.FrozenChoiceSet{Source: "event", InstanceID: state.Pending.Event.InstanceID,
			ChoiceIDs: append([]string(nil), state.Pending.Event.ChoiceIDs...)})
	}
	if state.Pending.Breakthrough != nil {
		out = append(out, engine.FrozenChoiceSet{Source: "breakthrough", InstanceID: state.Pending.Breakthrough.ParentActionID,
			ChoiceIDs: append([]string(nil), state.Pending.Breakthrough.ChoiceIDs...)})
	}
	return out
}

func consumedLedger(state *engine.GameState) engine.ConsumedLedger {
	ledger := engine.ConsumedLedger{Options: map[string]string{}}
	if state == nil {
		return ledger
	}
	ledger.ActionIDs = append([]string(nil), state.Idempotency.Order...)
	if state.World != nil {
		for _, event := range state.World.ConsumedEvents {
			ledger.EntryIDs = append(ledger.EntryIDs, event.InstanceID)
			ledger.Options[event.InstanceID] = event.ChoiceID
		}
	}
	return ledger
}

func describeTransition(before, after *engine.GameState) string {
	if after == nil {
		return "进度已保存。"
	}
	if before == nil {
		return "创角草稿已自动保存；尚未消耗游戏时间。"
	}
	if before.Player == nil && after.Player != nil {
		return "角色创建完成；创角没有消耗游戏时间。"
	}
	if before.Player != nil && after.Player != nil && before.Player.XP != after.Player.XP {
		gain := after.Player.XP - before.Player.XP
		return "修炼修为 +" + formatFixed(gain) + "；当前 " + formatFixed(after.Player.XP) + "。行动已自动保存。"
	}
	if before.Phase == engine.PhaseCreation && after.Phase == engine.PhaseCreation && before.Pending.Creation != nil && after.Pending.Creation != nil {
		return fmt.Sprintf("创角草稿已保存（第 %d 步）。", after.Pending.Creation.Step)
	}
	if after.Counters.WorldMonth > before.Counters.WorldMonth {
		return "游戏时间推进 1 个月；行动已自动保存。"
	}
	return "进度已安全保存。"
}

func recentChanges(logs []string) []panel.ChangeLine {
	start := len(logs) - 2
	if start < 0 {
		start = 0
	}
	out := make([]panel.ChangeLine, 0, len(logs)-start)
	for _, line := range logs[start:] {
		out = append(out, panel.ChangeLine{Field: "save.log", Text: safeNotice(line), Direction: "flat"})
	}
	return out
}

func safeNotice(value string) string {
	var b strings.Builder
	for _, r := range value {
		if r == '\n' || r == '\r' || r == '\t' || r < 0x20 || (r >= 0x7f && r <= 0x9f) {
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

func friendlyError(code engine.ErrorCode) string {
	switch code {
	case engine.ErrPreconditionUnmet:
		return "当前条件不满足。"
	case engine.ErrBadPhase:
		return "当前阶段不能执行这项操作。"
	case engine.ErrSerialization:
		return "存档校验失败。"
	default:
		return "操作被拒绝。"
	}
}

func formatFixed(value int64) string {
	negative := value < 0
	var magnitude uint64
	if negative {
		magnitude = uint64(-(value + 1)) + 1
	} else {
		magnitude = uint64(value)
	}
	whole := magnitude / uint64(engine.SCALE)
	fraction := magnitude % uint64(engine.SCALE)
	text := fmt.Sprintf("%d", whole)
	if fraction != 0 {
		decimals := strings.TrimRight(fmt.Sprintf("%04d", fraction), "0")
		text += "." + decimals
	}
	if negative {
		return "-" + text
	}
	return text
}

func nonEmpty(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func vitalsView(v engine.Vitals) panel.VitalsView {
	view := panel.VitalsView{Current: v.Current, Max: v.Max, Text: formatFixed(v.Current) + " / " + formatFixed(v.Max)}
	if v.Max > 0 {
		view.RatioPercent = int(v.Current * 100 / v.Max)
		if view.RatioPercent > 100 {
			view.RatioPercent = 100
		}
	}
	return view
}

func tierLabel(tier engine.RealmTier) string {
	switch tier {
	case engine.TierEarly:
		return "初期"
	case engine.TierMiddle:
		return "中期"
	case engine.TierLate:
		return "后期"
	case engine.TierPerfection:
		return "圆满"
	default:
		return string(tier)
	}
}

func chineseMonth(month int64) string {
	labels := [...]string{"", "正月", "二月", "三月", "四月", "五月", "六月", "七月", "八月", "九月", "十月", "冬月", "腊月"}
	if month < 1 || month >= int64(len(labels)) {
		return fmt.Sprintf("%d 月", month)
	}
	return labels[month]
}

func originLabel(cat *engine.Catalogue, origin engine.Origin) string {
	for _, item := range cat.Origins {
		if item.ID == origin {
			return item.NameZH
		}
	}
	return string(origin)
}

func spiritRootLabel(cat *engine.Catalogue, root engine.SpiritRoot) string {
	for _, item := range cat.SpiritRoots {
		if item.ID == root {
			return item.NameZH
		}
	}
	return string(root)
}

func themeLabel(theme string) string {
	if theme == "paper" {
		return "宣纸"
	}
	return "青玉"
}

func colorModeLabel(mode string) string {
	switch mode {
	case "basic":
		return "基础色"
	case "none":
		return "无色"
	default:
		return "自动"
	}
}

func boolLabel(v bool) string {
	if v {
		return "开"
	}
	return "关"
}
