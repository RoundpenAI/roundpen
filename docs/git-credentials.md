# Git personal tokens

统一用 **Personal Access Token / OAuth token**（不用 SSH 钥匙）。同一条 token 既给 `git clone` / `push`，
也给 `tea` / `gh` / `glab` 管 issue、PR。

```
用户填写 PAT（Git 个人令牌）        ┐
                                    ├→ 按 host 合并（PAT 优先）→ 注入该用户的 guest $HOME
GitHub / Gitea OAuth 登录（关联账号）┘
```

不把 token 打进镜像，也不从宿主机 `~/.ssh` 拷贝。

## 与联合登录的关系

OAuth 登录（见 [auth.md](auth.md)）拿到的 token 存在 `user_identities`，注入时与手填 PAT 按 **host**
合并（`gitcred.MergeCreds`）：

- **同一 host 上手填 PAT 优先**——用户显式配置的覆盖登录身份；解绑 OAuth 不会动 PAT。
- 其余 host 用 OAuth token（用户名取该 provider 默认：GitHub `x-access-token`、Gitea `git`）。
- Gitea 的 access token 默认 1 小时过期（`REFRESH_TOKEN_EXPIRATION_TIME` 控制 refresh token）：
  注入时若 5 分钟内过期会先刷新；另有后台任务每 10 分钟刷新 30 分钟内过期的身份，并重跑运行中
  Agent 沙箱的注入脚本。刷新失败沿用旧 token（git 会明确报认证失败），此时可改用 PAT。

## 用户

Settings → **Git personal tokens**（所有登录用户）：

| 字段 | 说明 |
|------|------|
| Provider | `gitea` / `github` / `gitlab` / `generic` |
| Host | 如 `git.eaxi.com`、`github.com` |
| Username | Gitea 建议填账号名。GitHub 默认 `x-access-token`，GitLab 默认 `oauth2`，Gitea 默认 `git` |
| Token | PAT / access token（GET 时脱敏） |

同一用户同一 host 只会保留一条。删除后下次 ensure 会清掉 guest 里的 git 文件。

API：`GET/PUT /v1/me/git-credentials`，`DELETE /v1/me/git-credentials/{id}`。

## 注入（guest $HOME，不是 workspace，更不是镜像）

`EnsureAgent` 通过 **guest exec** 写入（不在宿主机上打开 workspace 内部文件）。git 文件刻意放 guest home：**`/workspace` 项目树里绝不能出现用户自己的凭据**。

| Guest 路径 | 作用 |
|------------|------|
| `/home/roundpen/.roundpen/git/credentials` | `git credential store`（HTTPS user:token@host） |
| `/home/roundpen/.roundpen/git/config` | `credential.helper` + `url.insteadOf`（`git@host:` → `https://host/`） |
| `/home/roundpen/.roundpen/git/env` | `GH_TOKEN` / `GITEA_TOKEN` / `TEA_TOKEN` / `GITLAB_TOKEN`，给以后的 `gh` / `tea` / `glab` |
| `/home/roundpen/.gitconfig` | include 上面的 config，所以 SSH / exec 进来的 `git` 自动用 PAT |

旧版落在 `/workspace/.roundpen/git` 的文件在注入时删除；`/workspace/.roundpen/ssh`（自动拷贝的旧 SSH 钥匙）同样删除。

Agent workspace 是 per-user 的 virtio 数据盘（`workspace.qcow2`），guest 挂到 `/workspace` 且属主是 `roundpen`。宿主机不 bind、不 9p、不 `chmod 0777`——**正因为 `/workspace` 会被项目源码混在一起，凭据一律不进它，只进 `$HOME/.roundpen`**。

## 不做什么

- 不读取、不打包开发机 `id_ed25519`
- 不把 token 写进 OCI / qcow2
- 不把 token 写进 `git config insteadOf` URL（只放 credential store）
