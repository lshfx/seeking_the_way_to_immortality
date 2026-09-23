# TASK-04 状态契约与版本化内容验证记录

- 更新日期：2026-09-23
- **状态：已完成 ✅**（`go build ./...`、`go vet ./...`、`go test ./...` 全部干净；engine 223 项测试通过）
- 依赖：TASK-02、TASK-03（均已完成）

**目的**：冻结游戏的状态契约、命令契约、展示契约与版本化内容，使后续 TASK-05（命令管线）与 TASK-06（存档）可以只面对已定义、已校验的类型。

**边界声明**：本任务**不含任何游戏逻辑**。状态转移、时钟推进、随机抽取属 TASK-05；存档读写属 TASK-06。本任务交付的是类型、校验器与配置数据。

---

## 1. 交付清单

| 文件 | 职责 |
| --- | --- |
| `internal/engine/contract.go` | 枚举、版本常量、定点标度、`ConfigValue`、`Provenance` |
| `internal/engine/state.go` | 持久化状态：`GameState` 及全部子结构 |
| `internal/engine/command.go` | 未决状态判别联合、`Command`/`Payload`、幂等表、`CommandResult` |
| `internal/engine/content.go` | 内容定义类型与 `Catalogue` |
| `internal/engine/save.go` | `SaveEnvelope`、规范串、FNV-1a 完整性摘要 |
| `internal/engine/validate.go` | **全量**校验器（非快速失败） |
| `internal/panel/model.go` | 展示协议（重写；只呈现，不计算） |
| `internal/content/catalogue.go` | 装运 M1 配置：境界/出身/灵根/体质/天赋/物品/功法/技能 |
| `internal/content/cast.go` | NPC、地点、宗门、对话 |
| `internal/content/m1.go` | 12 个事件、突破链、委托 |
| `internal/content/load.go` | `LoadM1`/`MustLoadM1`、`Index` |
| `scripts/check-gofmt.sh` | CRLF 感知的 gofmt 校验脚本 |

---

## 2. 关键设计决策

### 2.1 校验器为全量而非快速失败

`ValidateCatalogue` / `ValidateState` / `ValidateSerializeReady` 一次返回**全部**问题（`ValidationReport.Errors`），而不是遇到第一条就返回。理由：策划拿到配置时，一次看到 14 条错误远好过改一条跑一次、来回 14 轮。

`ValidationReport` 提供 `OK()` / `Error()` / `Codes()`（去重排序）/ `Has(code)` / `CountOf(code)`，使测试可以断言"恰好出现这类错误"，而不是仅断言"有错"。这直接支撑了"每条拒绝规则一个破坏性 fixture"。

### 2.2 十境完整，但开放与否是独立断言

十个境界全部配置（含易漏的**结晶**与**具灵**，历史上被跳过）。但 `M1Paths` **只含人道**。

**"配置了高境界"不等于"开放了该境界"**——后者由 `validateM1Paths` 单独把关。两者分离，避免"内容表里写了"被误读为"玩家可达"。

### 2.3 原稿值与试算值必须可区分

```go
type ConfigValue struct {
    Provenance Provenance  // manuscript | trial | pending | design_note
    Value      int
    Note       string
}
```

- `manuscript`（原稿）与 `design_note`（设计注）视为**已批准**。
- `trial`（试算）与 `pending`（待定）**必须带说明**，且**不算批准**。
- 校验器对未批准参数报 `UNAPPROVED_PARAM`，使"候选文案/候选参数"无法冒充已定稿数据。

这正是方案 v0.3 补充要求的落地：**第 12～14 节候选文案及参数必须转成校验通过的配置后才能视为实现。**

### 2.4 展示协议不计算领域数值

`internal/panel/model.go` 被重写为**纯呈现契约**：`VitalsView.RatioPercent`、`ProgressView.Full`/`ProjectedOverflow`、`EventChoiceView.CostText`/`RiskText` 等**全部由引擎预先算好**，面板只负责摆放。

面板拿不到规则，就无法在渲染函数里偷偷抽事件或推进世界——这是 TASK-05"不在渲染函数里抽事件"约束的结构性保证，而不是靠约定。

### 2.5 规范串是显式列举，不是反射

`SaveEnvelope.CanonicalString()` 逐字段手写。**新增字段不会自动进入摘要**——否则一次无害的结构体改动会让所有既有存档突然校验失败。进入摘要必须是刻意编辑，并配合版本号提升。

### 2.6 `LogTail` 刻意排除在摘要之外

日志尾部是纯展示、不影响重放。若折入摘要，任何日志裁剪都会导致存档失效。已用反证确认这一边界（见 §4）。

---

## 3. 零依赖与引擎边界

TASK-03 确立的硬约束在 TASK-04 保持不变：

- **未引入任何第三方依赖。**
- **`internal/engine` 未导入** `hash`、`strconv`、`sort`、`net`、`os`、`time`、`math/rand`、`crypto/rand`。
  - FNV-1a 内联实现（含偏移基 `14695981039346656037`、质数 `1099511628211`）。
  - 十进制渲染（`appendIntBytes`/`appendUintBytes`）手写，`math.MinInt64` 通过 `uint64(-(v+1))+1` 避免溢出。
  - 键排序用插入排序而非 `sort`。
- `internal/engine/boundary_test.go` 通过 `go/build.ImportDir` 检查**真实导入图（含测试导入）**，单独 `go test ./...` 即可发现违规。

---

## 4. 反证记录（回退旧实现 → 测试必须失败）

本项目纪律：**每个修复都必须有"回退后测试失败"的反证。** 本任务对 `save.go` 完整性机制执行了三次。

### 反证 ①：`VerifyIntegrity` 恒真

把 `return want == s.Integrity.Digest` 改为 `return true`。

**结果：42 个子用例失败**（`TestTamperingEveryCanonicalFieldIsDetected` 的 39 个字段用例 + `TestExtraResourceEntryIsDetected` 等）。防篡改覆盖面真实有效。

### 反证 ②：移除键排序

`sortedKeys` 去掉插入排序，使其返回 Go 的随机 map 迭代顺序。

**结果：确定性测试失败**，且 `TestComputeThenVerifyRoundTrips`（基线）也失败——同一文档两次计算摘要不同。这正是映射迭代顺序进入摘要后的典型症状：**间歇性校验失败，看起来像随机损坏**。排序是必需的，不是优化。

### 反证 ③：移除算法标识校验 → **暴露一处空测试**

去掉 `if s.Integrity.Algorithm != IntegrityAlgorithmFP { return false }`。

**结果：测试全部通过——测试没有失败。** 这是一个**空测试（vacuous test）**。

**根因**：`integrity.algorithm` 本身被折入了规范串，所以改写算法名会**同时改变摘要**。原 `TestVerifyRejectsUnknownAlgorithm` 因此因"摘要不匹配"而非因"算法标识不符"而拒绝——它测的不是它声称要测的守卫。

**修复**：重写该用例——先改名，**改名之后重新计算摘要使其自洽**，并显式断言摘要确实匹配（测试自身的前提校验），此时唯一可拒绝的理由只剩算法标识本身。反证复跑确认**现在会失败**：

```
--- FAIL: TestVerifyRejectsUnknownAlgorithm
    save_test.go:135: an envelope naming an unknown algorithm must not verify
                      even when its digest is self-consistent
```

**这是本任务反证发现的第二类静默通过缺陷**（此前已有 `safe_text` 转义载荷泄漏、`_boxed` 截断后宽度断言仍过、`verify-release.ps1` 从未执行断言等同类记录）。

### 附带修正：`LogTail` 测试从"只记录"改为"真断言"

原测试在发现 `LogTail` 进入摘要时只调用 `t.Log`，永不失败。已改为 `t.Fatal`，并加了"两个仅 `LogTail` 不同的信封必须摘要相同"的正向断言。反证（把 `LogTail` 折入规范串）确认现在会失败：

```
--- FAIL: TestLogTailIsOutsideTheDigest
    save_test.go:240: LogTail must remain outside the digest; folding it in
                      invalidates every existing save
```

---

## 5. 运行期发现并修复的问题

| # | 问题 | 性质 | 处理 |
| --- | --- | --- | --- |
| 1 | 物品价格从未做来源检查 | **真实校验器缺口** | 新增 `validateZeroAwareConfigValue`——价格允许为 0，但仍必须带来源标记 |
| 2 | 测试期望 `DANGLING_REFERENCE`，实际报 `BROKEN_EVENT_CHAIN` | 测试期望写错 | 修正期望，并把两类情形拆成各自断言精确码的独立用例 |
| 3 | 测试期望 `UNKNOWN_ENUM_VALUE`，实际报 `UNKNOWN_GRADE` | 测试期望写错 | 同上 |
| 4 | `state.go` 与 `content.go` 同时声明 `Condition` | 编译错误 | 内容层门控类型改名 `Precondition`（玩家状态仍为 `Condition`） |
| 5 | `save_test.go` 用了不存在的字段/常量名（`Player.Name`、`ResourceSpiritStone`） | 我凭记忆猜名 | 实读源码修正为 `Identity`、`ResSpiritStones`、`ResContribution`、`OriginCommoner` |
| 6 | 六个文件 gofmt 对齐不一致 | 真实格式问题 | 经 CRLF 感知脚本格式化，现无告警 |

**关于 #5**：这是"凭记忆猜标识符"的反面教材。**正确做法是先读定义再写测试**，不应依赖对自建 API 的记忆。

---

## 6. 复现命令

```bash
# 全量验证
go build ./... && go vet ./... && go test ./...

# 仅引擎与内容
go test ./internal/engine/ ./internal/content/ -v

# 引擎边界（不得导入终端/文件系统/时钟/网络）
go test ./internal/engine/ -run TestEngineImportsStayWithinTheDomainBoundary -v

# 格式（CRLF 感知，勿直接用 gofmt -l）
bash scripts/check-gofmt.sh
```

**注意**：本仓库使用 CRLF。直接跑 `gofmt -l` 会把**每个** Go 文件都报为未格式化，这是行尾差异而非真实问题。必须用 `scripts/check-gofmt.sh`。

---

## 7. 当前测试规模

| 包 | 测试数 | 状态 |
| --- | --- | --- |
| `internal/engine` | 223 | ok |
| `internal/content` | 全套 | ok |
| `internal/cli` | 7 | ok |
| `internal/panel` | — | 无测试文件 |
| `internal/session` | — | 无测试文件 |
| `internal/storage` | — | 无测试文件（TASK-06） |
| `internal/tui` | — | 无测试文件 |

---

## 8. 未完成 / 待后续任务

- **未签名**：产物仍未做代码签名。
- **未测平台**：macOS/Linux 未测试，**不声明支持**。
- **TASK-05 起点**：状态转移、耗时矩阵执行、月末顺序、确定性随机、actionId 去重、revision 校验、确认票据、`DeltaLedger` 结算。
- **TASK-06 起点**：本任务定义了 `SaveEnvelope` 与完整性摘要，但**存档的磁盘读写、原子替换、多存档槽位均未实现**；`IntegrityBlock.Algorithm` 字段已为将来更换更强摘要（在存储边界）预留。
- **`Condition` 命名**：玩家状态与内容门控分别叫 `Condition` 与 `Precondition`。此不对称是刻意的（`Precondition` 只在内容定义中出现），但如果将来有人混淆，考虑统一为更明确的名字。
