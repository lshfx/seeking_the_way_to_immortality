# TASK-12 伤势、寿元、疗伤与因果 — 验证记录

- **状态：** 已完成 ✅（2026-09-24）
- **依赖：** TASK-08（修炼公式与派生值）、TASK-10（效果 DSL）、TASK-11（场景层）
- **规则依据：** [游戏与技术设计文档 §6.2、§7.4、§13.1、§13.2](../问道长生_游戏与技术设计文档.md)、[ADR-001 R15/R19](decisions/ADR-001-m1-product-freeze.md)
- **范围：** 伤势区间、异常（伤势/中毒/内伤/心境异常）、月末异常结算、疗伤行动、寿元守卫、因果分档与状态详情投影。

## 1. 交付内容

| 交付 | 实现位置 |
|---|---|
| 伤势区间与区间惩罚 | `internal/engine/injury.go`（`InjuryBandFor`、`InjuryPenaltyPermille`） |
| 派生属性重算（幂等） | `internal/engine/injury.go`（`refreshDerived`、`applyPermillePenalty`） |
| 异常目录（取代空的 `CultivationModifiers`） | `internal/engine/content.go`（`AfflictionDefinition`）、`internal/content/cast.go`（`m1Afflictions`） |
| 月末异常结算 | `internal/engine/heal.go`（`settleAfflictions`）、`internal/engine/pipeline.go` |
| 疗伤行动（养伤 / 气血换灵力） | `internal/engine/heal.go`（`applyHeal`）、`internal/engine/pipeline.go` |
| 寿元守卫 | `internal/engine/karma.go`（`hasLifespanLeft`）、`internal/engine/pipeline.go` |
| 因果分档与状态详情 | `internal/engine/karma.go`（`KarmaTierFor`、`BuildStatusDetail`） |
| 内容校验 | `internal/engine/validate.go`（`validateAfflictions`、`validateInjuryBands`、`validateKarmaTiers`、疗伤数值） |
| 反证工具（18 项变异） | `tools/task12-counterproof/counterproof.py` |

### 「不重复」是靠重算而不是靠小心

设计 6.2 给重伤一个遁速惩罚，验收要求它「不重复」。实现上这不是一个 guard，而是一个结构选择：**速度每次都从敏捷重算**，再套用区间与异常惩罚。累加式实现在被调用一次时同样正确，被调用两次就错；重算式在任何调用次数下都给出同一答案。

攻击与防御刻意**不**参与重算：它们由内容效果直接写入（`attack` / `defense` 目标），从属性重算会把已授予的加成悄悄抹掉。速度没有对应的授予目标，因此它是唯一适合由这里拥有的派生值。

## 2. 验收结果

| 验收项 | 结果 | 证据 |
|---|---|---|
| 0/33%/66% 及小数边界无空隙 | 通过 | `TestInjuryBandsPartitionTheHealthRange`（对 100、99、101、7、400000 五个上限穷举每一个整数）、`TestInjuryBandBoundariesAreInclusiveOnTheLowerSide` |
| 重伤遁速惩罚不重复 | 通过 | `TestHeavyWoundSpeedPenaltyIsNotRepeated`（含「惩罚确实生效」的对照组）、`TestHealthyCharactersCarryNoInjuryPenalty`、`TestBandAndAfflictionPenaltiesAdd` |
| 21 岁炼气余 79 年、同龄筑基余 179 年 | 通过 | `TestLifespanRemainingMatchesTheDesignExamples`、`TestShippedLifespanCeilingsMatchTheDesign` |
| 突破替换上限而非累加 | 通过 | `TestBreakthroughReplacesTheCeilingRatherThanAddingToIt` |
| 切出/离线不会掉寿元 | 通过 | `TestOnlyASettledMonthAgesTheCharacter`（遍历七种零月命令，逐一断言年龄与寿元上限不变）、`TestSaveRoundTripPreservesAgeAndLifespan` |
| 开始时剩余 0 不能行动 | 通过 | `TestNoLifespanLeftRefusesMonthActions` |
| 疗伤不清除无关异常 | 通过 | `TestRestDoesNotClearUnrelatedAfflictions`、`TestShippedPoisonIsNotCuredByRest` |
| 疗伤恢复气血且不超上限 | 通过 | `TestRestRestoresHealthUpToTheCeiling`、`TestRestNeverExceedsTheCeiling` |
| 气血换灵力按比例兑换且不能自杀 | 通过 | `TestConversionBurnsHealthForSpirit`、`TestConversionRefusesToLeaveNoHealth`、`TestConversionRefusesWhenSpiritIsAlreadyFull` |
| 月末异常扣血、到期不重复扣 | 通过 | `TestAfflictionsDrainHealthAtMonthEnd`、`TestAnAfflictionThatEndsThisMonthDoesNotAlsoDrain` |
| 无人救治的毒可致命 | 通过 | `TestAnUntreatedPoisonCanBeFatal`（终局原因为 `INJURY`，且在结果事件中报告） |
| 因果按行为情境结算 | 通过 | `TestKarmaIsNotChangedByAnyAutomaticRule`（一个完整月 + 移动 + 修炼后，功德/业力/声望/贡献四项均不变） |
| 因果分档与顺序无关 | 通过 | `TestKarmaTiersAreOrderedAndEveryTotalIsDescribed`、`TestKarmaTierDoesNotDependOnCatalogueOrder` |
| 状态详情投影 | 通过 | `TestStatusDetailProjectsBandLifespanAndAfflictions`、`TestStatusDetailReportsNoNegativeRemainingLifespan`、`TestStatusDetailSurvivesAMissingCharacter` |
| 装运内容覆盖四种异常且至少一种不可静养 | 通过 | `TestShippedAfflictionsCoverAllFourKinds`、`TestAtLeastOneShippedAfflictionNeedsMoreThanRest` |
| 装运内容：每个区间已配置、因果分档有零点 | 通过 | `TestShippedInjuryBandsCoverEveryBand`、`TestShippedKarmaTiersStartAtZero`、`TestShippedHealNumbersAreUsable` |
| 创角授予真的落到角色身上 | 通过 | `TestCreationGrantsReachTheCharacter` |

`go test ./...` 全绿，`go vet ./...` 通过，`go build ./...` 通过。

## 3. 反证清单（18/18 CAUGHT）

工具：`python3 tools/task12-counterproof/counterproof.py --all`。

| # | 变异 | 回退了什么性质 | 结果 |
|---:|---|---|---|
| 1 | `band-threshold-truncated` | 区间阈值精确比较（**本轮真实缺陷**） | CAUGHT 1/1 |
| 2 | `band-boundary-exclusive` | 边界归属较严重的一侧 | CAUGHT 2/2 |
| 3 | `band-penalty-accumulated` | 惩罚由重算得出 | CAUGHT 1/1 |
| 4 | `band-penalty-not-applied` | 惩罚确实生效 | CAUGHT 1/1 |
| 5 | `rest-clears-everything` | 疗伤只清除可静养异常 | CAUGHT 1/1 |
| 6 | `rest-restores-nothing` | 养伤恢复气血 | CAUGHT 1/1 |
| 7 | `conversion-can-kill` | 换灵力不得耗尽气血 | CAUGHT 1/1 |
| 8 | `conversion-gains-nothing` | 换灵力真的换到灵力 | CAUGHT 1/1 |
| 9 | `afflictions-do-not-drain` | 月末异常扣血 | CAUGHT 1/1 |
| 10 | `afflictions-drain-on-expiry` | 到期月不重复扣 | CAUGHT 1/1 |
| 11 | `poison-death-not-reported` | 气血耗尽即终局 | CAUGHT 1/1 |
| 12 | `lifespan-guard-removed` | 剩余寿元为 0 不能行动 | CAUGHT 1/1 |
| 13 | `travel-ages-the-character` | 只有结算月份才增龄 | CAUGHT 1/1 |
| 14 | `karma-granted-automatically` | 引擎没有自动因果规则 | CAUGHT 1/1 |
| 15 | `karma-tier-lowest-wins` | 分档取最高匹配门槛 | CAUGHT 1/1 |
| 16 | `remaining-lifespan-negative` | 剩余寿元不为负 | CAUGHT 1/1 |
| 17 | `creation-attack-grant-dropped` | 创角授予落地 | CAUGHT 1/1 |
| 18 | `shipped-poison-cured-by-rest` | 装运的毒不可静养 | CAUGHT 2/2 |

### 反证自身暴露的问题（首轮 2 项 NOT CAUGHT，已修正）

1. `band-threshold-truncated` → **BUILD**：变异体把 `switch` 嵌套进了 `switch`，编译失败。变异体缺陷，已修正（把外层 `switch` 一并纳入替换）。
2. `conversion-can-kill` → **PASS**：这是**测试不敏感**，而且很典型。回退守卫后，命令仍然被拒绝——但拒绝它的是第 8 步的不变量检查（气血变负），而不是守卫本身。原测试只断言「被拒绝」与「气血未变」，两种实现下这两点都成立。已改为断言拒绝**代码**（`ErrPreconditionUnmet`），于是「因为别的原因被拒绝」不再算通过。

### 反证暴露的产品缺陷（2 项，已修）

1. **伤势区间在非整除数时判错。** 初版用 `heavyThreshold := max*33/100` 预计算阈值，截断使边界移动：上限 99 时阈值算成 32，于是 `hp=32`（32.3%，明确落在 (0,33%) 内）被判为**重伤**而非垂死。穷举测试直接抓到。修法：改用交叉相乘 `hp*100 < max*33`，对任何上限都精确。
2. **创角工厂静默丢弃四个目标。** `grantAccumulator.apply` 对不认识的目标「忽略而非报错」，而 `attack`、`defense`、`xp`、`breakthrough.*` 这四类**装运内容实际在用**的目标都不在它认识的集合里。校验器只查白名单，而这四个都在白名单内，所以内容通过了校验、被接受、然后什么都不做——作者看不到错误，玩家看不到效果。已补齐折叠，并补 `TestCreationGrantsReachTheCharacter` 断言授予**落到成品角色身上**（而不是断言累加器，因为丢弃正是在那里不可见的）。

## 4. 版本与兼容性

- `RulesVersion 4 → 5`：疗伤从空动作变成真实结算；异常在月末扣血；气血在非遭遇情境下耗尽即终局；剩余寿元为 0 时拒绝耗月行动。状态形状未变，是既有字段上的新规则。
- `ContentVersion 4 → 5`：空的 `CultivationModifiers` 列表被更宽的 `Afflictions` 目录取代，并新增 `InjuryBands`、`KarmaTiers`、`HealRestorePermille`、`HealConvertBurnPermille`。
- `SchemaVersion` **不变**（仍为 4）：没有新增或改型状态字段，摘要的规范形式也未变。

**仍未加迁移适配器**：与 TASK-08/10/11 的处置一致，M1 尚无已发行的玩家存档。

## 5. 已知边界（诚实记录，勿误认为已覆盖）

1. **忍伤不是一个新命令。** 设计 13.1 的时间成本矩阵只给「疗伤」一行，所以本任务把它实现为**不做疗伤**：带伤继续修炼或历练，惩罚照常生效。给它一个独立的命令种类会超出矩阵，而矩阵是 TASK-01 冻结的。
2. **气血换灵力接的是角色灵力（MP），不是战斗灵力。** 设计第 299 行把「五行灵气」与「人物灵力」分开；后者是 `Player.MP`，前者是战斗内的 `SpiritEnergy`。战斗侧的换取属 TASK-15。
3. **零气血的遭遇内后果未实现。** 设计 6.2 说气血 0 的后果来自遭遇预置规则（切磋停止、被俘、已存在的救援）。遭遇内没有规则可循时才是死亡；本任务实现的是**遭遇外**的那一支。遭遇内的分支属 TASK-15。
4. **内伤与中毒的解除路径尚未接通。** 二者都不是可静养的，需要解药或另行调理——解药是 TASK-13 的物品，调理属 TASK-16。当前它们只能靠时间到期自然结束。
5. **状态详情已投影但未渲染。** `BuildStatusDetail` 是引擎侧投影，把它画到终端上是 TASK-17 的集成工作。
6. **因果只有描述，没有结算规则。** 这是刻意的：R19 禁止「对所有击杀一刀切」，所以引擎不拥有任何自动因果规则，因果只由内容中具名的效果改变。分档用于把数字讲成话。
7. **伤势区间惩罚只配了遁速。** 设计只点名遁速，攻击与防御的惩罚没有被配置——编一个数字会是一个没人做过的平衡决定。
8. Windows x64 是当前已验证环境；macOS/Linux 仍不声明支持。

## 6. 验证命令

```powershell
go build ./...
go vet ./...
go test ./...
python3 tools/task12-counterproof/counterproof.py --check-coverage
python3 tools/task12-counterproof/counterproof.py --record-baseline
python3 tools/task12-counterproof/counterproof.py --all
```

2026-09-24 执行结果：全部 Go 测试通过，`go vet ./...` 通过，反证 **18/18 CAUGHT**。
