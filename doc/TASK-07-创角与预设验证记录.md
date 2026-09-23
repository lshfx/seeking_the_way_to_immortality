# TASK-07 双步骤创角与合法预设 — 验证记录

- **任务：** [TASK-07 实现双步骤创角与合法预设](问道长生_分阶段开发方案.md)（P0｜玩法/交互｜依赖 TASK-04/05/06）
- **状态：** 交付完成 ✅（2026-09-23）
- **规则来源：** [ADR-001 §2 / §2.1](decisions/ADR-001-m1-product-freeze.md)、设计文档 6.1 / 7.1 / 12.1。
  **所有数值均从上述文档重新读取，未凭记忆书写。**

## 1. 交付物

| 交付项 | 位置 |
|---|---|
| 角色工厂 | `internal/engine/creation_factory.go`（`CreatePlayer`、`DeriveFor`） |
| 输入校验 | `internal/engine/creation_validate.go`（`ValidateCreation`）、`creation.go`（`ValidateBase`） |
| 创角状态机 | `internal/engine/pipeline.go` `applyCreateEdit` / `applyCreateConfirm`；`creation_factory.go` `NewCreationDraft` / `FixAllocation` |
| 预设 | `internal/engine/creation_factory.go` `M1CreationPresets`（3 组） |
| 页面模型 | `internal/panel/creation.go`（`BuildCreationBlock`）、`internal/engine/creation_view.go`（`BuildCreationView`）、`internal/wiring/creation.go`（桥接） |

新增契约文件：`internal/engine/creation.go`（常量、错误码、报告、固定键、`checkFixedNotChanged`）。
测试：`internal/engine/creation_test.go`（14 个测试）、`internal/wiring/creation_test.go`（6 个测试）。
反证工具：`tools/task07-counterproof/counterproof.py`（14 项变更，**14/14 CAUGHT**）。

## 2. 验收逐条对应

| 验收项 | 实现位置 | 测试 |
|---|---|---|
| 第一屏署名“作者：雾见川” | `CreationAuthorLine`（engine + panel 双声明） | `TestFirstScreenCarriesAuthorByline`、`TestAuthorBylineDoesNotDrift`、`TestFirstScreenCarriesTheByline` |
| 基础 60 点 | `ValidateBase` 比较 `b.Total() != CreationBasePoints` | `TestBaseTotalMustBeExactlySixty`（59/60/61 三例） |
| 59/61 不可提交 | 同上，经真实管线 | `TestPipelineRefusesFiftyNineAndSixtyOneAndTwentyOne`（含 draft 未被改动的断言） |
| 单项 1～15 | `ValidateBase` 逐字段检查 | `TestBaseRangeBoundaries`（0 与 16） |
| 初始最终 1～20 | `validateFinalRange`（基础 + 出身/体质/天赋授予） | `TestFinalTwentyOneIsRefusedSeparately`（15 + 6 天赋 = 21） |
| 超限 21 不可提交 | 同上，经真实管线 | `TestPipelineRefusesFiftyNineAndSixtyOneAndTwentyOne/final-twenty-one` |
| 自定义性别不被强改 | `ApplySelection` 原样写入；**无任何归一化分支** | `TestCustomGenderIsNotCoerced`（值 `无性别/自定`） |
| 创角不耗月 | `applyCreateConfirm` 断言 `WorldMonth` 未动；`MonthCostApplied = 0` | `TestCreationCostsNoMonth`（同时断言随机流计数未变） |
| 刷新/恢复不重复随机初始化 | `FixedResults` + `checkFixedNotChanged` | `TestResumeDoesNotReRoll`（JSON 往返后重放，改动固定值被拒） |
| 年龄 16～60 | `ValidateCreation` 年龄检查 | `TestCreationAgeBoundaries`（15/16/60/61） |
| 妖族延后判定与失败回退 | `YaoIntent` 先记录；`YaoVerdictPermille`+`YaoVerdictGiven` 后判定并锁定 | `TestYaoIntentJudgmentIsDeferredAndLocked` |
| 预设 3 次内确认 | `NewCreationDraft` 预置默认预设 | `TestPresetConfirmsInThreeActions` |
| 预设无隐藏优势 | 预设走同一 `ValidateCreation` | `TestPresetsAllPassTheSameValidation` |
| 未开放道途标记而非隐藏 | `pathOptions` 标 `Disabled` + `Reason` | `TestClosedPathsAreMarkedNotHidden` |

**基准算例复核（设计 7.1）：** 用真实 M1 目录跑默认预设得
`资质=10、真灵根、月增=195000 子单位（19.5）`；加先天道体得 `292500（29.25）`；
寿元 100 年、年龄 252 月（21 岁）。三个数字与文档逐一相符。

## 3. 反证纪律

工具 `tools/task07-counterproof/counterproof.py`，机制沿用 TASK-06 已验证的四条铁律：
行尾**探测而非假定**、替换必须**恰好命中一次**、有**基线污染检测**、
捕获必须满足**编译成功 + 有测试运行 + 有测试失败**。

```
CAUGHT   age-range-removed                  1 of 1 tests failed
CAUGHT   aptitude-factor-dropped            2 of 3 tests failed
CAUGHT   author-byline-wrong                1 of 1 tests failed
CAUGHT   base-range-removed                 1 of 1 tests failed
CAUGHT   base-total-budget-removed          3 of 8 tests failed
CAUGHT   closed-path-shown-available        1 of 1 tests failed
CAUGHT   custom-gender-coerced              1 of 1 tests failed
CAUGHT   final-range-removed                1 of 5 tests failed
CAUGHT   fix-allocation-not-recorded        2 of 2 tests failed
CAUGHT   fixed-guard-removed                1 of 1 tests failed
CAUGHT   fixed-results-not-checked          1 of 1 tests failed
CAUGHT   path-open-check-removed            1 of 1 tests failed
CAUGHT   preset-not-validated               2 of 2 tests failed
CAUGHT   spirit-root-multiplier-ignored     2 of 3 tests failed

14/14 CAUGHT
```

### 3.1 反证暴露的两处真实缺口（已修）

1. **`aptitude-factor-dropped` 首次 NOT CAUGHT。** 原 `TestConfirmProducesAValidPlayer`
   只断言 `CultivationRate > 0`；而该夹具里资质因子与灵根倍率之外的多数字段都乘 1.0，
   把资质因子整个删掉后速率仍为正，断言看不见。
   **修复：** 新增 `TestCultivationRateExactBaseline`，直接钉住 `195000`（19.5）与
   `390000`（闭关 39），并新增两个"随资质/灵根单调"的测试。复跑确认 CAUGHT。
   *教训：只断言"为正"对乘法公式等于没有断言。*

2. **`age-range-removed` 首次 NOT CAUGHT。** 年龄规则当时没有任何测试覆盖。
   **修复：** 新增 `TestCreationAgeBoundaries`（15/16/60/61，同时走校验器与真实管线）。复跑确认 CAUGHT。

### 3.2 一次自我纠正：反证本身写错

`m_fixed_key_namespace_collapsed` 首次 NOT CAUGHT。核查后确认**变更本身是语义空操作**：
两个前缀同时置空后，`"strength"` 与 `"yao_verdict"` 依旧不冲突，行为完全未变，
测试"没失败"是正确答案。已删除该变更，改为把固定值守卫真正短路（`m_fixed_guard_removed`），
复跑 CAUGHT。
*这正是工具文档里那条规则的应用：NOT CAUGHT 时先怀疑观测，再怀疑变更；此处两者都查，结论是变更错了。*

### 3.3 一处结构性调整

`checkFixedNotChanged` 原在 `pipeline.go`。反证工具因其不在受护文件列表内而无法守护，
且该规则本属"草稿形状"而非"命令派发"。已迁入 `creation.go` 与其固定键契约同处，
既让守卫可被守护，也让归属更正确。

## 4. 关键设计决定

- **工厂在提交前运行。** 旧 `applyCreateConfirm` 有一处 `s.Player == nil` 前置检查，
  但**创建阶段没有任何代码路径会写入 `s.Player`**，该分支在构造上不可达。
  现在工厂在克隆上先造角色，再清草稿、翻相位，因为
  `validatePhasePendingConsistency` 要求 `Phase==CREATION` 时草稿非空，
  而 `ValidateState` 只在 `CREATION` 容忍 `Player==nil`。
  **失败时整份克隆被丢弃，相位与草稿原样保留**，所以"失败可改选"成立。

- **零值歧义的处理。** `YaoVerdictPermille` 是 int，无法区分"未提供判定"与"判定为 0"。
  首次实现把 0 当真实判定，导致**第一次编辑就锁死一个失败判定**——测试当场抓到。
  已增加显式 `YaoVerdictGiven` 布尔。同类零值在 `mergeSelection` 处是安全的，
  因为 0 不是合法属性值（下限 1）、不是合法年龄（下限 16）、不是合法 id（空串会先报未知名）。

- **字段 id 冲突。** 面板同时渲染"根骨"属性行与"体质"内容选择，二者原先 id 都是
  `constitution`，渲染器按 id 查行会拿到错的那一行。内容选择 id 已加 `choice_` 前缀，
  并新增 `TestNoDuplicateCreationFieldIDs` 守住这类回归。

- **`panel` 保持零依赖。** `engine` 的边界测试会导入 `panel`，所以 `panel` 反向导入
  `engine` 会成环。创角页因此由三方分工：`engine.BuildCreationView` 出已解析的值、
  `panel.BuildCreationBlock` 只排版、`internal/wiring` 做两者桥接（该包本就是
  "唯一允许同时导入两侧"的地方），并承担署名等重复常量的漂移守卫。

- **数据驱动授予，顺序无关。** 出身/体质/天赋的 `GrantEffect` 先全部折进
  `grantAccumulator`（加法与乘法分桶），再一次性算最终值，避免"编辑列表顺序改变结果"。
  同一效果不重复应用。

- **未测平台不宣称支持。** 本任务仍只在 Windows x64 验证。

## 5. 复现方式

```bash
# 全量测试
go test ./...

# 反证（需先记录一次基线）
python3 tools/task07-counterproof/counterproof.py --record-baseline
python3 tools/task07-counterproof/counterproof.py --check-coverage
python3 tools/task07-counterproof/counterproof.py --all
```

`Go 1.26.0`；`CGO_ENABLED=0`。反证运行前后须 `--check-baseline` 为 `baseline matches`。

## 6. 已知边界与未做

- **妖族加成本身未配置。** 本任务只实现"先记录意向、后判定、判定锁定、失败可改选"的
  **流程与防线**；妖族在六维之外如何加成属内容配置，M1 未提供具体数值，
  因此判定通过后与不通过的唯一差别是能否确认，不含隐藏倍率。
- **天赋点数是待平衡补值。** 设计 6.1 的"天赋默认 5 点、各正面天赋暂定 1 点、
  体弱多病返还 2 点"尚未定案，故只做**存在性与互斥**校验，不校验点数收支。
- **`internal/session` 仍为桩。** 本任务交出的是可被调用的模型与桥接；
  正式会话编排（命令去重、确认、事务）属 TASK-09。
- **`internal/panel` 无独立测试文件**，其行为由 `internal/wiring` 的测试覆盖；
  这是因为 `panel` 零依赖、无法自建夹具。
- **本地提交未推送。**
