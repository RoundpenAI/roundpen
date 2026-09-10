# Git personal tokens

统一用 **Personal Access Token**（不用 SSH 钥匙）。同一条 token 既给 `git clone` / `push`，也给后续 `tea` / `gh` / `glab` 管 issue、PR。

```
用户填写 PAT  →  平台按用户存 PostgreSQL  →  EnsureAgent 注入该用户 workspace
```

不把 token 打进镜像，也不从宿主机 `~/.ssh` 拷贝。

## 用户

Settings → **Git personal tokens**（所有登录用户）：

| 字段 | 说明 |
|------|------|
| Provider | `gitea` / `github` / `gitlab` / `generic` |
| Host | 如 `git.eaxi.com`、`github.com` |
| Username | Gitea 建议填账号名。GitHub 默认 `x-access-token`，GitLab 默认 `oauth2`，Gitea 默认 `git` |
| Token | PAT / access token（GET 时脱敏） |

同一用户同一 host 只会保留一条。删除后下次 ensure 会清掉 workspace 里的 git 文件。

API：`GET/PUT /v1/me/git-credentials`，`DELETE /v1/me/git-credentials/{id}`。

## 注入（workspace，不是镜像）

`EnsureAgent` 通过 **guest SSH exec** 写入（不在宿主机上打开 workspace 内部文件）：

| Guest 路径 | 作用 |
|------------|------|
| `/workspace/.roundpen/git/credentials` | `git credential store`（HTTPS user:token@host） |
| `/workspace/.roundpen/git/config` | `credential.helper` + `url.insteadOf`（`git@host:` → `https://host/`） |
| `/workspace/.roundpen/git/env` | `GH_TOKEN` / `GITEA_TOKEN` / `TEA_TOKEN` / `GITLAB_TOKEN`，给以后的 `gh` / `tea` / `glab` |
| `/home/roundpen/.gitconfig` | include 上面的 config，所以 SSH 进来的 `git` 自动用 PAT |

Agent workspace 是 per-user 的 virtio 数据盘（`workspace.qcow2`），guest 挂到 `/workspace` 且属主是 `roundpen`。宿主机不 bind、不 9p、不 `chmod 0777`。

旧的自动拷贝路径 `/workspace/.roundpen/ssh` 会在注入时删除。

## 不做什么

- 不读取、不打包开发机 `id_ed25519`
- 不把 token 写进 OCI / qcow2
- 不把 token 写进 `git config insteadOf` URL（只放 credential store）
