# Windows 便携发行

TASK-03 冻结首个目标为 Windows x64 便携包。当前只生成技术骨架 `dist/wendao.exe`，还不是 M1 游戏发行版。

```powershell
.\scripts\test.ps1
.\scripts\build.ps1 -Version 0.0.0-task03
.\scripts\verify-release.ps1
.\scripts\verify-release-account.ps1 -Label "<账户标签>"
```

构建使用 `CGO_ENABLED=0`、`GOOS=windows`、`GOARCH=amd64` 和 `-mod=readonly`。当前模块没有第三方依赖，构建和启动都不会下载组件。`dist/` 为本地产物目录，不提交二进制。

`verify-release.ps1` 会把产物复制到隔离目录，把 PATH 缩到仅 System32，运行 `--version` 与 `--diagnose`，并校验隔离副本与源产物的 SHA-256 一致。它通过 `System.Diagnostics.Process` 读取真实退出码——早期版本依赖 `$LASTEXITCODE`，在部分 PowerShell 宿主中该值不可靠，导致脚本在任何真实断言前就误报失败。

`verify-release-account.ps1` 用于补齐"独立 Windows 用户账户"验收：在开发者账户和新建的干净测试账户下各运行一次，把输出的 `TASK03-ACCOUNT-EVIDENCE-BEGIN/END` 块贴入记录。**在开发者账户下运行不能替代干净账户验收。**

维护约定：本目录下 `.ps1` 脚本保持**纯 ASCII**。Windows PowerShell 5.1 在没有 BOM 时按 ANSI 读取脚本，含中文会乱码并语法报错。

TASK-17 再补便携压缩包、许可清单、干净用户环境、数据目录和升级验证；TASK-28负责长期发行流程。
