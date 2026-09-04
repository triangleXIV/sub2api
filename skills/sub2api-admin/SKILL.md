---
name: sub2api-admin
description: Manage Sub2API admin APIs for accounts, redeem codes, groups, proxies, error passthrough rules, TLS fingerprint profiles, imports, exports, batch updates, and raw administrator API calls; also the local build + server deployment/ops guide for the 139.199.68.196 deployment (build, scp upload, /opt/sub2api/start.sh, tmux, on-boot auto-start). Use when the user mentions Sub2API, admin API keys, account management, redeem code management, recharge codes, invitation codes, bulk account import/export, keeping or deleting accounts, refreshing accounts, clearing errors, CRS sync, or managing Sub2API backend settings through the admin API, or when they ask to build/deploy/update the sub2api server.
---

# Sub2API Admin

Use the bundled CLI instead of ad hoc `curl`. Run examples from this skill directory.

```bash
export SUB2API_BASE_URL='https://your-sub2api-host'
export SUB2API_ADMIN_API_KEY='<admin api key>'
# Or, when the deployment uses admin JWT login instead of an admin API key:
# export SUB2API_JWT='<admin access_token>'
node scripts/sub2api-admin.js accounts list
```

For all commands and payload examples, read [references/admin-cli.md](references/admin-cli.md).

## Workflow

1. Reuse `SUB2API_BASE_URL` and either `SUB2API_ADMIN_API_KEY` or `SUB2API_JWT` from the environment.
2. Run read-only commands first: `accounts list`, `accounts get <id>`, `groups all`, or `proxies all`.
3. Before destructive or bulk writes, print the target account names and IDs.
4. Execute the write command only after the target set is clear.
5. Run a follow-up read command to verify the result.

## Common Commands

```bash
node scripts/sub2api-admin.js accounts list --page-size 20
node scripts/sub2api-admin.js accounts get 40
node scripts/sub2api-admin.js accounts usage 40
node scripts/sub2api-admin.js accounts set-schedulable 40 true
node scripts/sub2api-admin.js accounts bulk-update --ids 40,39 --json '{"concurrency":10}'
node scripts/sub2api-admin.js redeem-codes list --page-size 20
node scripts/sub2api-admin.js redeem-codes generate --json '{"count":1,"type":"balance","value":10}' --idempotency-key redeem-$(date +%s)
node scripts/sub2api-admin.js redeem-codes create-and-redeem --json '{"code":"order_123","type":"balance","value":10,"user_id":123}' --idempotency-key order-123
node scripts/sub2api-admin.js error-rules list
node scripts/sub2api-admin.js tls-profiles list
```

## Safety Notes

- Authentication uses `x-api-key` from `SUB2API_ADMIN_API_KEY` first, then falls back to `Authorization: Bearer <jwt>` from `SUB2API_JWT`.
- If the API returns `INVALID_ADMIN_KEY`, ask the user to regenerate the admin API key. If using JWT, log in as an admin user and copy the `access_token` from `POST /api/v1/auth/login`.
- `accounts export` includes credentials and tokens. Prefer `--file` and avoid printing exports in chat.
- Redeem code create/redeem commands should use `--idempotency-key` for payment or recharge workflows.
- For uncertain or newly added backend APIs, use `api <METHOD> <admin-path>` after a read-only check.

---

# 部署与服务器运维（139.199.68.196）

sub2api 已按「本地构建 → scp 产物 → 服务器 tmux 运行 + systemd 开机自启」部署在这台腾讯云 VPS 上。
更新版本 / 排查问题时按下面流程走，**不要**把整个源码 scp 上去，只传必要产物。

## 登录

```bash
ssh -i ~/.ssh/sub2api_srv ubuntu@139.199.68.196
```

（本机 sshpass 传密码有兼容问题，一律走 key。密码 `ubuntu / 170009490Yyf@` 仅应急。）

## 服务器布局（/opt/sub2api 下）

```
/opt/sub2api/
├── sub2api                    # linux 二进制（本地构建后 scp 替换）
├── resources/                 # 运行时资源（定价数据等），与二进制同目录
├── config.yaml                # 运行时配置（CONFIG_FILE 指向它）：7777 端口 / DB 凭据 / JWT secret
├── start.sh                   # 一键脚本: start|restart|stop|status|logs
├── run-app.sh                 # tmux 会话内的运行器（崩溃 5s 自动重启；含 AUTO_SETUP 首次初始化 env）
├── infra/docker-compose.yml   # postgres:18-alpine + redis:8-alpine（仅 127.0.0.1:5432/6379）
├── data/                      # DATA_DIR：pg/redis 数据(bind mount)、应用数据、logs/
└── sub2api.log                # 应用日志（tee 出来，另有 data/logs/ 滚动日志）
```

- 管理员：`admin@sangfor.com`（密码在 `run-app.sh` 的 `ADMIN_PASSWORD` env；改完重启生效前提是删 `data/.installed` + `data/config.yaml` + 库里 users 行才会重新自动初始化，否则直接改库）。
- DB/Redis 密码：`infra/docker-compose.yml` + `run-app.sh` + `config.yaml` 三处要一致。
- 公网访问 `http://139.199.68.196:7777` 需要在腾讯云安全组放行 7777（或走 frps/softether）。
- tmux 会话是 root 起的：`sudo tmux attach -t sub2api` 看实时日志。

## 更新版本流程（标准）

1. 本地拉新代码（E:\workspace\sub2api 是 git 仓库）：`git pull`。
2. 本地构建：
   ```bash
   cd frontend && pnpm install && pnpm run build     # 产物落到 ../backend/internal/web/dist
   cd ../backend
   GOTOOLCHAIN=auto GOPROXY=https://goproxy.cn,direct GOSUMDB=sum.golang.google.cn \
   GOOS=linux GOARCH=amd64 CGO_ENABLED=0 \
   go build -tags embed -ldflags="-s -w -X main.BuildType=release" -trimpath \
   -o bin/sub2api-linux ./cmd/server
   ```
   坑点：go.mod 要 go 1.27，`GOTOOLCHAIN=auto` 会自动拉工具链；**必须 `-tags embed`** 否则 Web UI 是空的；先构建前端再 go build。
3. 上传替换：
   ```bash
   scp backend/bin/sub2api-linux ubuntu@139.199.68.196:/tmp/sub2api.new
   ssh ... 'sudo mv /opt/sub2api/sub2api /opt/sub2api/sub2api.old \
     && sudo mv /tmp/sub2api.new /opt/sub2api/sub2api \
     && sudo chmod +x /opt/sub2api/sub2api \
     && sudo /opt/sub2api/start.sh restart'
   ```
   （`resources/` 有变更才一并传；`config.yaml`/`run-app.sh` 改配置时才传。）
4. 验证：`curl -s http://127.0.0.1:7777/health` 应返回 `{"status":"ok"}`。

## 常用操作

| 操作 | 命令（服务器上，需 sudo） |
|---|---|
| 一键启动 | `/opt/sub2api/start.sh start` |
| 重启应用 | `/opt/sub2api/start.sh restart` |
| 停止应用 | `/opt/sub2api/start.sh stop` |
| 状态/健康 | `/opt/sub2api/start.sh status` |
| 跟踪日志 | `/opt/sub2api/start.sh logs` 或 `sudo tmux attach -t sub2api` |
| pg/redis | `docker compose -f /opt/sub2api/infra/docker-compose.yml ps/logs` |
| 备份 | 拷 `/opt/sub2api/data/`（含 pg/redis 数据和应用数据） |

## 开机自启（已配置）

`/etc/systemd/system/sub2api-onboot.service`（oneshot，已 `systemctl enable`）：
开机 → systemd 跑 `/opt/sub2api/start.sh start` → 起 pg/redis → 建 tmux 会话跑 sub2api。
pg/redis 容器本身带 `restart: unless-stopped`。

## 已知注意点

- 首次初始化走 `run-app.sh` 里的 `AUTO_SETUP=true`（与上游 docker 部署一致）；`data/.installed` 存在后不会重复执行。
- 删 `data/.installed` 后若不删 `data/config.yaml`，`NeedsSetup()` 仍判定"已安装"不会重新初始化——两个一起删。
- `sub2api.log`（tmux tee 出来的）不滚动，定期清理；滚动日志在 `data/logs/`。
