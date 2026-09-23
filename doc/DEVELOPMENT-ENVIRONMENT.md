# 开发环境配置记录

- 更新日期：2026-09-23
- 适用范围：本机 Windows 开发 / 构建 / 测试《问道长生》
- 依据：[ADR-002 正式技术栈与 Windows 便携发行](decisions/ADR-002-technology-and-packaging.md)

## 1. 结论：本机已就绪

正式语言为 **Go 1.26**，ADR-002 记录当时验证工具链为 **Go 1.26.5**。
本机当前安装的是 **Go 1.26.0**，与 `go.mod` 声明的 `go 1.26.0` 精确一致，无需升级或降级。

| 项目 | 要求 | 本机实际 | 状态 |
|---|---|---|---|
| Go 版本 | 1.26（`go.mod`: `go 1.26.0`） | go1.26.0 windows/amd64 | 一致 |
| 安装路径 | — | `C:\Program Files\Go` | 已装 |
| `go` 可执行文件 | 在 PATH 中 | `C:\Program Files\Go\bin\go` | 已装 |
| `GOPATH` | 默认 | `C:\Users\Administrator\go` | 正常 |
| `CGO_ENABLED` | `0`（固定） | `0` | 已固定 |
| 第三方模块 | 0 个 | 1 个（仅主模块自身） | 符合 |
| 发行目标 | windows/amd64 | windows/amd64 | 一致 |

版本说明：`go.mod` 用 `go 1.26.0` 而非 `go 1.26.5` 是刻意选择。Go 的 `go` 指令是**最低版本要求**，写 `1.26.0` 让工具链小版本有兼容余量；ADR-002 里的 1.26.5 只是当时的验证环境记录，不是硬性下限。

## 2. 已执行的配置

### 2.1 固定工具链版本

写入用户级 Go 配置文件 `C:\Users\Administrator\AppData\Roaming\go\env`：

```text
GOTOOLCHAIN=go1.26.0
CGO_ENABLED=0
```

- `GOTOOLCHAIN=go1.26.0`：禁止 Go 在遇到更高 `go` 指令时**静默联网下载**另一套工具链。这直接对应 ADR-002 的"启动与构建不下载组件"要求。
- `CGO_ENABLED=0`：ADR-002 硬性要求，保证单文件静态产物。

验证：

```bash
go env GOTOOLCHAIN CGO_ENABLED
# go1.26.0
# 0
```

### 2.2 已知环境问题：`APPDATA` 未定义

**现象**：在 Git Bash 中直接执行 `go env -w` 报错 `cannot find go env config: %AppData% is not defined`，且 `go env GOTOOLCHAIN` 返回 `auto`，配置看似"没生效"。

**原因**：Git Bash 会话未导出 `APPDATA` 变量，Go 无法解析用户配置路径 `%AppData%\go\env`。配置文件本身是正确的，只是读取路径解析失败。

**影响范围**：仅影响从 Git Bash 调用 `go`、且需要读取用户级 `go env` 配置的场景（如依赖 `GOTOOLCHAIN` 的文件配置）。在原生 PowerShell / CMD 下 `APPDATA` 正常，无此问题。

**规避方式**：在 Git Bash 中先导出变量：

```bash
export APPDATA="C:\\Users\\Administrator\\AppData\\Roaming"
```

PowerShell 下无需任何额外处理。

### 2.3 已知环境问题：Git Bash 路径风格

**现象**：在 Git Bash 中把 `GOCACHE` / `GOTMPDIR` 设成 `/c/Users/...` 形式时，`go` 报
`creating work dir: GetFileAttributesEx /c/Users/... : The system cannot find the path specified.`。

**原因**：Go 是原生 Windows 程序，不认 MSYS 的 `/c/...` 路径转换（该转换只对命令行参数生效，环境变量不转换）。

**规避方式**：环境变量一律用盘符形式，正斜杠或反斜杠均可：

```bash
GOCACHE="C:/Users/Administrator/Desktop/play/.task-cache/go-build"
GOTMPDIR="C:/Users/Administrator/Desktop/play/.task-cache/go-tmp"
```

项目自带的 `.ps1` 脚本不受影响，因为它们用 PowerShell 的 `Join-Path` 生成原生路径。
推荐直接在 PowerShell 中运行这三个脚本，避免上述两类 Git Bash 问题。

## 3. 构建与测试

项目已提供四个脚本（PowerShell），命令与 README 一致：

```powershell
.\scripts\test.ps1                          # Go 测试 + vet + 边界检查 + TASK-02 探针测试(56项)
.\scripts\build.ps1 -Version 0.0.0-dev
.\scripts\verify-release.ps1                # 隔离目录 + PATH 仅 System32 + 哈希一致性
.\scripts\verify-release-account.ps1        # 独立账户验收证据采集
```

`test.ps1` 可用 `-SkipProbeTests` 只跑 Go 侧。脚本会自行设置 `GOCACHE` / `GOTMPDIR` 到仓库内 `.task-cache\`，并强制 `CGO_ENABLED=0`、`GOOS=windows`、`GOARCH=amd64`，因此**不依赖本机全局 Go 配置**，是可复现的。

### 3.1 脚本必须是纯 ASCII

`scripts/` 与 `packaging/` 下的 `.ps1` 一律保持纯 ASCII。Windows PowerShell 5.1 在没有 BOM 时按 ANSI（本机为 GBK）读取脚本文件，含中文会解码成乱码并触发语法错误，例如把 `throw "找不到产物"` 变成无法解析的乱码 token。需要中文说明时写进 Markdown 文档，不要写进 `.ps1`。

### 3.2 校验脚本读取退出码的方式

`verify-release.ps1` 与 `verify-release-account.ps1` 通过 `System.Diagnostics.Process` 启动产物并读取 `ExitCode`，**不使用 `& $exe` + `$LASTEXITCODE`**。原因：在部分 PowerShell 宿主中，调用控制台程序后 `$LASTEXITCODE` 不会被赋值（保持为空），于是 `$LASTEXITCODE -ne 0` 恒为真。早期版本因此会在任何真实断言之前就抛出 `--version failed with exit code `（退出码为空）并以码 1 退出，却被误读为"产物问题"。

用 `Process` 方式时还有两点必须设置：

- `StandardOutputEncoding` / `StandardErrorEncoding` 设为 UTF-8，否则产物输出的中文会被按控制台代码页解码成乱码。
- 传空参数列表时不要用 `-Arguments @()`，会触发参数绑定错误；改为可选参数并默认空数组。

若需在 Git Bash 中手工构建（注意用盘符路径，见 2.3）：

```bash
cd /c/Users/Administrator/Desktop/play
GOCACHE="C:/Users/Administrator/Desktop/play/.task-cache/go-build" \
GOTMPDIR="C:/Users/Administrator/Desktop/play/.task-cache/go-tmp" \
  CGO_ENABLED=0 GOOS=windows GOARCH=amd64 \
  go build -mod=readonly -trimpath -buildvcs=false \
  -ldflags "-s -w -X main.version=0.0.0-dev" \
  -o dist/wendao.exe ./cmd/wendao
```

注意 `.task-cache/` 若被清理需先重建：`mkdir -p .task-cache/go-build .task-cache/go-tmp`。

## 4. 本次验证结果（2026-09-23）

| 检查项 | 命令 | 结果 |
|---|---|---|
| 工具链版本 | `go version` | go1.26.0 windows/amd64 |
| 模块校验 | `go mod verify` | all modules verified |
| 依赖清单 | `go list -m all` | 仅主模块，0 第三方依赖 |
| 单元测试 | `go test ./...` | 通过（`internal/cli` 7 项、`internal/engine` 2 项） |
| 静态检查 | `go vet ./...` | 通过，无输出 |
| 边界规则 | `go list -f '{{.Imports}}' ./internal/engine` | 无输出，engine 未导入禁用包 |
| 边界守卫（Go 测试） | `go test ./internal/engine/` | 2 项通过；已用注入违规导入反证会失败 |
| CLI 测试 | `go test ./internal/cli/` | 7 项通过 |
| TASK-02 探针测试 | `python -m unittest discover` | 56 项通过 |
| TASK-02 探针自检 | `python verify_probe_selfcheck.py` | 28/28 通过，失败时退出码非 0 |
| 发行校验 | `.\scripts\verify-release.ps1` | 真实通过（受限 PATH、哈希一致） |
| 构建产物 | `go build ...` | `dist/wendao.exe` = 1,681,408 字节 |
| 产物 SHA-256 | `Get-FileHash` | `2b43e8afe230638090dccd9e3de2919d90cc7c439d7fd1cfbf08afa05be3ed21` |
| 产物元数据 | `go version -m dist/wendao.exe` | `CGO_ENABLED=0`, `GOOS=windows`, `GOARCH=amd64`, `trimpath=true` |
| 产物运行 | `dist/wendao.exe --version` | `wendao 0.0.0-dev (windows/amd64)` |
| 产物诊断 | `dist/wendao.exe --diagnose` | `network=disabled`, `game_state=not_implemented` |

产物体积 1,681,408 字节，与 ADR-002 记录的 1,681,920 字节同量级（差异来自 Go 补丁版本与版本字符串不同），符合预期。

## 5. 复现步骤（新机器 / 新账户）

1. 安装 Go 1.26.x（官方 Windows MSI，默认装到 `C:\Program Files\Go`）。
2. 确认 `go version` 输出 `go1.26.x windows/amd64`。
3. 拉取仓库并进入根目录。
4. 执行 `.\scripts\test.ps1`，应全部通过（Go 9 项 + 探针 56 项）。

### 3.3 探针脚本的缓存陷阱（踩过坑）

Python 会按解释器版本把编译结果写进 `tools/task02-terminal-probe/__pycache__/`（文件名形如 `interactive_probe.cpython-314.pyc`）。这曾在 TASK-02 造成一次真实误判：改了源码后运行，看到的仍是**旧版输出**，因为：

- 该目录里残留的是**另一个 Python 版本**编译的缓存；
- 阅读输出时未核对文件时间戳。

**规避**：

```powershell
cd tools\task02-terminal-probe
Remove-Item -Recurse -Force __pycache__   # 改过源码后清一次
python -m unittest discover -p 'test_*.py' -v
```

**另一个容易混淆的点**：本机存在**两个 Python**——受管 `3.13.14` 与系统 `C:\Python314\python.exe`（3.14.3）。`collect_terminal_matrix.py` 会把版本写进证据（`"python": "3.14.3"`），可用于回查。两版下 56 项测试与 28 项自检结果一致，但**记录证据时必须注明用的哪个版本**，否则日后无法复现。
5. 执行 `.\scripts\build.ps1 -Version 0.0.0-dev`，确认产出 `dist\wendao.exe`。
6. 执行 `.\scripts\verify-release.ps1` 做发行校验。
7. 在**新建的干净 Windows 用户账户**下执行 `.\scripts\verify-release-account.ps1 -Label "<账户标签>"`，把证据块贴入记录。

不需要 Perl、不需要 Python、不需要 Node、不需要任何包管理器来构建本项目。只有 TASK-02 的 Python 探针需要 Python 3 标准库。

## 6. 尚未完成的环境相关事项

以下沿用 ADR-002 与任务文档的未决项，**不是环境配置缺陷**，但会影响验收结论：

- **独立测试账户验收**：尚未在新的干净 Windows 用户配置文件下验证产物，不能用"隔离目录 + 精简 PATH"替代。采集脚本 `verify-release-account.ps1` 已就绪，TASK-03 关闭前需在真实新账户下运行并补记录。
- **签名**：`wendao.exe` 未签名。本地开发不受影响；若公开分发需评估 SmartScreen 与签名成本。
- **TASK-02 目标终端实测**：Windows Terminal / VS Code / conhost 的完整交互矩阵仍待补，TASK-02 保持进行中。
- **macOS / Linux**：未测试，不声明支持。

## 7. 沙箱/受限环境提示

若在受限沙箱中通过被拦截的 shell 执行本项目脚本，可能遇到"无法将 `go` 识别为 cmdlet"这类报错——那是沙箱拦截了外部程序调用（即使 `C:\Program Files\Go\bin` 在 PATH 上、`go.exe` 确实存在）。判断方法：用另一个未被拦截的 shell 直接执行 `go version`，若正常则属环境限制而非项目问题。

这类情况下校验逻辑本身仍然有效，只是无法在该 shell 内运行 `.ps1`;可按第 3 节的手工命令逐个执行等价步骤来验证。

