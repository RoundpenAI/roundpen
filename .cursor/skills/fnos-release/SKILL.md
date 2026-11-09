---
name: fnos-release
description: >-
  Roundpen fnOS release: bump VERSION, branch/commit/tag/push, open PR, make fpk,
  install on NAS mike@10.10.1.3, then checkout master. Use when the user says
  部署飞牛、部署到飞牛、发布飞牛、fnOS release/deploy, asks to ship a Roundpen
  fpk to the NAS, or after finishing shippable product changes (auto-deploy then
  notify — do not wait for an explicit deploy ask unless user said WIP / 先别部署).
---

# Roundpen 飞牛发布

按顺序做完下面四步，不要只改版本或只打包。目标机：`mike@10.10.1.3`（x86_64）。`appcenter-cli` 必须 `sudo -n`。

```
Progress:
- [ ] 1. Bump version
- [ ] 2. Branch, commit, tag, push, open PR
- [ ] 3. make fpk + install on NAS
- [ ] 4. Switch back to master
```

## 1. Bump version

`make fpk` 默认读仓库最高 `v*` tag（`git tag -l 'v[0-9]*' --sort=-v:refname`）。不必再改 Makefile / 脚本硬编码。

版本怎么定：

- 用户给了版本（如 `2.5.0`）→ 用该版本
- **未指定** → 读最新 tag，**补丁号 +1**（`0.4.16` → `0.4.17`，`2.5.1` → `2.5.2`）

`deploy/fnos/manifest` 的 `changelog` 写成这一版用户能看懂的一两句。`version` / `platform` 由打包脚本写入，不必改仓库里的占位值。

## 2. Branch, commit, tag, push, open PR

从干净/可提交的工作区出发（通常在 `master`）：

1. `git checkout -b release/<version>`（已在该分支则继续用）
2. `git add` 本版相关改动 + changelog（如有）
3. Commit（HEREDOC；说明 why；遵循仓库 commit 风格）
4. Annotated tag：`git tag -a v<version> -m "Roundpen <version>"`
5. `git push -u origin HEAD` 与 `git push origin v<version>`
6. `gh pr create` 打到 `master`（标题/正文用 HEREDOC；Summary + Test plan）

不要 `--force` 推 main/master；不要改 git config。

## 3. make fpk + install on NAS

```bash
VERSION=<version> make fpk
./scripts/fnos-install-nas.sh <version>
```

（tag 若尚未推到本地可见，必须显式 `VERSION=`；已有对应 tag 时可直接 `make fpk`。）

产物：`dist/fnos/roundpen-<version>-fnos-{x86,arm}.fpk`（NAS 装 x86）。

**为什么不用「纯升级」命令：** `appcenter-cli` 没有独立 upgrade；`install-fpk` 在应用已存在时只解包，真正切换版本要靠 `install-local`（内部 stop → uninstall → install）。卸装会清掉飞牛「可访问文件夹」（`app_authorization_path` / `app_user_authorization_path`），`@appdata` 仍保留。`fnos-install-nas.sh` 会在装前快照授权路径、装后写回新 `app_id`，并重启以重新注入 `TRIM_DATA_ACCESSIBLE_PATHS`。

装完确认：

- `sudo -n appcenter-cli list` 里 roundpen 是新版本且 `running`
- `curl -fsS http://127.0.0.1:9527/health` 为 `ok`
- `/vol1/@appdata/roundpen/data/secret.key` 仍在
- 若装前有授权目录：`TRIM_DATA_ACCESSIBLE_PATHS` 非空（或 DB 里 shared/user 路径仍在）

## 4. Switch back to master

```bash
git checkout master
git pull --ff-only origin master
```

若 PR 尚未合并，本地 `master` 暂时没有本版 commit 是正常的；回报 PR URL、tag、NAS 版本与 health 即可。
