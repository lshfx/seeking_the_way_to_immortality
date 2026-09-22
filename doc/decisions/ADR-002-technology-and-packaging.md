# ADR 002 正式技术栈与 Windows 便携发行

- 状态：已采纳；独立 Windows 用户账户验收待补
- 日期：2026-09-22
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
```

2026-09-22结果：

- `go test ./...` 与 `go vet ./...` 通过；脚本同时检查engine没有直接导入终端、存储、网络、墙上时钟或系统调用包，并确认当前只有主模块。
- `dist/wendao.exe` 为1,681,920字节；SHA-256为`8d4561e58a9eb08cebbc92ca1a026a177993c5dd8d896cdfdf44018f71df46c6`。
- Go构建元数据显示`GOOS=windows`、`GOARCH=amd64`、`CGO_ENABLED=0`、`trimpath=true`。
- 将产物复制到仅含该exe的隔离目录，并把PATH缩到Windows System32后，`--version`和`--diagnose`均成功；诊断明确`network=disabled`和`game_state=not_implemented`。
- 产物尚未签名。签名不是当前本地开发验证的前提，但若公开分发应评估SmartScreen体验和签名成本。

当前没有创建新的Windows用户账户，因此“不同的干净用户配置文件”仍为未测项；不能把隔离目录和PATH试验写成独立账户验收。TASK-03实现与发行试验完成，关闭任务前补一次独立测试账户记录。macOS和Linux也未测试，不声明支持。

## 5. 后果

正面结果是正式工程可以在零第三方依赖下编译、测试并交付小型单文件，领域层容易保持离线和可重复。代价是终端控制需要维护少量Windows适配代码，版本化快照也必须自行严谨处理锁、刷盘、替换和迁移。

后续任何改为SQLite、加入终端框架、启用CGO或改变发行平台的决定，都应更新本ADR、依赖锁和发行证据。TASK-02的终端矩阵与本ADR互补：一个验证交互行为，一个验证正式语言和玩家产物；二者都不能替代TASK-09/17的完整TUI与M1发行验收。
