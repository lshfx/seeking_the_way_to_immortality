# TASK-03 独立账户验收操作指南

- 更新日期：2026-09-23
- **状态：已完成 ✅**（2026-09-23，账户 `wendao-test`，`account_check=passed`，`looks_like_developer_profile=False`）
- 本指南保留为**复现步骤**与故障排查参考；原始证据见 [ADR-002 §4.4](decisions/ADR-002-technology-and-packaging.md)。

**目的**：把 TASK-03 最后一项「独立干净 Windows 用户账户验收」做掉。

**前置**：`dist\wendao.exe` 已构建（当时 1,681,408 字节，SHA-256 `2b43e8af...be3ed21`）。

## 验收结果

```text
user=WIN-20231210AVZ\wendao-test          ← 新账户
userprofile=C:\Users\wendao-test          ← 干净 profile
looks_like_developer_profile=False        ← 脚本自证
profile_looks_fresh=True
sha256=2b43e8af...be3ed21                 ← 与开发者账户产物一致
go_reachable_under_restricted_path=False
data_root_created=False
account_check=passed
```

关键结论：产物在一个**从未运行过它的干净 Windows 用户配置文件**下、且 PATH 仅含 System32 时，成功启动并如实报告 `network=disabled`、`game_state=not_implemented`。

以上结论和下方证据块是TASK-09实现前的历史快照。当前构建诊断改为 `game_state=short_loop_implemented`；独立账户脚本已更新为检查此值，并检查无交互终端时安全拒绝启动。当前开发账户的脚本预跑不替代新的干净账户验收。

**当时踩过的坑（值得记住）**：第一次尝试时账户已建好，但检查是在**原来的 PowerShell 窗口**里跑的——`Win+L` 切换用户只是锁屏，原会话仍在后台。证据块记下的是开发者 profile，而其余字段全部正常，极易误判通过。脚本现已有 `looks_like_developer_profile` 提醒。

## 为什么必须换账户

项目此前做的是**隔离目录 + PATH 只留 System32** 的试验。那是两个不同的主张：

| 主张 | 已做了什么 | 证明了什么 |
|---|---|---|
| 隔离目录 + 受限 PATH | 已做（`verify-release.ps1`） | 不依赖本机额外 PATH 项、不下载临时依赖 |
| **干净用户配置文件** | **未做** | 不依赖开发者 profile 里的缓存、环境变量、已有数据目录 |

开发机上的 `%LOCALAPPDATA%`、`%APPDATA%`、`PATH`、以及任何已存在的 `~/.wendao` 或 `%LOCALAPPDATA%\WendaoChangsheng` 目录，都可能**掩盖**首次运行才会暴露的问题。所以必须在**一个从未跑过本程序的用户配置文件**下验证。

脚本无法替你创建账户——这需要管理员权限。所以流程是：你建账户 → 登进去 → 跑脚本 → 把证据块贴回来。

## 第一步：创建一个新的本地测试账户

**方式 A：设置界面（最简单）**

1. `Win + I` → 「账户」→「其他用户」→「添加账户」。
2. 选「我没有这个人的登录信息」→「添加一个没有 Microsoft 账户的用户」。
3. 用户名填 `wendao-test`，密码随意（例如 `Test!2345`，需满足复杂度）。
4. 建好后点该账户 →「更改账户类型」→ 选 **管理员**（便于后续操作）→ 确定。

**方式 B：命令行（需要管理员 PowerShell）**

```powershell
# 以管理员身份打开 PowerShell
$pw = ConvertTo-SecureString 'Test!2345' -AsPlainText -Force
New-LocalUser -Name 'wendao-test' -Password $pw -PasswordNeverExpires
Add-LocalGroupMember -Group 'Administrators' -Member 'wendao-test'
```

**方式 C：如果只是想快速验一次，不想建持久账户**

可以「新建一个本地账户 → 验完删掉」。删除命令（管理员）：

```powershell
Remove-LocalUser -Name 'wendao-test'
```

> 注意：`Remove-LocalUser` 会连同该用户的 profile 目录一起移除，不会影响你的开发者账户。若想保留证据文件，先把文档拷出来。

## 第二步：切换到新账户并准备文件

> **这一步最容易做错。** 在 PowerShell 窗口里执行 `Win+L` 或用 `runas` **都不会**让后续命令跑在新账户下。**必须真正登录新账户，并重新打开一个新的 PowerShell 窗口。**

### 2.1 把文件放到两个账户都能访问的位置

新账户访问 `C:\Users\Administrator\...` 会受权限限制，所以先把产物和脚本复制到共享目录。

在**当前（Administrator）账户**里执行一次：

```powershell
New-Item -ItemType Directory -Force -Path 'C:\wendao-check' | Out-Null
Copy-Item 'C:\Users\Administrator\Desktop\play\dist\wendao.exe' 'C:\wendao-check\' -Force
Copy-Item 'C:\Users\Administrator\Desktop\play\scripts\verify-release-account.ps1' 'C:\wendao-check\' -Force
```

> `C:\wendao-check` 是刻意选的：它在两个账户下都可访问，且**不触发**新账户对 `C:\Users\Administrator` 的权限限制。

### 2.2 切换到新账户

**推荐做法（图形界面，最稳）**：

1. 按 `Win + L` 锁定屏幕（或「开始」→ 用户头像 → 「切换用户」）。
2. 在登录界面左下角选择 **`wendao-test`**。
3. 输入密码 `Test!2345`，登录。
4. **等待首次登录的"正在准备 Windows"完成**（新账户首次登录会初始化 profile，可能耗时数十秒到几分钟）。
5. **重要：重新打开一个新的 PowerShell 窗口**（`Win+X` → 终端 / 或开始菜单搜 PowerShell）。
   - 不要用之前那个 Administrator 的窗口——切换用户只是锁屏，原会话仍在后台，那个窗口还是 Administrator。
6. 打开后**先确认身份**：

```powershell
whoami
```

**必须输出 `win-20231210avz\wendao-test`（或含 `wendao-test`）。若仍显示 `administrator`，说明你还在旧会话，回到第 5 步。**

**备选做法（命令行，免登录）**：

```powershell
# 在 Administrator 的 PowerShell 里执行；会提示输入 wendao-test 的密码
runas /user:wendao-test powershell
```

这会在新账户下开一个 PowerShell。`runas` 启动的会话环境变量是新账户的，脚本判定可用；但它不创建完整交互式 profile 会话，**不如图形登录真实**。如果用它，请在证据里注明 `via=runas`，并在记录中标注该限制。**优先用图形登录。**

### 2.3 在真正的新账户下运行

确认 `whoami` 显示 `wendao-test` 之后：

```powershell
cd C:\wendao-check
.\verify-release-account.ps1 -Label "fresh-local-account"
```

**不需要传 `-Binary`**：脚本会自动探测。当 `C:\wendao-check\` 里没有 `go.mod`（即"脚本被单独拷出来"的场景），它会自动选用同目录下的 `wendao.exe`，并把临时 staging 目录建在 `C:\wendao-check\.account-check\` 内——**不会**跑到仓库布局或盘符根目录去写东西。这两条分支都已验证。

**如果脚本报执行策略错误**（提示 `无法加载文件...因为在此系统上禁止运行脚本`）：

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File C:\wendao-check\verify-release-account.ps1 -Label "fresh-local-account"
```

这不必改系统策略，只对这一次进程生效。

**如果提示找不到脚本或产物**，先确认：

```powershell
Test-Path C:\wendao-check\verify-release-account.ps1
Test-Path C:\wendao-check\wendao.exe
```

两个都应为 `True`。若产物在别处，用 `-Binary` 显式指定：

```powershell
.\verify-release-account.ps1 -Binary "D:\somewhere\wendao.exe" -Label "fresh-local-account"
```

## 第三步：核对证据块

脚本输出应形如：

```text
----- TASK03-ACCOUNT-EVIDENCE-BEGIN -----
label=fresh-local-account
user=<机器名>\wendao-test          ← 必须含 wendao-test
userprofile=C:\Users\wendao-test   ← 必须指向新账户目录
localappdata=C:\Users\wendao-test\AppData\Local
computer=<机器名>
os=...
sha256=2b43e8afe230638090dccd9e3de2919d90cc7c439d7fd1cfbf08afa05be3ed21
version_out_restricted_path=wendao 0.0.0-dev (windows/amd64)
diagnose_out_restricted_path=product=wendao / ... / network=disabled / game_state=not_implemented
bare_out=...
go_present_on_host_disk=True
go_reachable_under_restricted_path=False
expected_data_root=C:\Users\wendao-test\AppData\Local\WendaoChangsheng
data_root_created=False
profile_looks_fresh=True
run_path=only System32
account_check=passed
----- TASK03-ACCOUNT-EVIDENCE-END -----
```

**先自己看一眼 `user=` 和 `userprofile=` 两行。** 如果还显示 `Administrator` / `C:\Users\Administrator`，就说明没切过去，这次结果**不能**计入验收。

> **脚本现在会自动提醒**：如果检测到你在开发者账户下运行，会多输出一行 `looks_like_developer_profile=True`，并在末尾打印三条警告。看到这个就必须重做。判定依据是「用户名是 Administrator/admin」「profile 不干净」「数据根目录已存在」三者任一。
>
> 这条提醒是被真实误操作逼出来的：第一次尝试时账户建好了，但检查是在**原来的 PowerShell 窗口**里跑的，于是证据记下的是开发者 profile，而其余字段看起来全都正常。

## 第四步：把证据贴回来

**把整段（含两行标记）复制给我**，我写进 ADR-002 和开发方案，然后 TASK-03 就可以关闭。

### 我要在证据里核对的关键项

| 项 | 期望值 | 为什么重要 |
|---|---|---|
| `user` | 新账户名（不是 `Administrator`） | 证明真的换了配置文件 |
| `userprofile` | 指向新账户目录 | 同上 |
| `sha256` | 与开发者账户下一致 | 证明跑的是同一个产物，没被替换 |
| `version_out_restricted_path` | `wendao 0.0.0-dev (windows/amd64)` | 受限 PATH 下仍能启动 |
| `network=disabled` | 必须出现 | 离线运行 |
| `game_state=not_implemented` | 原TASK-03历史产物必须出现；当前重跑版本请见文首说明 | 原始产物没有谎称已实现游戏 |
| `go_reachable_under_restricted_path` | `False` | 证明不依赖本机 Go 工具链 |
| `data_root_created` | `False` | 只读诊断**不应**创建数据目录 |
| `profile_looks_fresh` | `True` | 确认这是干净 profile |
| `account_check` | `passed` | 三项退出码均为 0 |

## 若验收失败

脚本会在 `account_check=passed` 之前抛错并给出真实退出码与 stderr。**不要**手工把 `passed` 补上——把错误原文贴回来，这属于要修的真实缺陷，或要如实记入"未通过"。

## 已知边界（与本次验收无关，但关闭任务时要一起声明）

- 产物**未签名**。SmartScreen 首次运行会有提示，这是已知项，不是本次验收内容。
- macOS / Linux **未测试，不声明支持**。
- 这只是一个诊断入口（`--version`/`--diagnose`），**尚无游戏功能代码**。本次验收证明的是"产物在干净账户下能跑"，不是"游戏能在干净账户下玩"。

## 本次修复（2026-09-23）

预跑时发现两处会影响跨账户使用的真实问题，已修：

1. **staging 路径推导错误。** 原脚本用 `Split-Path -Parent $PSScriptRoot` 求仓库根。一旦你把脚本单独拷到 `C:\wendao-check\`，仓库根会算成 `C:\`，staging 目录变成 `C:\.task-cache\account-check`——**试图往盘符根目录写东西**。
   - 修复：用 `go.mod` 作为仓库标记来判定布局；找不到标记时回落到脚本自身目录下的 `.account-check\`。已分别验证两条分支，并确认盘符根目录未被创建。
2. **`computer=` 字段为空。** 原脚本读 `$env:COMPUTERNAME`。该变量在部分宿主进程里不会被传递，导致证据块出现空字段。
   - 修复：为空时回落到 `[System.Environment]::MachineName`（.NET 始终可用，是权威来源）。同法加固了 `user=` 字段对 `USERDOMAIN` 缺失的处理。

3. **新增开发者账户自动提醒（2026-09-23，被真实误操作逼出来）。** 第一次尝试时用户已成功创建 `wendao-test` 并设为管理员，但**检查是在原来的 PowerShell 窗口里跑的**——`Win+L` 切换用户只是锁屏，原会话仍在后台。结果证据块记录的是开发者 profile，而其余所有字段（哈希、版本输出、`network=disabled`、`account_check=passed`）看起来全都正常，**极容易被误当成本次验收通过**。
   - 修复：新增 `looks_like_developer_profile` 字段（判定：用户名是 `Administrator`/`admin`，或 profile 不干净，或数据根目录已存在），命中时在末尾打印三条警告并明确写出「开发者账户结果不能作为独立账户验收记录」。
   - 同时把指南里"切换账户"那节重写：明确 `Win+L` 只是锁屏、**必须重新打开新的 PowerShell 窗口**、并用 `whoami` 自证身份。

**开发者账户预跑结果**（仅供参考，**不能**替代干净账户验收）：

```text
label=dev-account-pretest2
user=WIN-20231210AVZ\Administrator
userprofile=C:\Users\Administrator
computer=WIN-20231210AVZ
sha256=2b43e8afe230638090dccd9e3de2919d90cc7c439d7fd1cfbf08afa05be3ed21
version_out_restricted_path=wendao 0.0.0-dev (windows/amd64)
diagnose_out_restricted_path=product=wendao / version=0.0.0-dev / platform=windows/amd64 / go_runtime=go1.26.0 / network=disabled / game_state=not_implemented
bare_out=《问道长生》工程骨架已启动；游戏功能尚未开放。 / 运行 wendao --help 查看当前可用命令。
go_present_on_host_disk=True
go_reachable_under_restricted_path=False
data_root_created=False
profile_looks_fresh=True
run_path=only System32
account_check=passed
```

同样在"脚本 + exe 单独拷进独立目录"的场景下复跑通过（`account_check=passed`，staging 落在该目录内）。说明脚本在两种布局下都能工作，剩下的唯一变量就是**用户配置文件本身**。
