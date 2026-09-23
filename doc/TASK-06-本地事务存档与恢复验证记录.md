# TASK-06 本地事务存档与恢复验证记录

- 更新日期：2026-09-23
- **状态：阶段一交付完成 ✅**（原子替换、数据目录、单写者锁、快照存储与检查点保留、损坏回退与跨版本迁移、导入导出、故障注入，全部已交付并验证）
- 依赖：TASK-05（已完成）

**目的**：把存档做成**崩溃安全、可恢复、不丢玩家进度**的本地事务，而不是"写个文件"。

**边界声明**：本任务不做云同步、不做加密、不承诺断电安全（见 ADR-003 §4.3，Windows 无法对目录 fsync）。承诺的是：**任意时刻崩溃，磁盘上只存在完整状态**。

---

## 1. 交付清单

| 文件 | 职责 |
| --- | --- |
| `doc/decisions/ADR-003-local-persistence.md` | 冻结存储方案：版本化原子 JSON 快照，**非 SQLite** |
| `internal/storage/atomic.go` | 临时文件 + 原子替换、刷盘策略、故障注入钩子 |
| `internal/storage/layout.go` | 数据根目录解析、目录布局、路径净化、修订号命名 |
| `internal/storage/localappdata_{windows,other}.go` | 平台数据根目录 |
| `internal/storage/lock.go` | 单写者锁：获取、四态判定、陈旧回收、安全释放 |
| `internal/storage/process.go` | 进程存活与身份判定 |
| `internal/storage/process_windows.go` | Windows `GetProcessTimes` 读取进程启动时刻 |
| `internal/storage/process_other.go` | 非 Windows 开发用回退（**非支持声明**） |
| `internal/storage/snapshot.go` | 信封编解码、分层校验、批量写入、归档保留 |
| `internal/storage/snapshotstore.go` | 快照存储：载入回退、修订号守卫、检查点与保留 |
| `internal/storage/stamp.go` | 时间戳与修订号后缀（可注入时钟，便于测试） |
| `internal/storage/migrate.go` | 显式版本迁移链：规划、应用、拒绝与保留 |
| `internal/storage/transfer.go` | JSON 导入导出 |
| `internal/wiring/version_test.go` | 跨包版本常量一致性守卫 |

测试：`atomic_test.go`、`crash_test.go`、`lock_test.go`、`process_test.go`、`snapshotstore_test.go`、`migrate_test.go`、`transfer_test.go`、`faultinject_test.go`。
反证工具：`tools/task06-counterproof/counterproof.py`（39 个变异 + 两项运行前守卫）。

---

## 2. 为什么不是 SQLite

ADR-003 记录了完整推理，决定性理由只有一条：**零第三方依赖**（`CGO_ENABLED=0`）。

补充理由（都被记录在 ADR 里，避免日后重新争论）：

- 本设计的一致性单元**本来就是单个文件**：`interactionSeq`、`worldMonth`、`combatRound`、四条随机流全部是值字段，没有跨表事务。SQLite 的跨表事务解决的是本项目不存在的问题。
- "每存档单写者"已经由单写者锁保证，SQLite 的锁语义是重复建设。

**被推翻的假设（重要）**：ADR 初稿曾断言"`json/v1` 对嵌套 map 会转义失败"。**实测否定了这一说法**——三种嵌套 map 全部正确转义，且按字节序稳定。该断言已从 ADR 删除，并留下了"归档到 ADR 的行为断言必须实测"的教训。

---

## 3. 原子替换

### 3.1 时序

```
创建临时文件（同一目录！）→ 写入 → [fsync] → chmod → close → rename → [fsync 目录]
```

临时文件必须在**目标同目录**，否则 rename 可能跨文件系统，退化为复制而非原子替换。

### 3.2 Windows 无法 fsync 目录，且这不是缺陷

`os.Open(dir)` + `Sync()` 在 Windows 返回拒绝访问。这是 ADR-003 §4.3 **预测过**的平台限制。

关键设计决定：**把"平台不提供"与"尝试后失败"区分开**。

原实现把它建模成 error，后果是**每一次保存都会报"失败"**。改为：

```go
func syncDir(dir string) (unsupported bool, err error)
type WriteOutcome struct { Sync SyncStrategy; DirectorySynced bool; DirectorySyncUnsupported bool }
func (w WriteOutcome) Supported() bool
```

`unsupported=true` 表示平台不提供，**写入本身成功**；真正的失败才返回 error。不做这个区分，恢复报告就没法诚实地说"这次写入得到了什么耐久性"。

### 3.3 `SyncNone` 的存在理由

它只为故障注入测试观察"未刷盘窗口"而存在，**绝不允许作为玩家存档的默认值**。这一点写在类型注释里。

---

## 4. 反证 15：原子性测试连续两次自证无效

这是本任务最重要的发现，完整记录如下。

### 4.1 第一版：靠运气采样

第一版 `TestAtomicWriteNeverExposesAPartialTarget` 在 4 MiB 写入进行时循环读目标文件，若读到"既非旧值也非新值"就失败。

**反证结果：NOT CAUGHT。** 把它替换成 `O_TRUNC` 就地写入，测试依然通过。

根因：observer goroutine 从未被调度进那个窗口——`os.File.Write` 落地页缓存是一次很快的 memcpy。**它断言了一个自己无法观测的性质。**

### 4.2 第二版：确定性暂停，但观测量选错了

加 `SetWritePauseHook`，在"写完"与"rename"之间挂住写入，再采样**目标文件**。

**反证结果：仍然是 NOT CAUGHT。**

根因（实验证据：`.task-cache` 下的一次性探针）：

| 实现 | 挂起期间 `stat(目标)` | 挂起期间目标首字节 |
| --- | --- | --- |
| 正确（临时文件） | 3（旧值） | `OLD` |
| 变异（就地截断） | 4 MiB（新值） | `NNNN…` |

**但这是探针直接读的结果。** 在测试进程内通过 `readFileIfExists` 读，两者**都返回旧值 3 字节**——因为 Windows 在句柄打开期间不更新目录项。**观测量在正确与错误实现下相等，采样一万次也不会有差别。**

教训：**当反证报告 NOT CAUGHT 时，先怀疑观测，再怀疑变异。**

### 4.3 第三版：断言结构性差异

真正能区分两种实现的，是**发布前新数据是否位于一个与目标分离的文件中**：

- 临时文件设计：存在独立文件，内容恰好是新值；目标仍是旧值
- 就地写入设计：**没有**这个文件，字节直接进了目标

`SetWritePauseHook` 改为接收 `stagingPath` 参数，测试在挂起瞬间断言：

1. 暂存路径存在、非空、**且不等于目标路径**（这是原子性的本质）
2. 暂存文件与目标同目录（否则 publish 不可能原子）
3. 暂存内容**完整**（长度等于新值）
4. 目标**未被修改**（长度仍为旧值）

第 4 条只比长度不比字节：Windows 上并发打开写入的文件不一定可重读，早先版本比较首字节时对未改动文件得到 `head=""` 而误报。

**反证结果：CAUGHT**，失败信息为
`the write was not staged in a separate file … publishing must not write into the target before it is complete`。

---

## 5. 单写者锁

### 5.1 判定是三分而非布尔

`LockOwner.judge()` 返回四种判定，而不是"是否陈旧"：

| 判定 | 含义 | 调用方动作 |
| --- | --- | --- |
| `verdictFree` | 无锁文件 | 获取 |
| `verdictHeldLive` | 活跃进程持有 | **拒绝**，报 `ErrLocked`（"游戏已在运行"） |
| `verdictStale` | 持有者可证明已消失 | 回收 |
| `verdictUnjudgeable` | 锁无法解读（损坏/未来版本） | **拒绝**，报 `ErrLockRefused` |

**把后两者合并成一个"已锁"错误会误导玩家。** 测试 `TestUnjudgeableLockIsNotReportedAsContention` 就是钉住这个区别：告诉玩家"游戏已在运行"而实际是锁文件损坏，会让他去找一个从未打开的窗口。

### 5.2 PID 复用防护

`LockOwner` 记录进程**启动时刻**（Windows 用 `GetProcessTimes`，FILETIME 转 unix 纳秒）。`processAlive` 返回三元组而非布尔：

- `(true, true, nil)` PID 存在且是同一进程
- `(false, true, nil)` PID 存在但是**别的**进程 → 已复用
- `(false, false, nil)` PID 不存在

三个分支的取舍是**双向**的：把复用 PID 当活跃 → 玩家永远打不开；把活跃进程当复用 → 摧毁正在进行的会话。**不确定时一律返回"活跃"**——拒绝是可恢复的错误。

### 5.3 回收必须原子

陈旧锁的回收若用"先删后建"，中间存在**无锁文件窗口**，第三个进程可在其中创建锁，此后两个进程都认为自己是持有者。

改用 `atomicWriteFile` 原地替换，路径**永不缺失**。

### 5.4 释放必须核对自己的身份

`Release` 读取当前锁文件，只在 PID 与启动时刻都匹配自己时才删除。若期间发生了接管，返回 `ErrLockRefused` 而非删掉别人的锁——否则会引入第三个写者。

---

## 6. 反证工具自身的两个缺陷（均已修复）

### 6.1 构建失败被当成"捕获"

harness 第一版对 7 个变异报告 **7/7 CAUGHT**，纯粹因为包路径少了 `./` 前缀——**每一次运行都构建失败**。

修复：`classify()` 要求同时满足「编译成功」+「至少一个测试运行」+「至少一个测试失败」。

```python
if "build failed" in out or "[setup failed]" in out or "cannot find package" in out:
    return "BUILD", "the package did not build; this proves nothing about the mutation"
```

修复后真实结果是 **4/7**，暴露出三个确实缺失的测试。**我自己用来检测"静默通过"的工具，本身就带着它要检测的缺陷。**

### 6.2 未经保护的文件被永久改动（最危险）

`GUARDED` 只列了 `atomic.go` 与 `layout.go`，但新增的锁变异编辑 `lock.go` 与 `process.go`——**这些改动被应用后从未还原**。

harness 对这个变异报告 CAUGHT（它是对的），却把工作区留在了损坏状态：紧接着的全量测试出现 **6 个虚假失败**（`TestReleaseRemovesOnlyItsOwnLock`、`TestReleaseIsIdempotent`、`TestLocksArePerCharacter`、`TestUnjudgeableLockIsNotReportedAsContention`、`TestProcessAliveTreatsARecycledPidAsGone`），另有 `lock.go` 残留变异导致下次读取 API 时 `IsStale` 不存在。

修复三处：

1. `GUARDED` 补全为**完整清单**，并注明"这是完整清单而非抽样"。
2. 新增 `verify_coverage()`：静态扫描每个变异函数里出现的 `.go` 路径字面量，凡不在 `GUARDED` 中即报错。
3. `main()` 在**触碰任何文件之前**调用它，发现问题直接 `return 2` 拒绝运行。

守卫本身经四项验证：真实表通过、未保护目标被报出、无法识别目标被报出、`main` 确实拒绝执行。

**并增加了运行前后全量哈希比对**，把"变异全部还原"从假设变成每次运行都可检验的断言：

```
sha256sum internal/storage/*.go tools/task06-counterproof/*.py | sort -k2
→ diff 为空 = IDENTICAL: all mutations reverted
```

### 6.3 污染级联：第三次把工作区留脏，也是第一次被工具自己抓住

`verify_coverage()` 挡住了"编辑未保护文件"，但没有挡住**更隐蔽的一类**：变异被留下之后，**下一次运行的 `snapshot()` 会把已损坏的文件当作干净状态备份下来**，随后 `restore()` 把它还原回去——**损坏就此被"祝福"为基线**。完整链条：

1. 一次运行在变异仍被应用时被中断（例如 SIGTERM，或 `patch()` 的断言抛错）
2. 下一次运行调用 `snapshot()`，忠实地把**已变异**的文件备份为"干净"
3. 该次运行 `restore()` 把变异版本写回，损坏转为永久
4. 该变异自己的 `patch()` 随后找不到它的模式（因为模式的不存在**就是**该变异），harness 报出的是"模式不匹配"，而不是"工作区已损坏"

第 4 步尤其有害：错误信息指向的是一处**看似**过时的模式，而真实原因是工作区早已被污染。

修复：新增**基线指纹**机制。

```
python tools/task06-counterproof/counterproof.py --record-baseline   # 只能对着已验证干净的工作区执行
python tools/task06-counterproof/counterproof.py --check-baseline
```

- 运行任何变异之前校验指纹，不匹配即 `return 3` **拒绝运行**
- 运行结束后再校验一次，不匹配即 `return 4` 并报 `RESIDUE`
- `run_one` 在 `patch()` 抛错时也执行 `restore()`（`except BaseException: restore(saved); raise`）
- `main()` 捕获断言错误并报为 `HARNESS BUG`（`return 5`），而不是让一处过时模式掩盖其余全部结论

守卫经自检确认：向 `stamp.go` 追加一行注释后 `--check-baseline` 报出 `does not match the recorded clean state`，且运行变异时以退出码 3 拒绝。

**关于"记录基线"这一步本身**：它不是把脏工作区合法化的手段，而是把"这棵树在开始时是干净的"从口头假设变成可检验断言。因此它必须对着 `go build` 与全量测试都通过的工作区执行。

---

## 7. 反证清单（39 项，全部 CAUGHT）

### 7.1 原子替换与路径（7 项）

| # | 变异 | 目标测试 | 结果 |
| ---: | --- | --- | --- |
| 1 | `nonatomic-write` 就地截断写入 | `TestAtomicWritePublishesOnlyCompleteData` | CAUGHT |
| 2 | `temp-debris` 失败时留下临时文件 | `TestAtomicWriteCleansUpTempAtEveryFailureStage` | CAUGHT（6/7 子测试） |
| 3 | `no-sync` 取消文件刷盘 | `TestAtomicWrite*` | CAUGHT（2/13） |
| 4 | `sync-lies` 谎报刷盘结果 | `TestAtomicWriteReportsHonestDurability` | CAUGHT |
| 5 | `no-traversal-guard` 取消路径净化 | `TestSanitiseComponentRejectsTraversal` | CAUGHT |
| 6 | `unpadded-revision` 版本号不补零 | `TestRevisionNameSortsLexicographicallyInNumericOrder` | CAUGHT |
| 7 | `relative-override` 接受相对数据根 | `TestResolveDataRootRejectsRelativeOverride` | CAUGHT |

### 7.2 单写者锁（7 项）

| # | 变异 | 目标测试 | 结果 |
| ---: | --- | --- | --- |
| 8 | `lock-ignores-liveness` 把活跃锁当陈旧 | `TestAcquireSaveLockRefusesWhileAnotherLiveProcessHoldsIt` | CAUGHT |
| 9 | `lock-ignores-pid-recycling` 只看 PID 是否存在 | `TestProcessAliveTreatsARecycledPidAsGone` | CAUGHT |
| 10 | `lock-releases-anyones-lock` 取消身份核对 | `TestReleaseRemovesOnlyItsOwnLock` | CAUGHT |
| 11 | `lock-deletes-then-creates` 先删后建回收锁 | `TestLockReclaimNeverLeavesAnInstantWithNoLock` | CAUGHT（第 0 轮即发现） |
| 12 | `lock-reports-damage-as-contention` 损坏锁报成争用 | `TestUnjudgeableLockIsNotReportedAsContention` | CAUGHT |
| 13 | `lock-accepts-conflicting-creation` 去掉 `O_EXCL` | `TestAcquireSaveLockSucceedsWhenFree` | CAUGHT |

第 11 项一度是 **NOT CAUGHT**：原变异只替换了实现，而单线程测试无法观察"无锁窗口"。新增 `TestLockReclaimNeverLeavesAnInstantWithNoLock`（观察者只在回收期间采样目标路径）后捕获。该观察者测试本身迭代了三次才写对，三次失败都记录在测试注释里：

1. 连续采样 → 对**正确**实现误报，因为 `Release` 合法地删除了锁
2. 单通道双向握手 → **死锁**（双方都在等对方接收）
3. 基于 `close` 的握手 → 重复 close **panic**

最终用互斥量 + 布尔的简单设计。39 次回收、41,539 次采样、0 次观察到锁缺失。

### 7.3 快照存储（12 项）

| # | 变异 | 目标测试 | 结果 |
| ---: | --- | --- | --- |
| 14 | `store-skips-character-dir` 不建角色目录 | `TestSnapshotStore*` | CAUGHT（12/21） |
| 15 | `store-drops-revision-guard` 允许旧版本覆盖新版本 | `TestSnapshotStoreRefusesAnOlderRevisionOverANewerOne` | CAUGHT |
| 16 | `store-masks-unsupported-as-corrupt` 未来版本报成损坏 | `TestSnapshotStoreRefusesAFutureEnvelopeVersion` | CAUGHT |
| 17 | `store-deletes-rejected-save` 覆盖被拒绝的档 | `TestSnapshotStorePreservesARejectedSave` | CAUGHT |
| 18 | `store-prune-ignores-retention` 不裁检查点 | `TestSnapshotStoreCheckpointRetentionPrunesOldest` | CAUGHT |
| 19 | `store-prune-deletes-unrecognised` 删掉看不懂的文件 | `TestSnapshotStorePruningIgnoresUnrecognisedFiles` | CAUGHT |
| 20 | `store-commits-before-rotating` **先发布后轮转** | `TestCommitRotatesBeforeItOverwrites` | CAUGHT |
| 21 | `store-verify-accepts-tampering` 不校验摘要 | `TestSnapshotStoreRejectsATamperedSave` | CAUGHT |
| 22 | `store-ignores-character-id` 不核对角色 | `TestSnapshotStoreRejectsACharacterMismatch` | CAUGHT |
| 23 | `store-accepts-incomplete-codec` 接受残缺 codec | `TestSnapshotStoreRejectsAnIncompleteCodec` | CAUGHT（5/5） |
| 24 | `store-drops-both-copies-error` 两份都坏报成"没有档" | `TestSnapshotStoreReportsWhenBothCopiesAreUnusable` | CAUGHT |

**第 20 项值得单独记一笔**，因为它连续错了两次，两次错误性质不同：

1. **第一次变异写错了。** 它只是**移除轮转失败的错误处理**，轮转本身照常发生，于是所有功能测试照常通过，报告 NOT CAUGHT——**这个报告是对的**，变异并没有破坏它所声称的性质。（教训：变异必须真的破坏它命名的那条性质。）
2. **第二次变异改对了（真的交换了两次写入的顺序），测试仍报 NOT CAUGHT**——因为"先轮转"是**顺序**性质，在两次写入都成功的提交里无法从最终状态观察。必须在**转折点**观察：让第二次原子写入在其最后阶段失败，再看轮转路径上有什么。

据此新增 `TestCommitRotatesBeforeItOverwrites`：注入 `stepRename` 失败（**在**改名之前触发），于是当前档确定幸存，测试不会把"轮转发生过"与"覆盖没走到"混为一谈。这是本项目第二次遇到"顺序性质无法从终态观察"，与第 4 节的原子性同源。

### 7.4 迁移（8 项）

| # | 变异 | 目标测试 | 结果 |
| ---: | --- | --- | --- |
| 25 | `migration-returns-partial-chain` 缺口时返回半条链 | `TestPlanMigrationRefusesAGapRatherThanStoppingPartWay` | CAUGHT |
| 26 | `migration-accepts-downgrades` 允许降级 | `TestPlanMigrationRefusesDowngrades` | CAUGHT |
| 27 | `migration-allows-version-skipping-steps` 接受跳版步 | `TestPlanMigrationRejectsVersionSkippingSteps` | CAUGHT |
| 28 | `migration-resolves-ambiguous-chain` 歧义链按顺序取一 | `TestPlanMigrationRejectsAnAmbiguousChain` | CAUGHT |
| 29 | `migration-reports-refusal-as-corruption` 拒绝报成损坏 | `TestMigrationErrorsAreDistinguishable` | CAUGHT |
| 30 | `migration-swallows-refusal` 吞掉拒绝 | `TestApplyMigrationLeavesNothingBehindWhenAStepRefuses` | CAUGHT |
| 31 | `migration-skips-version-advance` 不推进版本号 | `TestApplyMigrationAdvancesTheDeclaredVersion` | CAUGHT |
| 32 | `migration-copy-moves-instead-of-copies` 保留变成搬走 | `TestCopySaveUnmigratedKeepsTheOriginal` | CAUGHT |

第 25 项在第 25 个变异跑出来时是**真的实现了缺陷**：`PlanMigration` 出错时返回的是**已收集到的步骤**而不是空计划。测试抓到了："a failed plan returned 1 steps; a caller might apply them"。修复为所有失败路径都返回 `MigrationPlan{From: from}`。

### 7.5 导入导出（7 项）

| # | 变异 | 目标测试 | 结果 |
| ---: | --- | --- | --- |
| 33 | `import-skips-digest-check` 不校验导入摘要 | `TestValidateImportRejectsATruncatedTransfer` | CAUGHT |
| 34 | `import-accepts-any-json` 任何 JSON 都收 | `TestValidateImportRejectsNonJSON` | CAUGHT |
| 35 | `import-writes-before-validating` **校验期间就写盘** | `TestValidateImportTouchesNothingOnDisk` | CAUGHT（2/3） |
| 36 | `export-allows-writing-into-the-data-root` 允许导出到数据根内 | `TestExportSaveRefusesToWriteInsideTheDataRoot` | CAUGHT |
| 37 | `export-reencodes-instead-of-copying` 导出时重新编码 | `TestExportSaveAllowsAPathOutsideTheDataRoot` | CAUGHT |
| 38 | `import-skips-rotation` 导入前不轮转 | `TestImportIntoSaveKeepsTheOutgoingSave` | CAUGHT |
| 39 | `import-reports-kept-copy-failure-as-import-failure` 附加副本失败报成导入失败 | `TestImportIntoSaveReportsAKeptCopyFailureWithoutFailing` | CAUGHT |

**第 35 项同样是"观测错了，不是变异错了"**，而且错了两次，代价是同一个道理的两种形态：

1. **第一次变异写到了数据根之外。** 它把标记写在**导入文件旁边**，而那个文件位于另一个临时目录；测试只指纹数据根本身，因此**正确**地没有看到。这是变异瞄错了证人：校验可以靠近导入文件，但不能靠近存档。
2. **第二次变异改对了位置，测试仍然 NOT CAUGHT**——因为原子写是"临时文件 + 改名"，把**相同字节**写回去后内容指纹完全看不出变化。修复是让指纹**包含修改时间与大小**，于是"文件被重写过"这件事第一次变得可观测。

这是本项目**第三次**遇到同一类错误（前两次见第 4 节与反证 15b）。已升级为过程纪律第 2 条。

---

## 7bis. 版本常量一致性（跨包守卫）

`internal/storage.CurrentEnvelopeVersion` 与 `internal/engine.EnvelopeVersion` 是**两个独立声明的同一个数**。之所以不合并：ADR-002 禁止 `internal/engine` 依赖 I/O 包，而 `internal/storage` 是兄弟包，导入 engine 会构成环。代价是一个重复的数字，而重复的数字会漂移。

`snapshot.go` 的注释里承诺"有一个测试会抓住漂移"。本任务补上了它：`internal/wiring/version_test.go`——这是全树**唯一**允许同时导入两侧的包，放在 `internal/storage` 内就等于让 storage 导入 engine，正是设计所禁止的环。

漂移的后果在两个方向都是静默且严重的：

- storage 高于 engine：storage 会接受并写出 engine 读不懂的文档，游戏把它当成空状态载入
- storage 低于 engine：storage 把自己 engine 写的档当成"来自更新的版本"拒绝，每个玩家都被告知去升级一个已经是最新的构建

守卫经反证确认：把 `CurrentEnvelopeVersion` 改成 2 后 `TestEnvelopeVersionsAgree` 失败并给出双方数值，改回后通过。

---

## 8. 当前测试规模

| 范围 | 数量 |
| --- | --- |
| `internal/storage` 顶层测试 | 97 |
| `internal/storage` + `internal/wiring` 含子测试 | 137 全部通过，0 失败 |
| 反证变异 | 39（全部 CAUGHT，无 BUILD 失败） |
| 跨包一致性守卫 | 2（`internal/wiring`） |

按文件分布：`atomic_test.go` 22、`snapshotstore_test.go` 22、`migrate_test.go` 13、`transfer_test.go` 13、`lock_test.go` 12、`process_test.go` 6、`faultinject_test.go` 5、`crash_test.go` 4。

全仓库 `go build ./...`、`go vet ./...`、`go test ./...` 干净；engine 边界测试仍通过（`internal/storage` 与 `internal/wiring` 都是独立包，engine 保持纯净）。

反证工具现带两项**运行前守卫**与一项**运行后复查**，三者都是拒绝而非警告：

| 守卫 | 检查 | 不通过时 |
| --- | --- | --- |
| `verify_coverage()` | 每个变异的目标文件都在 `GUARDED` 完整清单里 | `return 2` |
| `check_baseline()`（前） | 工作区与记录的干净基线一致 | `return 3` |
| `check_baseline()`（后） | 运行结束后工作区仍与基线一致 | `return 4`（报 `RESIDUE`） |

命令：`--record-baseline`（只能对着已验证干净的工作区执行）、`--check-baseline`、`--check-coverage`、`--list`、`--all`、`<mutation-name>`。退出码 `5` 表示某变异的模式已过时（报 `HARNESS BUG`），与"测试没抓到"是不同的问题。

---

## 9. 过程纪律（本任务确立）

1. **构建失败不是测试失败。** 反证工具必须区分，否则会得到百分比很漂亮的假报告。
2. **反证报告 NOT CAUGHT 时，先怀疑观测。** 观测量在正确与错误实现下相等时，采样次数无关紧要。本任务在三个不同位置各踩一次：原子性测试（目标档内容）、轮转顺序（终态无法观察顺序）、导入校验（相同字节写回看不出变化）。
3. **变异必须真的破坏它命名的那条性质。** 只移除错误处理的变异没有破坏顺序保证，因此 NOT CAUGHT 是正确的报告；此时要修的是**变异**。
4. **顺序性质必须在转折点观察。** 注入一次失败，看那个失败点之前必然已经完成的部分。
5. **反证工具修改的每个文件都必须可还原**，且这一点要在**运行前**静态校验。
6. **工作区必须在运行前证明干净。** 否则 `snapshot()` 会把上次留下的损坏备份为"干净"，`restore()` 再把它写回去——损坏被祝福成基线。用基线指纹，且运行后复查。
7. **运行反证期间不得编辑源码。** 本次有两次全量运行因此报废（每个变异都报 BUILD 失败）。这不是 harness 的缺陷——它对构建失败不计数是**正确**的——而是操作纪律。
8. **归档到 ADR 的行为断言必须实测。** 已有一条被实测推翻（`json/v1` 嵌套 map）。
9. **平台限制要与真实失败分离建模。** Windows 无法 fsync 目录是预期行为，不是每次保存都失败；而 `sync-dir` 阶段失败属于 **Partial**（数据已发布，只是持久性保证更弱），不是失败，把它报成失败会让玩家在进度已经落盘后看到"保存失败"。
10. **在 Windows 上与并发读写有关的测试必须容许共享冲突。** 被拒绝比读到半个文件是**更强**的保证。
