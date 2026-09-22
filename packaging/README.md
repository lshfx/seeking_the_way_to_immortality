# Windows 便携发行

TASK-03 冻结首个目标为 Windows x64 便携包。当前只生成技术骨架 `dist/wendao.exe`，还不是 M1 游戏发行版。

```powershell
.\scripts\test.ps1
.\scripts\build.ps1 -Version 0.0.0-task03
.\scripts\verify-release.ps1
```

构建使用 `CGO_ENABLED=0`、`GOOS=windows`、`GOARCH=amd64` 和 `-mod=readonly`。当前模块没有第三方依赖，构建和启动都不会下载组件。`dist/` 为本地产物目录，不提交二进制。

TASK-17 再补便携压缩包、许可清单、干净用户环境、数据目录和升级验证；TASK-28负责长期发行流程。
