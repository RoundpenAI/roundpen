# 移动端控制台（Flutter）

连接自托管 Roundpen 控制面的手机客户端。**与「Mobile 槽位」（QEMU 里的 Agent 环境）无关**，只是名字相近。

设计与计划见 `docs/superpowers/specs/2026-09-20-mobile-console-design.md` 与
`docs/superpowers/plans/2026-09-20-mobile-console.md`。

## 本地运行

```bash
make dev            # 先在仓库根目录起控制面（roundpend :19001）
make mobile-setup   # 首次：flutter pub get
make mobile-dev     # 模拟器 / 真机
```

App 里填服务器地址：模拟器用 `http://10.0.2.2:19001`（Android）或 `http://127.0.0.1:19001`（iOS），
真机填本机局域网地址 `http://<局域网IP>:19001`。

**不要用 `flutter run -d chrome` 调试**：WebSocket 鉴权靠原生握手 header，浏览器会丢弃它，
连接必然 401。本工程也只生成 `android/` 与 `ios/`。

iOS 首次访问局域网地址会弹「本地网络」权限，必须允许；自签 HTTPS 会被系统拒绝，局域网请用 http。

## 检查

```bash
make mobile-analyze   # flutter analyze
make mobile-test      # flutter test
```
