# seeking_the_way_to_immortality

《问道长生》是一款面向碎片时间的本地终端修仙游戏。TASK-01已完成M1范围与规则冻结；TASK-02正在补目标终端实测；TASK-03已建立Go正式工程和Windows x64独立产物，独立测试账户验收待补。游戏功能尚未实现。

## 开发验证

```powershell
.\scripts\test.ps1
.\scripts\build.ps1 -Version 0.0.0-dev
.\scripts\verify-release.ps1
```

构建结果在本地 `dist/wendao.exe`，该目录不提交。当前程序只提供工程诊断入口，不是可玩的M1版本。

## 项目文档

- [游戏与技术设计文档](doc/问道长生_游戏与技术设计文档.md)
- [分阶段开发方案](doc/问道长生_分阶段开发方案.md)
- [ADR-001 M1产品范围与规则冻结](doc/decisions/ADR-001-m1-product-freeze.md)
- [ADR-002 正式技术栈与Windows便携发行](doc/decisions/ADR-002-technology-and-packaging.md)
