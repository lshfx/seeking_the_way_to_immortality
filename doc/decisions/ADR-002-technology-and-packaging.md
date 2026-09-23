# ADR 002 正式技术栈与 Windows 便携发行

- 状态：**已采纳；独立 Windows 用户账户验收已完成（2026-09-23，见 §4.4）**
- 日期：2026-09-22（2026-09-23 复验修正，见 4.1）
- 关联任务：TASK-02、TASK-03
- 决策范围：M1 正式语言、进程模型、终端适配方向、工程边界、首个平台产物和存储候选

## 1. 决策

M1 正式实现采用 **Go 1.26**，当前验证工具链为 Go 1.26.5。首个发行目标为 **Windows x64 便携包**，核心产物是 `wendao.exe`；玩家运行时不需要 Go、Python、Node、编译器或包管理器。

当前 `go.mod` 没有第三方模块。构建固定 `CGO_ENABLED=0`、`GOOS=windows`、`GOARCH=amd64`、`-mod=readonly`、`-trimpath`，确保启动不下载组件。后续若确需第三方依赖，必须固定版本、记录许可证并证明便携包仍可离线运行，不能在首次启动时下载。

程序采用单进程、单前台会话、同步命令循环。终端层使用 ANSI 输出与窄的 Windows 控制台适配器，负责尺寸、按键、模式进入和恢复；领域引擎不接触终端。M1不引入全屏TUI框架、后台守护进程或并发游戏时钟。非TTY、控制序列不可用或尺寸不足时进入明确的朴素输出/提示路径。

终端适配先使用 Go 标准库和 Windows 系统调用所需的最小封装。TASK-02探针已经证明交互形态可行，但正式 Go 适配仍由TASK-09实现并做 Windows Terminal、VS Code和conhost实测。如果标准库封装在恢复或输入方面达不到验收，再单独评估小型终端依赖；不得为了界面引入Web运行时。

存储首选候选为**版本化原子 JSON 快照**：状态、随机流、未决节点、账本尾和幂等结果放在一个版本化信封中，以同目录临时文件、刷新、原子替换和上一份有效快照实现。理由是单机单角色规模小、可导出检查、无需数据库运行时。TASK-06仍须通过断写、磁盘满、权限、锁和迁移故障注入后才能正式定案；若单文件信封无法满足一致性或查询需求，再回到 SQLite 方案，不能静默降低恢复语义。

Windows 数据根目录候选为 `%LOCALAPPDATA%\WendaoChangsheng\`，安装目录和当前工作目录不保存玩家进度。具体目录、单写者锁和刷盘语义由TASK-06实现验证。

## 2. 候选比较

试验机器：Windows NT 10.0.26200.0 x64，Intel64 Family 6 Model 154，运行时报告可用内存31.65 GiB；C盘测试时已用221.81 GiB、可用58.64 GiB；PowerShell 7.6.5 ConsoleHost。交互证据来自Codex Windows PowerShell PTY，无独立终端产品名和字体信息。启动时间通过 PowerShell `Measure-Command` 顺序运行20次，仅作为本机量级证据；未扣除PowerShell和安全扫描开销。

| 候选 | 本机条件与最小试验 | 独立发行结果 | 本机启动结果 | 结论 |
|---|---|---|---:|---|
| Go 1.26.5 | 可用；标准库构建 `hello.go` 和正式骨架 | `wendao.exe` 1,681,920字节，CGO关闭，无第三方模块 | 正式产物平均34.63 ms，中位27.18 ms，范围22.63～109.77 ms | 采用；单文件、边界清楚、确定性测试和跨平台构建成本合适 |
| Python 3.13.5 | 脚本平均60.34 ms，中位54.58 ms；PyInstaller/Nuitka均未安装 | 当前只能依赖本机Python；`python.exe`之外仍需约6.1 MB核心DLL及标准库 | 依赖已安装运行时 | 不采用为正式交付；继续作为一次性原型工具 |
| Node 22.17.0 | 脚本平均86.90 ms，中位85.80 ms；`node.exe`约85.2 MB | pkg、nexe、postject均未安装；当前只能依赖Node运行时 | 依赖已安装运行时 | 不采用；项目也不需要Web生态 |
| .NET 10运行时 | 本机仅有Microsoft.NETCore.App 10.0.8，无SDK | 无法在当前开发环境构建或验证自包含产物 | 未测 | 不采用；为此安装新SDK没有胜过现有Go工具链的证据 |
| Rust | rustc/cargo均未安装 | 无法构建 | 未测 | 不采用；为本项目额外引入工具链没有当前收益 |

Python和Node的脚本启动数字不能与Go独立产物直接等价比较，它们使用本机已经安装并可能被系统缓存/信任的运行时。决定主要依据是玩家交付形态、依赖、维护边界和实际独立产物，而非单项启动数字。

输入支持也按“已验证”和“可实现”区分：Python探针已经在当前PowerShell PTY验证按键、分页和正常退出，但无法直接成为无需Python的玩家产物；Go具备ANSI输出与Win32控制台API，正式适配尚未实现，必须沿用TASK-02矩阵在TASK-09复验。Node的raw stdin、.NET Console和Rust终端crate本轮均未做真实输入试验，因此不将其记为兼容优势。维护方面，Go当前一个模块、一个入口、零第三方依赖；其代价是需要维护小型Windows终端适配。其他候选要么需要额外打包工具，要么本机没有编译工具链。

## 3. 工程结构

```text
cmd/wendao/       唯一正式可执行入口
internal/cli/     参数、进程退出码、标准输入输出边界
internal/tui/     PanelModel渲染、输入上下文、分页
internal/panel/   只读展示协议
internal/session/ 命令路由、确认、提交、恢复协调
internal/engine/  纯领域规则；不访问终端、文件、时钟和网络
internal/content/ 版本化静态内容加载与校验
internal/storage/ 本地事务、锁、导入导出和迁移
content/m1/       M1配置和manifest
tests/            单元、回放、集成故障和终端验收
packaging/        便携发行说明
scripts/          测试、构建和独立运行验证
```

TASK-03只建立边界和可构建骨架。`GameState`、`Command`、完整`PanelModel`、内容schema和版本策略从TASK-04加入；存储、终端和玩法模块的说明不得被宣传为已经实现。

## 4. Windows产物证据

执行：

```powershell
.\scripts\test.ps1
.\scripts\build.ps1 -Version 0.0.0-task03
.\scripts\verify-release.ps1
.\scripts\verify-release-account.ps1 -Label "<账户标签>"
```

2026-09-22结果：

- `go test ./...` 与 `go vet ./...` 通过；脚本同时检查engine没有直接导入终端、存储、网络、墙上时钟或系统调用包，并确认当前只有主模块。
- `dist/wendao.exe` 为1,681,920字节；SHA-256为`8d4561e58a9eb08cebbc92ca1a026a177993c5dd8d896cdfdf44018f71df46c6`。
- Go构建元数据显示`GOOS=windows`、`GOARCH=amd64`、`CGO_ENABLED=0`、`trimpath=true`。
- 将产物复制到仅含该exe的隔离目录，并把PATH缩到Windows System32后，`--version`和`--diagnose`均成功；诊断明确`network=disabled`和`game_state=not_implemented`。
- 产物尚未签名。签名不是当前本地开发验证的前提，但若公开分发应评估SmartScreen体验和签名成本。

当前没有创建新的Windows用户账户，因此“不同的干净用户配置文件”仍为未测项；不能把隔离目录和PATH试验写成独立账户验收。TASK-03实现与发行试验完成，关闭任务前补一次独立测试账户记录。macOS和Linux也未测试，不声明支持。

### 4.1 2026-09-23 复验与脚本缺陷修复

复验时发现上一轮记录的结论**有几项不成立**，已修复：

- **`verify-release.ps1` 实际上从未通过。** 脚本用 `& $stagedBinary --version` 后检查 `$LASTEXITCODE`。在部分PowerShell宿主（含本机Windows PowerShell 5.1调用控制台程序）中该变量不会被可靠赋值，于是 `$LASTEXITCODE -ne 0` 恒真，脚本在**任何真实断言之前**就抛出 `--version failed with exit code `（退出码为空），进程以码1退出。上一轮记录的"隔离运行成功"因此缺少脚本证据。
  另外该脚本只检查**源**产物是否存在，未检查staging目录是否真的拿到了产物，可能对一次空运行报通过。
  - 修复：改用 `System.Diagnostics.Process` 获取真实 `ExitCode`；显式断言staging产物存在；新增staging与源产物的SHA-256一致性校验；失败信息带真实退出码与stderr。
- **`.ps1` 脚本一律保持纯ASCII。** Windows PowerShell 5.1 在没有BOM时按ANSI读取脚本，含中文的脚本会被解码成乱码并语法报错。本次给 `test.ps1`、`verify-release.ps1`、`verify-release-account.ps1` 都加了该约束注释。
- **engine边界检查此前只存在于 `.ps1` 中**，`go test ./...` 单独运行无法发现边界违规。新增 `internal/engine/boundary_test.go`，把边界规则做成Go测试（用 `go/build` 检查真实导入图，覆盖测试文件），并对 `CurrentStatus` 加纯度断言。已用注入 `import _ "os"` 的方式反证该守卫会失败。
- **CLI测试覆盖不全**：新增 `--help`、`-h` 与 `--help` 等值、参数过多路径、以及无参数启动措辞的断言。`internal/cli` 测试从3项增至7项。
- **`test.ps1` 现在同时运行TASK-02探针测试**（`python -m unittest discover`），可用 `-SkipProbeTests` 跳过。

2026-09-23复验结果：

- `go test ./...` 通过（`internal/cli` 7项、`internal/engine` 2项）；`go vet ./...` 无输出。
- `go list -m all` 仍只有主模块；`internal/engine` 导入列表为空，边界干净。
- `dist/wendao.exe` 为1,681,408字节；SHA-256为`2b43e8afe230638090dccd9e3de2919d90cc7c439d7fd1cfbf08afa05be3ed21`。
  （与上一轮1,681,920字节的差异来自Go补丁版本与版本字符串不同，量级一致。）
- `verify-release.ps1` 本次**真实通过**：PATH仅System32下 `--version`/`--diagnose` 退出码为0，输出确认 `network=disabled`、`game_state=not_implemented`，且staging产物哈希与源一致。
- 新增 `scripts/verify-release-account.ps1`：在任意账户下采集独立账户验收证据（用户、profile、LOCALAPPDATA、OS版本、哈希、受限PATH下的三组输出、是否生成了数据根目录），输出 `TASK03-ACCOUNT-EVIDENCE-BEGIN/END` 包裹的证据块。**开发者账户下的采集结果不能替代干净测试账户验收**，该未测项仍然成立。

### 4.3 2026-09-23 `verify-release-account.ps1` 预跑与修复

在开发者账户与"脚本+产物单独拷入独立目录"两种布局下预跑脚本，发现并修复两处会影响跨账户使用的真实问题：

- **staging 路径推导错误（会写盘符根目录）。** 原实现用 `Split-Path -Parent $PSScriptRoot` 求仓库根。脚本一旦被单独拷到 `C:\wendao-check\`，仓库根会算成 `C:\`，staging 目录变成 `C:\.task-cache\account-check`，即试图写入盘符根目录。修复：以 `go.mod` 作为仓库布局标记，找不到标记时回落到脚本自身目录下的 `.account-check\`。两条分支均已验证，并确认盘符根目录未被创建。
- **`computer=` 字段为空。** 原实现读 `$env:COMPUTERNAME`，该变量在部分宿主进程里不被传递。修复：为空时回落到 `[System.Environment]::MachineName`；同法加固 `user=` 对 `USERDOMAIN` 缺失的处理。
- **新增开发者账户自动提醒。** 首次真实尝试时，测试账户 `wendao-test` 已创建并提权成功，但检查是在**原来的 PowerShell 窗口**里执行的（`Win+L` 切换用户只是锁屏，原会话仍在后台），于是证据块记录的是开发者 profile，而哈希、版本输出、`network=disabled`、`account_check=passed` 等其余字段全部正常，**极易被误当成本次验收通过**。修复：新增 `looks_like_developer_profile` 字段（用户名是 `Administrator`/`admin`、或 profile 不干净、或数据根目录已存在即命中），命中时额外打印三条警告，明确写出「开发者账户结果不能作为独立账户验收记录」。验证：在开发者账户下运行确实输出 `looks_like_developer_profile=True` 并打印三条警告。

预跑结果（开发者账户）：`account_check=passed`，`sha256` 与源产物一致，`version_out_restricted_path=wendao 0.0.0-dev (windows/amd64)`，`network=disabled`、`game_state=not_implemented` 均确认，`go_reachable_under_restricted_path=False`，`data_root_created=False`，`profile_looks_fresh=True`。独立目录布局下同样 `passed`。

**该预跑不能替代验收**：它仍是开发者 profile。脚本现在被证明在两种文件布局下都能工作，剩下的唯一变量就是用户配置文件本身。操作步骤见[独立账户验收操作指南](../TASK-03-独立账户验收操作指南.md)。

当前仍没有创建新的Windows用户账户，因此"不同的干净用户配置文件"仍为未测项；不能把隔离目录和PATH试验写成独立账户验收。TASK-03实现与发行试验完成，关闭任务前补一次独立测试账户记录。macOS和Linux也未测试，不声明支持。

> 上段为当次记录，**已于 2026-09-23 完成**，见 §4.4。

### 4.4 2026-09-23 独立干净用户账户验收（已完成）

创建本地测试账户 `wendao-test`（管理员），登录该账户后在**全新 PowerShell 窗口**中执行：

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File C:\wendao-check\verify-release-account.ps1 -Label "fresh-local-account"
```

原始证据（完整块）：

```text
----- TASK03-ACCOUNT-EVIDENCE-BEGIN -----
label=fresh-local-account
user=WIN-20231210AVZ\wendao-test
userprofile=C:\Users\wendao-test
localappdata=C:\Users\wendao-test\AppData\Local
computer=WIN-20231210AVZ
os=Microsoft Windows NT 10.0.19045.0
sha256=2b43e8afe230638090dccd9e3de2919d90cc7c439d7fd1cfbf08afa05be3ed21
version_out_restricted_path=wendao 0.0.0-dev (windows/amd64)
diagnose_out_restricted_path=product=wendao / version=0.0.0-dev / platform=windows/amd64 / go_runtime=go1.26.0 / network=disabled / game_state=not_implemented
bare_out=《问道长生》工程骨架已启动；游戏功能尚未开放。 / 运行 wendao --help 查看当前可用命令。
go_present_on_host_disk=True
go_reachable_under_restricted_path=False
expected_data_root=C:\Users\wendao-test\AppData\Local\WendaoChangsheng
data_root_created=False
profile_looks_fresh=True
looks_like_developer_profile=False
run_path=only System32
account_check=passed
----- TASK03-ACCOUNT-EVIDENCE-END -----
```

逐项判读：

| 项 | 值 | 结论 |
|---|---|---|
| 运行身份 | `WIN-20231210AVZ\wendao-test` | 非开发者账户，验收成立 |
| 用户 profile | `C:\Users\wendao-test` | 全新 profile |
| `looks_like_developer_profile` | `False` | 脚本自证非开发者账户 |
| `profile_looks_fresh` | `True` | 该 profile 从未运行过本程序 |
| SHA-256 | `2b43e8afe230638090dccd9e3de2919d90cc7c439d7fd1cfbf08afa05be3ed21` | **与开发者账户下的产物完全一致**，证明是同一份产物、未被替换 |
| 受限 PATH 下 `--version` | `wendao 0.0.0-dev (windows/amd64)` | 退出码 0，可启动 |
| `network=disabled` | 存在 | 离线运行确认 |
| `game_state=not_implemented` | 存在 | 产物未谎称已实现游戏 |
| `go_reachable_under_restricted_path` | `False` | 不依赖本机 Go 工具链 |
| `data_root_created` | `False` | 只读诊断**未**创建数据目录 |
| `account_check` | `passed` | 三项调用退出码均为 0 |

**这条证据关闭了 ADR-002 与 TASK-03 的最后一项未测项**：产物在一个从未运行过它的干净 Windows 用户配置文件下、且 PATH 仅含 System32 时，成功启动并如实报告离线与未实现状态。

**仍未覆盖（与本验收无关，继续如实声明）**：产物未签名；macOS 与 Linux 未测试，不声明支持；本次验证的是工程诊断入口，不是游戏功能（游戏尚未实现）。

2026-09-23新增的 `--version` 输出与诊断输出（受限PATH下）：

```text
wendao 0.0.0-dev (windows/amd64)
product=wendao / version=0.0.0-dev / platform=windows/amd64 /
go_runtime=go1.26.0 / network=disabled / game_state=not_implemented
```

### 4.2 本机工具链与已知运行环境限制

- 本机Go为 `go1.26.0`，与 `go.mod` 的 `go 1.26.0` 精确一致；`GOPATH=C:\Users\Administrator\go`。
- 用户级Go配置已写入 `C:\Users\Administrator\AppData\Roaming\go\env`：`GOTOOLCHAIN=go1.26.0`（禁止构建期静默下载另一套工具链）、`CGO_ENABLED=0`。
- Git Bash 环境中 `APPDATA` 未导出，会让 `go env -w` 报 `%AppData% is not defined` 且 `go env GOTOOLCHAIN` 回落到 `auto`（配置看似未生效）。导出 `APPDATA` 后正常；PowerShell下无此问题。
- Git Bash 中 `GOCACHE`/`GOTMPDIR` 不能写成 `/c/...` MSYS 路径，Go 是原生Windows程序不认该转换，必须用 `C:/...` 盘符形式。
- 详细环境记录见[开发环境配置记录](../DEVELOPMENT-ENVIRONMENT.md)。

## 5. 后果

正面结果是正式工程可以在零第三方依赖下编译、测试并交付小型单文件，领域层容易保持离线和可重复。代价是终端控制需要维护少量Windows适配代码，版本化快照也必须自行严谨处理锁、刷盘、替换和迁移。

后续任何改为SQLite、加入终端框架、启用CGO或改变发行平台的决定，都应更新本ADR、依赖锁和发行证据。TASK-02的终端矩阵与本ADR互补：一个验证交互行为，一个验证正式语言和玩家产物；二者都不能替代TASK-09/17的完整TUI与M1发行验收。

### 后续状态说明（2026-09-24，TASK-09）

本ADR前文的 `game_state=not_implemented` 输出及对应脚本证据均为TASK-09之前的历史快照，不代表当前构建。TASK-09实现预设创角、普通修炼、本地保存/恢复和正式TUI后，当前诊断值为 `game_state=short_loop_implemented`；`scripts/verify-release.ps1` 与 `scripts/verify-release-account.ps1` 已同步更新。完整M1仍未完成，且正式Go界面的Windows Terminal等人工兼容矩阵仍待补测，不能仅凭短循环称为完成发行验收。
