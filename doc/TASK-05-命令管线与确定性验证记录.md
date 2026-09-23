# TASK-05 命令管线、时钟与确定性随机验证记录

- 更新日期：2026-09-23
- **状态：已完成 ✅**（`go build ./...`、`go vet ./...`、`bash scripts/check-gofmt.sh`、`go test ./...` 全部干净；engine 214 顶层 / 368 含子测试通过，0 失败）
- 依赖：TASK-04（已完成）

**目的**：实现命令提交管线、游戏时钟与确定性随机，使「同快照 + 同动作序列 → 同机械状态」成为可执行的保证，而不是一句设计意图。

**边界声明**：本任务**不含内容效果**。委托报酬、事件选项的具体收益、战斗数值属 TASK-07/11/14。本任务交付的是时序契约、确定性协议、幂等与提交流程。

---

## 1. 交付清单

| 文件 | 职责 |
| --- | --- |
| `internal/engine/rng.go` | 确定性随机流：四个独立流、拒绝采样、快照恢复 |
| `internal/engine/clock.go` | 世界月/季节/日期、年龄与寿元、三计数推进 |
| `internal/engine/store.go` | 存储接口、内存存储、深拷贝、请求指纹与幂等表 |
| `internal/engine/pipeline.go` | 命令提交流程（九步）、相位门、确认票据、父行动结算 |

测试（新增 112 个）：

| 文件 | 测试函数 | 覆盖 |
| --- | ---: | --- |
| `rng_test.go` | 23 | 流确定性、四流独立、快照恢复、拒绝采样边界、已知摘要向量 |
| `clock_test.go` | 19 | 纪元、季节分组、日期回卷、年龄截断、寿元求和与闭区间死亡、三计数分离 |
| `pipeline_test.go` | 28 | 九步顺序、相位门、确认票据、幂等去重与冲突、拒绝后状态逐字节不变 |
| `month_test.go` | 20 | 月末六步顺序、`MonthCharged` 幂等、效果衰减与过期、NPC 增龄与死亡 |
| `store_test.go` | 22 | 深拷贝隔离、内存存储语义、请求指纹、幂等表上限与淘汰 |

---

## 2. 提交流程的九步顺序

设计文档 9.2 规定的是**顺序**，不是一组检查。顺序本身就是正确性的一部分：

| 步 | 检查 | 为什么必须在这个位置 |
| ---: | --- | --- |
| 1 | 重复 actionId → 返回既有结果 | 放在最前，否则重发会因 revision 已变而被误报为失败——玩家会看到一个实际成功的点击报错 |
| 2 | kind 合法 | — |
| 3 | revision 匹配 | — |
| 4 | viewToken 匹配 | **查询也要查**：对着旧界面查询会渲染出与当前世界不符的面板 |
| 5 | 相位允许该 kind | — |
| 6 | 确认票据 | **在花费之前**。这是「取消不扣费」成立的原因 |
| 7 | 在**克隆**上执行 | 所有变更发生在副本上；此后任何拒绝都丢弃副本 |
| 8 | 不变量 + 可序列化校验 | 留下不可序列化状态会让存档无法写出，此时拒绝优于存档时才发现 |
| 9 | 提交，然后记录幂等 | **先提交再记录**：只有成功落盘的命令才占用 actionId |

**第 6 步与第 7 步是「取消不扣费」的全部秘密。** 第 7 步之前的一切是纯校验，不得变更状态、不得抽取随机数。

---

## 3. 确定性随机

`splitmix64-counter-v1`。每次抽取是 `(seed, counter)` 的**纯函数**，因此恢复的流能正确从中途继续，而不是重放开头。

### 3.1 为什么不用 `math/rand`

标准库的默认源在不同 Go 版本间不保证稳定，而本项目的承诺是「同存档同序列永远同结果」。`crypto/rand` 按定义不可复现。两者都禁用，也顺带满足了 engine 的边界规则（不得导入 `math/rand`/`crypto/rand`）。

### 3.2 四个独立流

`world` / `combat` / `drop` / `npc` 各自从同一游戏种子**派生**不同子种子。派生而非共用是关键：共用种子会让战斗流与掉落流产生相关值，表现为「掉落表像被操纵」。

已用反证确认两类缺陷都能被捕获（§5 反证 2、3），且这两类是**不同的**缺陷：

- **子种子相同** → 由 `TestStreamsHaveDistinctSubSeeds` 捕获。
- **共用同一游标**（推进一个会影响另一个）→ 由 `TestStreamsAreIndependent` 捕获。

后者不会被前者发现：子种子相同但游标独立时，推进互不干扰。两个测试缺一不可。

### 3.3 拒绝采样——一处诚实声明的局限

`NextUint64N` 用拒绝采样而非朴素取模。**但我必须如实说明：测试无法证明这一点。**

反证时把拒绝采样换成 `s.Next() % n`，**全部测试仍然通过**。原因是 64 位输出空间下取模偏差约为 2⁻⁶⁴ 量级，30000 次试算根本无法分辨。测试确实做了有用的工作（能捕获范围错误、差一、某个取值被严重偏袒），但**拒绝采样的无偏性由构造与阅读代码确立，不由该断言确立**。

这条限制已写进测试注释，避免后人把一次绿色运行当作无偏性的证据。

---

## 4. 时钟：现实时间永不映射游戏时间

`clock.go` 中**没有任何地方读取时钟**。时间是存储计数器的纯函数。

- **开局**：天玄历 387 年一月；1-3 春 / 4-6 夏 / 7-9 秋 / 10-12 冬。
- **年龄与寿元内部统一整数月**；`AgeInYears` **截断而非四舍五入**——215 月是 17 岁不是 18 岁，寿元边界上的差一会让角色早死或晚死一个月。
- **寿元耗尽为闭区间**：恰好到达上限即死亡（960 月上限在第 960 月死）。
- **`AdvanceWorldMonth` 把推进月份与增龄绑在一起**。若调用方能推进月份而不增龄，寿元缺陷会是静默的；若能增龄而不推进，面板会与记录不符。
- **战斗轮不推进世界月、不增龄**：战斗不是时间的流逝。
- **`LifespanMonths` 每次求和而非维护累计值**——账本是唯一来源，奖励无法被重复命令重复计入。

---

## 5. 反证记录（回退 → 测试必须失败）

### 反证 1：`VerifyIntegrity` 恒真（承 TASK-04）
42 个子用例失败。

### 反证 2：所有流共用同一子种子
`TestStreamsHaveDistinctSubSeeds` 失败。**`TestStreamsAreIndependent` 未失败**——两者测的是不同属性（见 §3.2）。

### 反证 3：所有流别名同一游标对象
`TestStreamsAreIndependent` 与 `TestRNGSetSnapshotRoundTrips` 失败。

### 反证 4：朴素取模替换拒绝采样
**测试未失败——且这是预期且如实记录的。** 无法分辨的偏差不可能被统计检验发现（见 §3.3）。

### 反证 5：月份推进但忘记增龄
`TestAdvanceWorldMonthAgesExactlyOnce` 失败。

### 反证 6：战斗轮推进世界月
`TestCombatRoundDoesNotAdvanceWorldOrAge` 失败。

### 反证 7：寿元边界改为开区间（差一）
`TestLifespanExhaustedIsInclusive`、`TestLifespanBonusExtendsTheBoundary` 失败。

### 反证 8：查询被算作状态变更
`TestRepeatedQueryDoesNotAdvanceAnything` 等 4 项失败。

### 反证 9：确认校验恒通过
`TestCancelledConfirmationChargesNothing`、`TestExpiredConfirmationIsRejected`、`TestConfirmationMustMatchKindAndTarget` 失败。

**一次无效的反证（如实记录）：** 首次尝试是**删掉** `if !requiresConfirmation(...)` 的提前返回。结果不是「不再要求确认」，而是**要求所有命令都确认**，导致 8 个无关测试级联失败。这说明该次反证证明的不是我想证明的东西，已重做。

### 反证 10：幂等去重被绕过
`TestDuplicateActionReturnsStoredResultAndChangesNothing`、`TestSameActionIDDifferentBodyIsRejected` 失败。

### 反证 11：`MonthCharged` 守卫失效
`TestSettlementChargesTheMonthExactlyOnce` 失败。

### 反证 12：保存失败被忽略
`TestSaveFailureIsNotReportedAsSuccess`、`TestSaveFailureDoesNotConsumeTheActionID` 失败。

### 反证 13：过期效果不从列表中移除
`TestTimedEffectsDecrementOnMonthEnd` 失败。

### 反证 14：月末顺序——**测试未失败，暴露一处答案不敏感的空测试**

这是本次最有价值的发现。设计 13.2 第 4 步规定：即时效果（如突破带来的寿元提升）必须**先于**增龄应用，否则刚抬高上限的角色会被旧上限判定死亡。

原 `TestMonthEndOrderAppliesLifespanBeforeAging` 只断言**最终结果**（角色活着）。把即时效果挪到增龄**之后**，测试**仍然通过**——因为死亡检查在两者之后只执行一次，抬高后的上限无论顺序如何都能救活角色。

**修复**：新增 `TestMonthEndOrderIsObservableByTheEffectItself`，让即时效果**记录它被调用时看到的计数器值**。顺序正确时它看到旧值（10 月 / 500 月龄），顺序错误时看到新值。反证复跑确认现在会失败：

```
--- FAIL: TestMonthEndOrderIsObservableByTheEffectItself
    month_test.go:65: the immediate effect saw world month 11, want 10
                      (it must run before the month advances)
```

**一般教训**：当一个不变量关乎**顺序**时，断言最终状态是不够的——最终状态可能对所有顺序都相同。必须让被测对象**观察**它所处的位置。

### 反证 15：过期效果塌缩为 nil 的保持逻辑
`decrementEffects` 的 `len(effects) == 0 { return effects }` 改为 `return []TimedEffect{}`，
`TestDecrementEffectsHandlesNilAndEmpty` 失败：

```
--- FAIL: TestDecrementEffectsHandlesNilAndEmpty (0.00s)
    month_test.go:757: decrementEffects(nil) = []engine.TimedEffect{}, want nil
```

### 反证 16：把 coverage 缺口真的补上

`TestCanonicalStringDoesNotCoverConditionEffects` 是**特征化测试**（characterisation
test）：它把当前的行为缺陷固化成可执行的事实，而不是写成散文。因此它的反证方向与
其他反证相反——**修好缺陷必须让它失败**。

在 `CanonicalString` 中临时加入对 `Condition.Effects` 的遍历后：

```
--- FAIL: TestCanonicalStringDoesNotCoverConditionEffects (0.00s)
    month_test.go:735: CanonicalString now covers Condition.Effects. That is an
    improvement, but it invalidates every digest written by an earlier build:
    bump the schema version, invert this assertion, and update the TASK-04 record
```

这条反证证明该测试**真的会在缺口闭合时发出信号**，而不是一条永远为真的注释。
（`README` 式的说明会随时间腐烂；特征化测试会主动报警。）

### 反证 17（无效，如实记录）：`nil` 切片保持

首次尝试把 `return effects` 改掉时，补丁脚本报 `AssertionError: 0`——`old` 字符串用了
LF 而行尾是 CRLF，`str.replace` 匹配不到。改用 `nl=''` 读入 + 按 `\r\n` 构造 `old` 后
成功。**这是同一类陷阱第二次出现**（前一次在 `month_test.go`），已提炼为流程规则（§9）。

---

## 6. 运行期发现并修复的问题

| # | 问题 | 性质 | 处理 |
| --- | --- | --- | --- |
| 1 | `TestConsumedEventIsRecordedOnce` 的重发改了 revision 与 viewToken，被判为 `ACTION_ID_CONFLICT` | **测试写错，且暴露一个真实的设计边界** | 重发必须逐字节相同。改成真正的重发，并新增 `TestResendThatChangesRevisionIsNotTreatedAsADuplicate` 显式记录这一边界 |
| 2 | 我又凭记忆猜了字段名（`Player.Name`、`NPC.Statuses`、`Condition.Statuses`、`BreakthroughDefinition.ID`、`EndLifespan`、`ResourceSpiritStone`） | 反复犯的错 | 逐个实读源码修正。**第二次犯同样的错，说明这是流程问题不是疏忽**——已在 §8 立规 |
| 3 | 11 个文件行尾为 LF，仓库为 CRLF | 工具写入与仓库约定不一致 | 统一为 CRLF；经 `git diff --numstat` 确认**无内容差异**（`core.autocrlf=true`） |
| 4 | 3 个测试文件 gofmt 对齐不一致 | 手写对齐 | 格式化，现无告警 |
| 5 | `TestSaveFailureDoesNotConsumeTheActionID` 最初可能掩盖「失败也占用 id」 | 设计选择 | 明确为：**只有成功提交才记录幂等**，失败后可同 id 重试 |

---

## 7. 复现命令

```bash
go build ./... && go vet ./... && go test ./...

# 确定性随机
go test ./internal/engine/ -run 'TestStream|TestResum|TestChance|TestIntRange' -v

# 时钟
go test ./internal/engine/ -run 'TestEpoch|TestDate|TestSeason|TestLifespan|TestAdvance' -v

# 提交流程与验收条款
go test ./internal/engine/ -run 'TestSameSnapshot|TestRepeatedQuery|TestDuplicateAction|TestCancelled|TestStale' -v

# 月末顺序（顺序敏感，见 §5 反证 14）
go test ./internal/engine/ -run 'TestMonthEndOrder' -v

# 引擎边界
go test ./internal/engine/ -run TestEngineImportsStayWithinTheDomainBoundary -v

# 格式（CRLF 感知，勿直接用 gofmt -l）
bash scripts/check-gofmt.sh
```

---

## 8. 流程规则（本次新增）

**规则一：写测试前必须先读类型定义。**

本轮两次因凭记忆猜标识符导致编译失败（累计 6 处符号：`Player.Name`、`NPC.Statuses`、
`Condition.Statuses`、`BreakthroughDefinition.ID`、`EndLifespan`、`ResourceSpiritStone`）。
第二次犯同样的错说明这不是疏忽而是流程缺陷：自建的 API 看起来「应该记得」，但记忆
不可靠，代价是整轮编译失败。

**规则二（本次新增）：编辑已存在文件时，补丁脚本必须按文件实际行尾构造匹配串。**

同一个陷阱在本轮出现**两次**（`month_test.go`、`pipeline.go`），两次都表现为
`AssertionError: 0`——Python 的 `str.replace` 静默返回未修改的字符串（`replace` 的
`count` 为 0 不报错，只有我自己加的 `assert` 才发现）。正确做法：

```python
s = io.open(p, encoding='utf-8', newline='').read()   # newline='' → 保留原始 \r\n
nl = '\r\n' if '\r\n' in s else '\n'                  # 探测
# ...
assert s.count(old) == 1, s.count(old)                # 匹配失败必须显式炸掉
s = s.replace(old, new)
if nl == '\r\n':                                       # 写回时还原
    s = s.replace('\r\n', '\n').replace('\n', '\r\n')
io.open(p, 'w', encoding='utf-8', newline='').write(s)
```

教训有两层：**（a）** 必须探测行尾而非假设；**（b）** 必须用 `assert` 把「匹配到 0 处」
从静默失败变成响亮失败——否则会以为改完了，实际什么都没改。

**规则三：检查覆盖率的测试，必须带一个已知为真的控制组。**

`TestCanonicalStringDoesNotCoverConditionEffects` 断言「摘要对某些字段不敏感」。
这类断言有一个隐蔽的失败模式：如果 `digestCovers` 助手本身坏了（永远返回 false），
测试会**因为错误的原因而通过**，缺口即使闭合也不会被发现。

对策：同一个测试里必须同时断言若干**确定被覆盖**的字段（revision、world_month、
age_months、spirit_stones、rng counter）确实被覆盖。这样助手一旦失效，失败信息会读作
「digestCovers 坏了」而不是「缺口闭合了」。

---

## 9. 测试规模

| 包 | 顶层测试 | 含子测试 | 状态 |
| --- | --- | --- | --- |
| `internal/engine` | 214 | 368 | ok |
| `internal/content` | 30 | — | ok |
| `internal/cli` | 8 | — | ok |
| `internal/panel` | — | — | 无测试文件（纯数据契约，由 engine 侧覆盖） |
| `internal/session` | — | — | 无测试文件（TASK-09 起） |
| `internal/storage` | — | — | 无测试文件（TASK-06） |
| `internal/tui` | — | — | 无测试文件（TASK-09/10） |

统计口径说明：`go test -v` 中 `--- PASS:` 有缩进的是子测试。上一版记录里的「362」是
顶层计数，本次修正为 **214 顶层 / 368 含子测试**（期间新增 3 个测试，其余差额来自
统计口径不同——旧数字用的是 `grep -c "--- PASS"` 无锚点版，会把部分输出算进去）。
以本表为准。

命令复现：

```bash
go test ./internal/engine/ -v 2>&1 | grep -cP '^--- PASS'   # 214
go test ./internal/engine/ -v 2>&1 | grep -cP '^ *--- PASS' # 368
```

---

## 10. 未完成 / 待后续任务

- **其他内容效果未实现**：修炼成长现由 TASK-08 接入；委托报酬、事件收益、战斗数值、突破成功率与材料消耗仍由 TASK-11/14 等后续任务填充。`immediateEffect` 钩子是 TASK-11 的接入点，其时序位置已在 TASK-05 固定。
- **磁盘存档未实现**：`Engine` 依赖 `Store` 接口，目前只有 `MemoryStore`。TASK-06 提供文件实现、原子替换与多槽位。
- **未签名**：产物仍未做代码签名。
- **未测平台**：macOS/Linux 未测试，**不声明支持**。
- **`immediateEffect` 钩子只接受 1 个函数**：若 TASK-11 需要多个独立订阅者，需要改成切片。当前单函数形态是刻意的，避免过早引入订阅机制。
- **`CanonicalString` 的覆盖缺口（TASK-08 已部分修复）：** `Condition.Effects`、心境、角色属性、主辅功法和熟练度现在进入规范摘要，并在 TASK-08 将状态/规则/内容版本升至2时一并落地。NPC年龄以及部分 `World`、`Relations`、`Pending` 状态仍未覆盖；它们成为正式玩法状态前须继续评估。

  TASK-05的反证16记录了当时 `Condition.Effects` 未被摘要覆盖的状态。TASK-08更新为 `TestCanonicalStringCoversCultivationState`，验证摘要对修炼所依赖状态的变化敏感。没有发行玩家存档，Schema 1 开发状态没有被承诺可直接导入；正式存档入口接线前仍需决定迁移或拒绝旧档的行为。

- **`tickTimedEffects` 现会推进 NPC 年龄**（此处原记「有一处空转」，现已填实）：它现在对 `World.NPCs` 逐个 `AgeMonths++`，并在超寿时置 `IsAlive = false`。副作用是导致上面那条摘要缺口从「理论问题」变成「可观测问题」——NPC 年龄是真实变化的状态，而摘要看不见它。已由 `TestNPCsAgeWithTheWorldMonth`、`TestNPCsDoNotAgeWithoutASettledMonth`、`TestNPCsDieWhenTheirOwnLifespanRunsOut` 覆盖行为，缺口本身见上一条。

