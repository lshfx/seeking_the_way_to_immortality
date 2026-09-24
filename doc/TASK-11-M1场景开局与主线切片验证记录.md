# TASK-11 M1场景、开局与主线切片 — 验证记录

- **状态：** 已完成 ✅（2026-09-24）
- **依赖：** TASK-07（创角）、TASK-10（事件调度与白名单条件）
- **规则依据：** [游戏与技术设计文档 §6.3、§13.1、§14](../问道长生_游戏与技术设计文档.md)、[ADR-001 §M1 内容范围](decisions/ADR-001-m1-product-freeze.md)
- **范围：** 让四个场景、三名具名 NPC、十二个事件节点与两条早期目标真正进入游玩。**不新增剧情**：文案与数值由 TASK-04 随目录交付，本任务补的是它们与游戏之间缺失的那半截。

## 1. 交付内容

| 交付 | 实现位置 |
|---|---|
| 节点正文（离线模板，1～3 句） | `internal/engine/content.go`（`EventDefinition.TextZH`）、`internal/content/m1.go` |
| 具名 NPC 年龄 | `internal/engine/content.go`（`NPCDefinition.InitialAgeYears`）、`internal/content/cast.go` |
| NPC 生成进世界 | `internal/engine/npc.go`（`SpawnNPCs`）、`internal/engine/pipeline.go`（创角确认时调用） |
| 场景移动 | `internal/engine/pipeline.go`（`applyTravel`） |
| 事件场景门控 | `internal/engine/events.go`（`eventMayRaise`） |
| 两条早期目标及其分支 | `internal/engine/content.go`（`EarlyGoalDefinition`）、`internal/content/m1.go`（`m1EarlyGoals`、EVT-010/EVT-011 分支选项） |
| 内容与状态校验 | `internal/engine/validate.go`（`validateEarlyGoals`、节点正文、NPC 年龄） |
| 摘要覆盖 | `internal/engine/save.go`（NPC 状态、当前位置、已访问地点） |
| 内容审计工具 | `tools/task11-audit/main.go` |
| 反证工具（18 项变异） | `tools/task11-counterproof/counterproof.py` |

### 为什么「生成 NPC」和「实现移动」属于本任务

TASK-04 交付了三名 NPC 与一张四节点连通图，但**没有任何生产代码读取它们**：`World.NPCs` 永远是空 map（`pipeline.go` 里唯一写它的地方是给已存在的 NPC 增龄），而 `TRAVEL` 是一个空动作，只清确认票据、不改 `CurrentLocation`。后果是三名具名 NPC 在实际游玩中不存在、`npc_available` 条件永远为假、对话不可达，四个场景之间的移动也只是菜单上的一行字。内容清单与「世界里有这些人、这些地方」是两件事，本任务补的是后者。

## 2. 内容交付表

`go run ./tools/task11-audit` 输出（同时作为构建闸门，不满足即非零退出）。

### 2.1 事件节点

| 节点 | 场景 | 选项数 | 风险预览 | 正文句数 | 备注 |
|---|---|---:|---:|---:|---|
| EVT-001 洞府开局 | cave_dwelling | 2 | — | 2 | 必发、教程、限次 1 |
| EVT-002 坊市委托告示 | market | 3 | — | 2 | |
| EVT-003 采药岔路 | herb_woods | 2 | 1 | 1 | 深入受伤约三成，事前给出 |
| EVT-004 委托交付 | market | 2 | — | 2 | 领报酬或留材料 |
| EVT-005 青云宗招募 | qingyun_sect | 2 | — | 2 | 入宗与散修都能成长 |
| EVT-006 宗门巡逻 | qingyun_sect | 2 | 1 | 2 | |
| EVT-007 坊市问药 | market | 3 | — | 2 | 余额不足只拒绝，不借贷 |
| EVT-008 同门切磋 | qingyun_sect | 2 | — | 2 | 不强制开战 |
| EVT-009 林中妖兽 | herb_woods | 3 | 1 | 2 | 交战／绕行／撤退，成本事前给出 |
| EVT-010 友人求助 | cave_dwelling | 3 | — | 2 | 守护所爱三条分支 |
| EVT-011 筑基准备 | cave_dwelling | 3 | — | 2 | 问道飞升三条分支；雷劫节点 |
| EVT-012 心魔抉择 | cave_dwelling | 2 | 1 | 2 | 雷劫节点；效果有配置依据 |

### 2.2 场景连通

| 地点 | 环境 | 安全 | 邻接 |
|---|---|---|---|
| cave_dwelling 洞府 | normal | 是 | market, herb_woods |
| market 坊市 | normal | 是 | cave_dwelling, qingyun_sect |
| qingyun_sect 青云宗 | rich | 是 | market, herb_woods |
| herb_woods 低危历练点 | normal | **否** | cave_dwelling, qingyun_sect |

四节点连通、邻接对称；唯一不安全的地点就是低危历练点，否则「安全节点」与「历练点」的区分没有意义。移动零月、不抽骰（设计 13.1：固定安全节点，不以往返刷新收益）。

### 2.3 NPC

| NPC | 来源 | 年龄 | 境界 | 寿元 | 阵营 | 所在地 | 台词 |
|---|---|---:|---|---:|---|---|---:|
| gu_qingxuan 顾清玄 | 原稿 | 42 | 炼气圆满 | 100 | 散修 | cave_dwelling | 2 |
| yunqi 云栖 | 原稿 | 63 | 筑基初期 | 200 | 青云宗 | qingyun_sect | 1 |
| steward_wen 温管事 | **新增设定** | 47 | 炼气后期 | 100 | 坊市 | market | 1 |

三人均标记成年；来源标记互斥（原稿 xor 新增设定），由校验器强制。设计 14 的「有固定事实资料」即此表：年龄、境界、寿元、阵营、性格、所在地、台词，全部随存档持久化并可被 `npc_available` 判定。

### 2.4 两条早期目标的分支

| 目标 | 成功 | 失败 | 放弃 |
|---|---|---|---|
| 问道飞升 goal_ascension | EVT-011 `begin`：备齐材料、开始筑基 | EVT-011 `cancel`：材料未备齐，这次没能开始 | EVT-011 `abandon_path`：明确不再走这条路 |
| 守护所爱 goal_protect | EVT-010 `help`：倾囊相助（30 灵石） | EVT-010 `tried_but_failed`：凑不齐，如实相告 | EVT-010 `politely_decline`：婉言相拒 |

失败分支刻意做成**一个可选选项**而不是一条报错：它的前置条件是「灵石 < 30」，也就是玩家恰好付不起时才出现。「我试过但没能帮上」因此是玩家能说出口的一句话，而不是「相助被系统拒绝」的副作用。

**散修可达性**（ADR-001：两个目标均可通过散修路径推进）：两个成功分支都不要求入宗——`help` 只要灵石，`begin` 只要筑基丹。校验器逐条检查「设置成功标记的选项及其所在事件都不得要求 `sect_member == 1`」。这是一条**必要条件而非完整可达性证明**，记录如此，不假装更强。

### 2.5 后继雷劫节点

`EVT-011` 与 `EVT-012` 各被 9 条突破表引用为 `TrialNodes`，即每个大境界推进都带一个雷劫／心魔节点。突破的实际结算属 TASK-16；本任务交付的是节点内容与引用关系。

## 3. 验收结果

| 验收项 | 结果 | 证据 |
|---|---|---|
| 节点 1～3 句 | 通过 | `TestEveryEventNodeHasOneToThreeSentences` |
| 节点 2～4 选择 | 通过 | `TestEveryEventOffersTwoToFourChoices` |
| 危险提前提示 | 通过 | `TestEveryWarnedRiskIsPreviewed`（预览文本非空且概率 1..100） |
| 十二个设计节点齐备 | 通过 | `TestEveryDesignNodeIsShipped` |
| 场景连通、邻接对称、无不可达 | 通过 | `TestTravelGraphIsConnectedAndSymmetric` |
| 所有 NPC 年龄明确 | 通过 | `TestEveryNPCHasAnExplicitAge`、`TestNPCWithoutAnExplicitAgeIsRejected`、`TestNPCOlderThanItsLifespanIsRejected` |
| NPC 来源标记与成年 | 通过 | `TestEveryNPCIsAdultAndItsSourceIsMarked` |
| NPC 所在地与台词可解析 | 通过 | `TestEveryNPCHomeAndDialogueResolves`、`TestEveryDialogueBelongsToADeclaredNPC` |
| 主线能成功/失败/放弃 | 通过 | `TestBothEarlyGoalsCanSucceedFailAndBeAbandoned`、`TestBothEarlyGoalsAreDeclared` |
| 两条主线散修可达 | 通过 | `TestBothEarlyGoalsAreAdvanceableAsARogue` |
| 后继雷劫节点齐备 | 通过 | `TestEveryMajorBreakthroughHasATrialNode` |
| 其他道途标记未开放 | 通过 | `TestOnlyTheHumanPathIsOpen` |
| NPC 真的进入世界 | 通过 | `TestConfirmingACharacterPopulatesTheNamedCast`、`TestSpawningTheCastIsIdempotent`、`TestTheCastAgesWithTheWorldMonth` |
| NPC 可被 `npc_available` 判定 | 通过 | `TestASpawnedNPCBecomesAvailableOnceMet`、`TestADeadNPCIsNotAvailable` |
| 移动真的改变地点 | 通过 | `TestTravelMovesBetweenNeighbours`、`TestTravelRefusesANonNeighbour`、`TestTravelToTheCurrentLocationIsRefused`、`TestTravelDrawsNoDomainDie` |
| 事件按场景门控 | 通过 | `TestEventsAreGatedByScene`、`TestAnEventWithNoSceneFiresAnywhere`（对照组） |
| 摘要覆盖场景状态 | 通过 | `TestDigestCoversTheScenarioState`（含控制组） |

`go test ./...` 全绿，`go vet ./...` 通过，`go build ./...` 通过，`go run ./tools/task11-audit` 审计通过。

## 4. 反证清单（18/18 CAUGHT）

工具：`python3 tools/task11-counterproof/counterproof.py --all`。

| # | 变异 | 回退了什么性质 | 结果 |
|---:|---|---|---|
| 1 | `npc-age-dropped` | NPC 年龄明确 | CAUGHT 2/2 |
| 2 | `npc-spawn-not-called` | 创角时生成具名 NPC | CAUGHT 1/1 |
| 3 | `npc-spawn-not-idempotent` | 重复生成不覆盖既有 NPC | CAUGHT 1/1 |
| 4 | `npc-age-not-carried` | 年龄写入世界状态 | CAUGHT 1/1 |
| 5 | `npc-age-not-validated` | 年龄校验 | CAUGHT 1/1 |
| 6 | `travel-not-implemented` | 移动改变地点 | CAUGHT 1/1 |
| 7 | `travel-accepts-any-location` | 只允许邻接地点 | CAUGHT 1/1 |
| 8 | `scene-gate-removed` | 事件按场景门控 | CAUGHT 1/1 |
| 9 | `node-text-dropped` | 节点正文存在 | CAUGHT 2/2 |
| 10 | `node-text-too-long` | 节点正文 1～3 句 | CAUGHT 2/2 |
| 11 | `goal-abandon-unreachable` | 放弃分支可达 | CAUGHT 2/2 |
| 12 | `goal-success-sect-gated` | 成功分支散修可达 | CAUGHT 2/2 |
| 13 | `travel-graph-asymmetric` | 邻接对称 | CAUGHT 2/2 |
| 14 | `trial-node-dropped` | 后继雷劫节点 | CAUGHT 1/1 |
| 15 | `earthly-path-opened` | 只开放人道 | CAUGHT 2/2 |
| 16 | `condition-operator-always-required` | 只答是非的条件不要求运算符 | CAUGHT 1/1 |
| 17 | `digest-ignores-npcs` | 摘要覆盖 NPC | CAUGHT 1/1 |
| 18 | `digest-ignores-current-location` | 摘要覆盖当前位置 | CAUGHT 1/1 |

### 反证自身暴露的问题（首轮 2 项 NOT CAUGHT，已修正）

1. `npc-age-not-validated` → **PASS**：变异回退了校验器的年龄规则，但当时的测试只用了**带年龄的夹具**，所以没有任何测试观察这条规则。属**测试不敏感**，已补 `TestNPCWithoutAnExplicitAgeIsRejected` 与 `TestNPCOlderThanItsLifespanIsRejected`。
2. `digest-ignores-current-location` → **PASS**：摘要确实覆盖了当前位置，但没有任何测试观察它。属**测试不敏感**，已补 `TestDigestCoversTheScenarioState`。

两次都是「观测量没落在会变的那一支」的同一类问题，与 TASK-10 记录里的教训一致。

## 5. 本轮修掉的真实缺陷

1. **`npc_available` 这类条件根本无法书写。** 条件种类被塞进「要么比较数字、要么比较文本」的二分法里，于是 `npc_available`、`consumed_event`、`skill_known` 这些**只靠 Key 答是非**的种类被要求同时提供运算符与文本操作数——任何用到它们的内容都会校验失败。修正为三个正交谓词：`ValueBearing`（比较数字）、`UsesTextOperand`（比较文本）、`NeedsOperator`（是否比较）。顺带修正 `sect_member` / `has_debt` 被误判为非数值（它们比较 `Value` 的 0/1）。
   这条是 TASK-10 的 DSL 缺陷，被 TASK-11 的 NPC 测试首次触发——**它此前从未被任何内容或测试走到，因为世界里的 NPC 一直是空的**。
2. **`TestCanonicalStringCoversCultivationState` 要求的 schema 提升。** 该特征化测试明确写着「扩展摘要覆盖必须与 schema 版本提升成对」。本任务把 NPC 状态、当前位置与已访问地点纳入摘要，因此 `SchemaVersion` 3→4。测试已按它自己的指引翻转为「摘要必须覆盖 NPC」，并重写了注释以记录当前仍未覆盖的字段。

## 6. 版本与兼容性

- `SchemaVersion 3 → 4`：状态**形状**未变，但摘要的规范形式变了，会让版本 3 写出的每一个摘要失效。这是一次必须成对的改动，由上面那条特征化测试要求。
- `RulesVersion 3 → 4`：创角确认现在还会生成具名 NPC；NPC 从此存在、随世界月增龄、可满足 `npc_available`。这是行为变化而非数据变化。
- `ContentVersion 3 → 4`：新增 `NPCDefinition.InitialAgeYears`、`EventDefinition.TextZH`、`Catalogue.EarlyGoals`，以及 EVT-010/011 的分支选项。

**仍未加迁移适配器**：与 TASK-08/10 的处置一致，M1 尚无已发行的玩家存档；跨版本升级验收留到存档入口接线后独立进行。

## 7. 已知边界（诚实记录，勿误认为已覆盖）

1. **离线模板指的是内容文本，不是渲染管线。** 节点正文、选项文本、风险预览都存放在目录里，M1 因此不需要任何模型或网络就能说话。但把这些文本画到终端上是 TASK-17 的集成工作；`internal/session` 目前只渲染短循环的主屏。
2. **委托与对话尚未接通。** 四个委托（`quest_safe_chore` 等）与四条对话已在目录中并校验通过，但 `ACCEPT_QUEST` / `CLAIM_REWARD` / `JOIN_SECT` 仍是零月空动作，对话没有触发入口——分别属 TASK-14 与 TASK-20。本任务交付的是它们的**内容与引用完整性**。
3. **突破的雷劫节点只建立引用关系。** 九条大境界推进都指向 EVT-011/EVT-012，但节点的实际结算（成功率、材料、后果）属 TASK-16。
4. **散修可达性只做了必要条件检查。** 校验的是「成功分支所在选项与事件不要求入宗」；没有追踪物品来源，因此「筑基丹只能从宗门获得」这类间接门槛不会被发现。要断言更强需要物品溯源，本任务不做。
5. **`EarlyGoals` 不是任务系统。** 它是一个声明，加三个由选项设置的世界 flag。没有目标进度、没有失败后果、没有界面。这样做是为了让「三条分支都存在」成为可机械检查的事实，而不是为了让 M1 拥有一套任务机制。
6. **NPC 日程与目标仍为空。** `NPC.Schedule` 与 `NPC.Goals` 已存在但无内容，属 TASK-23。
7. Windows x64 是当前已验证环境；macOS/Linux 仍不声明支持。

## 8. 验证命令

```powershell
go build ./...
go vet ./...
go test ./...
go run ./tools/task11-audit
python3 tools/task11-counterproof/counterproof.py --check-coverage
python3 tools/task11-counterproof/counterproof.py --record-baseline
python3 tools/task11-counterproof/counterproof.py --all
```

2026-09-24 执行结果：全部 Go 测试通过，`go vet ./...` 通过，内容审计通过，反证 **18/18 CAUGHT**。
