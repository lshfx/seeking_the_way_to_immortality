# TASK-10 事件调度与白名单条件 — 验证记录

- **状态：** 已完成 ✅（2026-09-24）
- **依赖：** TASK-04（状态契约与内容目录）、TASK-05（命令管线与确定性随机）
- **规则依据：** [游戏与技术设计文档 §9.3–9.4、§13.2、§13.3、§14](../问道长生_游戏与技术设计文档.md)、[ADR-001 R17/R19/R20](decisions/ADR-001-m1-product-freeze.md)
- **范围：** 引擎侧的调度器、白名单效果 DSL、内容与状态校验。十二个事件节点（EVT-001～012）的文案、选项与数值已由 TASK-04 随内容目录交付；**本任务不新增剧情**，只让它们真正跑起来。

## 1. 交付内容

| 交付 | 实现位置 |
|---|---|
| 月末调度器：到期、必发、基础检定、加权抽取、队列、提升 | `internal/engine/events.go` |
| 白名单效果 DSL：授予、成本、前置条件求值 | `internal/engine/effects.go` |
| 事件队列与实例台账状态 | `internal/engine/command.go`（`QueuedEvent`）、`internal/engine/state.go`（`RaisedEvent`） |
| 月末接入设计 13.2 第 5 步 | `internal/engine/pipeline.go` 的 `settleParentAction` |
| 事件选择真实结算：条件、成本、效果、后继节点分支 | `internal/engine/pipeline.go` 的 `applyEventChoice` |
| 基础检定概率配置化 | `internal/engine/content.go`、`internal/content/catalogue.go` |
| 必发事件标记 | `internal/engine/content.go`（`EventDefinition.Forced`）、`internal/content/m1.go`（EVT-001） |
| 事件内容校验与状态校验 | `internal/engine/validate.go` |
| 摘要覆盖与深拷贝 | `internal/engine/save.go`、`internal/engine/store.go` |
| 反证工具（25 项变异） | `tools/task10-counterproof/counterproof.py` |

调度顺序固定为 **到期 → 触发 → 提升**：先丢弃超期实例，再触发本月事件，最后才在「当前没有待决节点」时按优先级提升队首。顺序是契约而非风格——先提升再触发会让一个刚触发的实例插队到已经等了更久的实例前面。

## 2. 验收结果

| 验收项 | 结果 | 证据 |
|---|---|---|
| 每世界月一次基础 20% 检定 | 通过 | `TestBaseEventChanceMatchesTheDesignFigure`（10 万次，误差 ≤0.5 个百分点，整数断言）、`TestZeroBaseChanceNeverRaises`、`TestCertainBaseChanceAlwaysRaises` |
| 一次只展示一个待决节点，其余排队 | 通过 | `TestOnlyOneNodeIsShownAndTheRestQueue` |
| 玩家行动不被吞掉：待决节点阻塞月行动 | 通过 | `TestPendingNodeBlocksMonthActions`（`BAD_PHASE`，且月份未推进） |
| 危机优先级 | 通过（机制层） | `TestOnlyOneNodeIsShownAndTheRestQueue`（高优先级实例排在队尾仍被选中）、`TestPromotionTieIsBrokenByRaiseOrder`（同优先级按触发先后） |
| 查询不抽月奇遇 | 通过 | `TestQueriesDoNotDrawAMonthlyEvent`（世界月与世界流游标均不变） |
| 战斗招式不抽月奇遇 | 通过 | `TestCombatRoundsDoNotDrawAMonthlyEvent`（战斗轮 +1，世界月与世界流游标不变） |
| 现实等待不抽月奇遇 | 通过（构造性） | 不输入不结算月份；月末检定只在 `settleParentAction` 内，无输入无法到达 |
| DSL 不能执行文件、网络或任意脚本 | 通过 | `TestUnknownEffectTargetIsRejected`、`TestRuntimeEffectCannotNameACreationOnlyTarget`、`TestCreationEffectMayNameACreationOnlyTarget`（对照组）、`TestUnknownRuntimeEffectTargetIsRejectedAtRuntime` |
| 资格、冷却、限次 | 通过 | `TestCooldownBlocksARaiseUntilItElapses`、`TestMaxOccurrencesStopsFurtherRaises`、`TestTheSameEventIsNotQueuedTwice` |
| 排队、到期 | 通过 | `TestQueuedEventExpiresWhenItsDeadlinePasses`、`TestExpiredInstanceStillConsumesTheOccurrenceBudget` |
| 分支与后继节点 | 通过 | `TestBranchAdvancesTheInstanceWithoutSettlingASecondMonth` |
| 多事件排队不强制一次处理完，可存档退出 | 通过 | `TestAnsweringANodePromotesTheNextQueuedOne`（不额外耗月）、`TestQueueSurvivesASaveRoundTripWithoutRerolling` |
| 无合格事件时返回普通场景，不以空队列阻塞 | 通过 | `TestEmptyCatalogueDoesNotBlockPlay` |
| 选项前置条件未满足即拒绝 | 通过 | `TestUnmetChoiceConditionIsRejectedAndChangesNothing`（资源、修订号、待决实例均不变） |
| 成本不足拒绝且不自动负债 | 通过 | `TestUnaffordableChoiceIsRejectedWithoutBorrowing`（余额不变、负债为 0、物品未发放） |
| 成本与效果真实结算并入账 | 通过 | `TestChoiceCostsAndEffectsAreAppliedAndRecorded` |
| 心境越界被夹紧而非破坏不变量 | 通过 | `TestMoodGrantIsClampedToTheContractRange` |
| 内容校验：必发事件权重为 0、后继节点选项须已声明 | 通过 | `TestForcedEventMustNotAlsoBeDrawn`、`TestForcedEventWithZeroWeightIsAccepted`、`TestEventFollowUpOfferingAnUndeclaredChoiceIsRejected` |
| 状态校验：队列与实例台账必须一致 | 通过 | `TestQueuedInstanceWithoutALedgerRowIsRejected`、`TestQueuedInstanceWithAMatchingLedgerRowIsAccepted` |
| 装运目录的开局节点确实是必发 | 通过 | `TestM1OpeningEventIsForced`、`TestM1HasDrawableEvents` |
| 摘要覆盖新事件状态 | 通过 | `TestDigestCoversEventSchedulingState`（含已知为真的控制组） |

`go test ./...` 全绿（engine 包 438 项运行用例），`go vet ./...` 通过，`go build ./...` 通过。

## 3. 反证清单（25/25 CAUGHT）

工具：`python3 tools/task10-counterproof/counterproof.py --all`。判定沿用 TASK-06/07 的四条纪律：**构建失败不计为捕获**、**未运行不计为捕获**、**无失败不计为捕获**、**基线指纹防污染**。

| # | 变异 | 回退了什么性质 | 结果 |
|---:|---|---|---|
| 1 | `scheduler-not-invoked` | 整个月末调度（一行） | CAUGHT 3/3 |
| 2 | `base-chance-always-passes` | 基础检定恒真 | CAUGHT 1/1 |
| 3 | `base-chance-ignores-configured-zero` | 以「值 >0」而非「有出处」判断是否已配置 | CAUGHT 1/1 |
| 4 | `branch-resolves-immediately` | 忽略后继节点 | CAUGHT 1/1 |
| 5 | `combat-draws-a-monthly-event` | 让战斗轮也跑月末调度 | CAUGHT 1/1 |
| 6 | `cooldown-ignored` | 冷却 | CAUGHT 1/1 |
| 7 | `cost-affordability-not-prechecked` | 成本可支付性（两处冗余守卫同时回退） | CAUGHT 1/1 |
| 8 | `creation-only-target-allowed-at-runtime` | 创建期/运行期作用域区分 | CAUGHT 1/1 |
| 9 | `digest-ignores-the-queue` | 摘要对队列的覆盖 | CAUGHT 2/2 |
| 10 | `duplicate-queue-entry-allowed` | 同一事件不重复入队 | CAUGHT 1/1 |
| 11 | `expiry-forgets-the-occurrence` | 过期仍占用限次额度 | CAUGHT 1/1 |
| 12 | `followup-choices-not-checked` | 后继节点选项须已声明 | CAUGHT 1/1 |
| 13 | `forced-events-not-raised` | 必发事件 | CAUGHT 2/2 |
| 14 | `forced-weight-rule-removed` | 必发事件权重必须为 0 | CAUGHT 1/1 |
| 15 | `growth-not-deep-copied` | 克隆隔离 `Attributes.Growth` | CAUGHT 1/1 |
| 16 | `max-occurrences-ignored` | 限次 | CAUGHT 1/1 |
| 17 | `mood-not-clamped` | 心境夹紧 | CAUGHT 1/1 |
| 18 | `opening-node-not-forced` | 装运目录的开局节点是必发 | CAUGHT 1/1 |
| 19 | `pending-node-does-not-block-months` | 待决节点替换场景行动 | CAUGHT 1/1 |
| 20 | `promotion-ignores-priority` | 提升按优先级 | CAUGHT 1/1 |
| 21 | `queue-ledger-agreement-not-checked` | 队列与台账一致 | CAUGHT 1/1 |
| 22 | `queue-not-deep-copied` | 克隆隔离队列候选集 | CAUGHT 1/1 |
| 23 | `queue-not-drained-on-answer` | 应答后提升下一个 | CAUGHT 1/1 |
| 24 | `unknown-runtime-target-ignored` | 未知目标不得静默无事发生 | CAUGHT 1/1 |
| 25 | `whitelist-not-enforced` | 白名单校验 | CAUGHT 1/1 |

### 反证自身暴露的问题（首轮 4 项 NOT CAUGHT，已修正）

首轮运行有 4 项未捕获、1 项因变异体写错而崩溃。按项目纪律，**先怀疑观测与变异，不要先怀疑被测代码**——逐项核查后确认全部是反证/测试侧的问题：

1. `base-chance-always-passes` → **BUILD**：变异后 `chance` 成为未使用变量。变异体缺陷，已补 `_ = chance`。
2. `cost-affordability-not-prechecked` → **PASS**：可支付性有两处冗余守卫（写入前的预检 + 支付函数内的复查）。只回退一处，另一处仍然拒绝，所以测试照旧通过——这是**正确的 NOT CAUGHT**，要修的是变异。已改为同时回退两处。
3. `duplicate-queue-entry-allowed` → **PASS**：原测试的场景（应答后再过一月）根本走不到「同一事件已排队时再次触发」。**测试不敏感**，已重写为直接驱动调度器、构造「事件已在队列中且再次合格」的状态。
4. `promotion-ignores-priority` → **PASS**：原夹具里高优先级实例恰好也是先入队的，因此「按优先级」与「取队首」得到同一答案。**测试不敏感**，已把夹具改为「必发事件先入队但优先级更低」，使两种实现分离。
5. `queue-ledger-agreement-not-checked` → 崩溃：变异体参数写错（4 个位置参数）。工具缺陷，已修正。

### 反证暴露的产品缺陷（2 项，已修）

1. **配置为 0 与「未配置」被混为一谈。** 初版实现用 `EventBaseChancePermille.Value > 0` 判断目录是否配置了基础概率，于是「显式配置 0」被当作「未配置」而回落到默认 20%。`TestZeroBaseChanceNeverRaises` 直接抓到。修法：以 `Provenance != ""` 判断是否已配置——这与校验器里 `validateConfigValue` / `validateZeroAwareConfigValue` 的区分是同一个道理。
2. **`clonePlayer` 未深拷贝 `Attributes.Growth` 与 `Insights`。** 这是**既有隐患**：此前 `Growth` 只在创角工厂写入（工厂造的是全新 Player），运行时从不修改，所以漏拷贝一直无害。TASK-10 的效果 DSL 让事件可以写入 `attributes.*`，于是它立刻变成真缺陷——被拒绝的命令会通过克隆体改到已提交状态。`TestCloningDoesNotShareTheQueueBackingArrays` 抓到，已修，并同时补上 `BreakthroughBonuses` 的深拷贝。

## 4. 版本与兼容性

三项版本号同步升至 **3**，且这是一次不可分割的改动：

- `SchemaVersion 2 → 3`：新增 `PendingState.EventQueue`、`World.RaisedEvents`、`World.EventInstanceSeq`、`Player.BreakthroughBonuses`。
- `RulesVersion 2 → 3`：结算一个月现在还会过期队列、触发必发事件并运行基础检定，属于**时序**变化而非纯数据变化。
- `ContentVersion 2 → 3`：新增 `Forced` 标记与基础概率配置。

摘要覆盖同时扩展到：队列（含冻结候选集与到期月）、实例台账、已消费事件（含月）、世界开关、角色关系、突破累加器。**未加迁移适配器**：TASK-08 之后仍没有已发行的玩家存档，正式存档入口的跨版本升级验收留到 TUI/会话接线后独立进行（与 TASK-08 的处置一致）。

## 5. 已知边界（诚实记录，勿误认为已覆盖）

1. **到期与「同一事件不重复入队」在 M1 当前阶段规则下不可达。** 设计 12.4 让待决节点替换场景行动，因此有待决节点时无法结算月份；而月末与应答时都会提升队列，队列随即被抽干。于是「排队实例跨月存活」这一前提在正常游玩中不成立，两条守卫只能由**未来构建写出的存档**或人工编辑的状态触发。二者均已实现并在单元层测试（直接驱动调度器），但**不能声称它们经由正常游玩路径被验证过**。
2. **危机优先只建立了机制，没有实现寿尽/致命危机的计算。** 本任务固定了「优先级决定展示顺序」这条规则，并用契约夹具验证；TASK-09 记录中的「寿尽计算」属 TASK-12，实际危机接入在 12/17 验证（与任务卡的测试边界一致）。
3. **`breakthrough.*` 是累加器，不是已生效的加成。** EVT-012 的「心魔抗性 +15 / 稳定 +10」被记入 `Player.BreakthroughBonuses`，由 TASK-16 在突破尝试时消费。本任务不做「把 +15 立刻判成成功」——那正是设计 R20 禁止的临场裁定。
4. **`cultivation_rate` 被限定为创建期目标。** 它在运行期每月都会被重算覆盖，若允许事件授予，内容会静默失效。校验器按作用域拒绝（`ScopeRuntime`），并有对照组证明它在创建期仍合法。
5. **摘要仍有未覆盖字段**：NPC 年龄、`World.Quests`、待决战斗与待决突破子状态。与 TASK-08 记录的是同一类缺口，仍须与 schema 版本提升成对处理。
6. **`RaisedEvents` 无上限。** M1 一次游玩的事件量很小，但长期不清理会线性增长；留待内容量上来后处理。
7. Windows x64 是当前已验证环境；macOS/Linux 仍不声明支持（`console_other.go` 路径未测）。

## 6. 验证命令

```powershell
go build ./...
go vet ./...
go test ./...
python3 tools/task10-counterproof/counterproof.py --check-coverage
python3 tools/task10-counterproof/counterproof.py --record-baseline
python3 tools/task10-counterproof/counterproof.py --all
```

2026-09-24 执行结果：全部 Go 测试通过，`go vet ./...` 通过，反证 **25/25 CAUGHT**。全量测试与反证均使用仓库外的隔离 Go 编译缓存目录。
