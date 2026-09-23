# TASK-08 修炼成长与第一次节奏模拟 — 验证记录

- **状态：** 已完成 ✅（2026-09-23）
- **依赖：** TASK-07（已完成）
- **规则依据：** [游戏与技术设计文档 §7.1–7.2、§14.1](问道长生_游戏与技术设计文档.md)、[ADR-001 §2.1](decisions/ADR-001-m1-product-freeze.md)
- **范围：** 引擎规则和可复现数值模拟；TASK-09 才接入正式 TUI 和短循环。

## 1. 交付内容

| 交付 | 实现位置 |
|---|---|
| 固定点修炼公式、心境档位、地点环境、主辅功法 | `internal/engine/cultivation.go` |
| 普通月修炼、修为账本、熟练度与月末状态刷新 | `internal/engine/pipeline.go` |
| 同组增益相加、不同组独立相乘 | `internal/engine/creation_factory.go` 的 `grantAccumulator` |
| 默认主功法与初始心境 | `internal/engine/creation_factory.go` |
| 阈值、具名临时修正和技术效果配置契约 | `internal/engine/content.go`、`internal/engine/validate.go` |
| 资质批量模拟工具 | `tools/task08-sim/main.go` |
| 状态克隆、完整性摘要及版本 | `internal/engine/store.go`、`internal/engine/save.go`、`internal/engine/contract.go` |

玩家从 21 岁、炼气初期、修为 0、心境 50 和黄阶主功法“吐纳诀”开始。每次普通修炼按当前地点、有效资质、灵根、主功法、心境和具名增益计算一个游戏月；成长只以 `SCALE=10000` 整数子单位累计。抵达小阶阈值时只截断本次溢出，不自动突破；之后再次提交修炼会被拒绝且不扣月份。

辅助功法的品级不会再乘到主功法倍率上，但其配置化具名效果可参与相应增益组；主辅功法各按每个有效修炼月增加 1 点熟练度。熟练度增量是本任务的临时基线，后续平衡可调整。

心境从 0～100 分中读取；M1 阈值配置为 50：低于阈值倍率 0.5，等于阈值倍率 1.0，高于阈值倍率 1.2。地点环境从当前地点配置读取。有效资质修正和修炼效率修正都通过具名临时状态计算，过期时重新计算派生值，不改写角色基础资质。

## 2. 验收结果

| 验收项 | 结果 | 证据 |
|---|---|---|
| 普通基准 19.5 | 通过 | `TestCultivationExactBaselinesAndClosedCandidate`、`TestCultivationRateExactBaseline` |
| 闭关候选 39 | 通过（仅数值预览） | 同上；TASK-08 命令仍拒绝闭关强度，入口留给 TASK-23 |
| 先天道体普通 29.25 | 通过 | `TestCultivationNamedCreationBonusAndPrimarySecondaryRules` |
| 同组 +50% 与 +20% 合并为 +70%，跨组相乘 | 通过 | `TestCultivationIndependentBonusesSumWithinGroupsAndMultiplyAcrossGroups`、`TestGrantAccumulatorSumsBonusesByGroupWithoutOrderDependence` |
| 10 个月小数固定点累计无显示舍入漂移 | 通过 | `TestCultivationRetainsTenMonthsOfFixedPointFractions`，资质11的月增20.15，十个月为201.5 |
| 心境阈值上下档 | 通过 | `TestCultivationMoodBandsAndTemporaryModifierRestoreBaseAptitude` |
| 辅助功法不重复乘品级，主辅熟练度各增加1 | 通过 | `TestCultivationNamedCreationBonusAndPrimarySecondaryRules`、`TestCultivationGrantsProficiencyToActiveTechniquesAndOnlyNormalActionIsExposed` |
| 临时修正到期后恢复基础资质与修炼率 | 通过 | `TestExpiredCultivationModifierRestoresDerivedAptitudeAndRate` |
| 满额截断且不自动突破；已满时不扣月 | 通过 | `TestCultivationCapsAtThresholdAndRequiresExplicitBreakthrough` |
| 低/中/高资质到阈值行动数 | 通过 | `go run ./tools/task08-sim`，结果如下 |

### 首轮确定性模拟

三个角色都使用真灵根、普通地点、黄阶主功法、心境50、基础属性总和60，不加入道体或其他额外加成。结果是规则算例，不是随机环境下的通关统计。

| 资质 | 普通月增 | 到100所需修炼/月数 | 最后一次实际增加 | 阈值后自动突破 |
|---:|---:|---:|---:|---|
| 5 | 16.25 | 7 | 2.5 | 否 |
| 10 | 19.5 | 6 | 2.5 | 否 |
| 15 | 22.75 | 5 | 9 | 否 |

复现命令：

```powershell
go run ./tools/task08-sim
```

## 3. 持久化与兼容性

新增的主/辅功法选择会保存在 `Player`；修为、心境、属性修正、状态效果、熟练度和派生修炼率均纳入规范串。按 ADR-003 的约定，状态结构和摘要覆盖一起升级：`SchemaVersion=2`、`RulesVersion=2`、`ContentVersion=2`。旧版摘要不能用新规范串直接验证。

TASK-08 之前没有已经发行的可玩版本或玩家存档，因此本次没有添加从 Schema 1 读取并迁移至 Schema 2 的运行时适配器；正式存档入口在游戏 TUI/会话接线后仍须做独立升级验收。此限制不影响新建状态和当前版本的保存摘要。

## 4. 已知边界

- 正式 TUI、玩家可见的效率预测和保存/退出/恢复短循环属于 TASK-09。
- M1 命令管线只开放普通修炼；闭关倍率目前只能被平衡工具预览。
- M1 当前只配置默认主功法，没有已开放的辅助功法内容；模型、配置校验和引擎计算已支持后续辅助功法。
- 任务、事件、疗伤和突破仍由后续任务提供完整可玩闭环；本记录中的节奏模拟只到一个炼气小阶，不表示已验证“炼气3～10年”目标。
- Windows x64 是当前已验证开发环境；macOS/Linux 仍不声明支持。

## 5. 验证命令

```powershell
go test ./...
go vet ./...
go run ./tools/task08-sim
```

2026-09-23 执行结果：全部 Go 测试通过，`go vet ./...` 通过；模拟输出与上表一致。全量测试使用仓库外的隔离 Go 编译缓存目录，不产生源码树缓存文件。
