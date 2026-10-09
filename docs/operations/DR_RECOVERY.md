# HiveMtk 灾难恢复指南

> 单商户本地部署的备份与恢复方案。

---

## 1. 备份策略

### 1.1 备份频率

| 类型 | 频率 | 保留期 | 说明 |
|------|------|--------|------|
| 全量备份 | 每日 02:00 | 7 天 | `pg_dump -Fc` 全库 + Redis `/data` 目录 + uploads + 配置 |
| 增量备份 | 未实现 | - | 见下方「当前未落地的能力」 |
| WAL 归档 | 未实现 | - | 见下方「当前未落地的能力」 |

**当前未落地的能力**（本文旧版把它写成了既有机制，此处按实测口径改正）：
`docker-compose.yml` 的 `mtk-postgres` 只设了 `shared_preload_libraries / max_connections /
shared_buffers / effective_cache_size / work_mem / maintenance_work_mem / port`，
未设 `archive_command`；实测 `SHOW archive_mode` 返回 `off`（`wal_level` 为 `replica`）。
⇒ 没有 WAL 归档，也就没有基于 WAL 的连续增量恢复（PITR）。
要启用需自行加 `wal_level=replica` + `archive_mode=on` + `archive_command` 并重建容器，
本仓当前不提供该配置。全量 `pg_dump` 是唯一可用的恢复点。

### 1.2 备份内容

数据层只有两个 Docker 卷（`docker volume ls` 现测：`mtk_user_pg_data`、`mtk_user_redis_data`），
其余产物都在宿主机目录，按目录直接打包。

| 组件 | 备份方式 | 真实来源 | 存储位置 |
|------|---------|---------|---------|
| PostgreSQL（库名 `user_db`） | `pg_dump -Fc` | 容器 `mtk-postgres`（`pgvector/pgvector:pg15`，端口 8202） | `/var/backups/hivemtk/` |
| Redis | 整目录 tar（AOF + RDB） | 容器 `mtk-redis` 的 `/data`（卷 `mtk_user_redis_data`） | `/var/backups/hivemtk/` |
| 上传文件 | tar + gzip | `user-server/uploads/`（`STORAGE_LOCAL_BASE_DIR`，默认 `./uploads`） | `/var/backups/hivemtk/` |
| 应用日志 | tar + gzip（可选） | `user-server/logs/user-server.log`（`config.yaml` `logging.file`） | `/var/backups/hivemtk/` |
| 配置文件 | 直接复制 | `.env` + `docker-compose.yml` | `/var/backups/hivemtk/config/` |

> Redis 必须整目录备份，不能只备 `dump.rdb`：`mtk-redis` 的启动参数带 `--appendonly yes`，
> 实测在保留 `appendonlydir/` 的情况下覆盖 `dump.rdb` 并重启，Redis 仍从 AOF 装载，
> RDB 里的键不会回来。

> 应用内另有一套 `POST /api/backups`（`internal/service/backup.go`，落盘目录由
> `BACKUP_BASE_DIR` 决定，默认 `./backups`），但它导出的是一份 `data.json`
> （线索 / 短链 / 用户的业务对象快照，见 `exportData` 与 `backupDatabase`），
> **不是数据库备份**，不能当灾难恢复的恢复点，本节说的备份仍以 `pg_dump` 为准。

### 1.3 备份脚本

```bash
#!/bin/bash
# /opt/hivemtk/scripts/backup.sh

set -euo pipefail

# docker compose 需要在仓库根运行才能自动读到 .env；脚本自己也要把 .env 导出到 shell
# 环境，后面的 ${POSTGRES_USER} / ${USER_DB_NAME} / ${REDIS_PASSWORD} 才有值
cd /opt/hivemtk
set -a
. ./.env
set +a

BACKUP_ROOT="/var/backups/hivemtk"
DATE=$(date +%Y%m%d_%H%M%S)
BACKUP_DIR="$BACKUP_ROOT/$DATE"

echo "[$(date)] Starting backup to $BACKUP_DIR"

mkdir -p "$BACKUP_DIR"

# PostgreSQL 全量备份
# 服务名是 mtk-postgres（不是 postgres），角色取 .env 的 POSTGRES_USER，库名取 USER_DB_NAME。
# pg_dump 在容器内跑，同时保证客户端与服务端同为 PG 15 —— 宿主机 Homebrew 的
# pg_dump 16.14 产出的 -Fc 文件，容器里的 pg_restore 15.18 读不了
# （实测报 "pg_restore: error: unsupported version (1.15) in file header"）。
echo "Backing up PostgreSQL..."
docker compose exec -T mtk-postgres \
  pg_dump -U "${POSTGRES_USER:-admin}" -h 127.0.0.1 -p 8202 -d "${USER_DB_NAME:-user_db}" -Fc \
  > "$BACKUP_DIR/postgres.dump"

# Redis 备份：整 /data 目录（appendonlydir + dump.rdb），AOF 开着时只拷 RDB 会丢数据
echo "Backing up Redis..."
docker compose exec -T mtk-redis \
  redis-cli -p 8203 -a "${REDIS_PASSWORD:?REDIS_PASSWORD must be set in .env}" \
  --no-auth-warning BGSAVE
docker compose cp mtk-redis:/data "$BACKUP_DIR/redis_data"
tar czf "$BACKUP_DIR/redis_data.tar.gz" -C "$BACKUP_DIR" redis_data
rm -rf "$BACKUP_DIR/redis_data"

# 上传文件备份（user-server 的本地存储根，默认 ./uploads，可用 STORAGE_LOCAL_BASE_DIR 覆盖）
echo "Backing up uploads..."
tar czf "$BACKUP_DIR/uploads.tar.gz" -C /opt/hivemtk/user-server uploads 2>/dev/null || true

# 配置文件备份
echo "Backing up config..."
cp /opt/hivemtk/.env "$BACKUP_DIR/"
cp /opt/hivemtk/docker-compose.yml "$BACKUP_DIR/"

# 生成校验
md5sum "$BACKUP_DIR"/* > "$BACKUP_DIR/checksums.md5"

# 清理超过 7 天的备份
find "$BACKUP_ROOT" -maxdepth 1 -type d -mtime +7 -exec rm -rf {} +

echo "[$(date)] Backup completed: $BACKUP_DIR"
echo "[$(date)] Backup size: $(du -sh "$BACKUP_DIR" | cut -f1)"
```

> 仓库根 Makefile 另有 `db-backup` / `db-restore` 两个目标，但**现测不可用**：
> `hivemtk/Makefile:122-134` 的 `pg_dump -U $${POSTGRES_USER:-admin}` /
> `psql -U ... -d ...` 都没带 `-p 8202`，而容器里 PostgreSQL 被显式改成了 8202 监听，
> 直接跑报 `connection to server on socket "/var/run/postgresql/.s.PGSQL.5432" failed`。
> 上面脚本里带全 `-h 127.0.0.1 -p 8202` 的写法是实测通过的版本。


### 1.4 定时任务

```cron
# /etc/cron.d/hivemtk-backup
0 2 * * * root /opt/hivemtk/scripts/backup.sh >> /var/log/hivemtk-backup.log 2>&1
```

## 2. 恢复流程

### 2.1 恢复步骤

```bash
cd /opt/hivemtk
set -a
. ./.env
set +a

# 0. 停应用写入
#    基线形态（docker-compose.yml）里 compose 只有 mtk-postgres / mtk-redis，
#    user-server 是宿主机进程，所以停它不能用 docker compose stop。
#    二进制由 make user-build 产出到 user-server/bin/user-server。
#    但本机现在跑的是开发态叠加（docker-compose.dev.yml 里的 user-server-dev，
#    现测 container_name=mtk-user-server-dev），这种形态下 pkill 打不到它，
#    要按叠加文件起停：
#      docker compose -f docker-compose.yml -f docker-compose.dev.yml stop user-server-dev
#    systemd/supervisor 场景改用 systemctl stop hivemtk-user。
pkill -f 'user-server/bin/user-server' || true

# 1. 选择要恢复的备份
BACKUP_DATE="20260815_020000"  # 改为实际日期
BACKUP_DIR="/var/backups/hivemtk/$BACKUP_DATE"

# 2. 起数据层（必须在恢复之前起；旧写法先 down 再 exec，容器已经停了必然失败）
docker compose up -d
until docker compose exec -T mtk-postgres \
        pg_isready -U "${POSTGRES_USER:-admin}" -h 127.0.0.1 -p 8202 -q; do
  sleep 2
done

# 3. 恢复 PostgreSQL：先重建空库，否则 pg_restore 会撞上一堆 already exists
docker compose exec -T mtk-postgres psql \
  -U "${POSTGRES_USER:-admin}" -d postgres -h 127.0.0.1 -p 8202 \
  -c "SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = '${USER_DB_NAME:-user_db}' AND pid <> pg_backend_pid();"
docker compose exec -T mtk-postgres psql \
  -U "${POSTGRES_USER:-admin}" -d postgres -h 127.0.0.1 -p 8202 \
  -c "DROP DATABASE IF EXISTS ${USER_DB_NAME:-user_db};" \
  -c "CREATE DATABASE ${USER_DB_NAME:-user_db};"

#    走 stdin：备份文件在宿主机，容器内的 pg_restore 读不到宿主机的绝对路径
#    （实测直接传路径报 could not open input file）
echo "Restoring PostgreSQL from $BACKUP_DATE..."
cat "$BACKUP_DIR/postgres.dump" | docker compose exec -T mtk-postgres \
  pg_restore -U "${POSTGRES_USER:-admin}" -h 127.0.0.1 -p 8202 \
  -d "${USER_DB_NAME:-user_db}" --no-owner --no-privileges

# 4. 恢复 Redis：整 /data 目录还原，AOF 目录必须一起处理
#    只 cp dump.rdb 是不够的 —— 实测保留 appendonlydir/ 时 Redis 从 AOF 装载，RDB 被忽略
echo "Restoring Redis..."
docker compose stop mtk-redis
docker run --rm \
  -v mtk_user_redis_data:/data \
  -v "$BACKUP_DIR":/backup \
  alpine:3 sh -c 'rm -rf /data/appendonlydir /data/dump.rdb /data/appendonlydir/* && \
                   tar xzf /backup/redis_data.tar.gz -C /data --strip-components=1'
docker compose up -d mtk-redis

# 5. 恢复上传文件（备份时是 -C user-server uploads，解回同一层目录）
echo "Restoring uploads..."
tar xzf "$BACKUP_DIR/uploads.tar.gz" -C /opt/hivemtk/user-server

# 6. 启动应用（宿主机进程；docker compose up -d 里没有后端）
echo "Starting services..."
cd /opt/hivemtk/user-server && nohup ./bin/user-server >> logs/user-server.log 2>&1 &
cd /opt/hivemtk

# 7. 验证恢复
echo "Verifying restoration..."
until curl -fsS http://127.0.0.1:8204/healthz >/dev/null 2>&1; do sleep 2; done
curl -s http://127.0.0.1:8204/healthz
docker compose exec -T mtk-postgres \
  pg_isready -U "${POSTGRES_USER:-admin}" -h 127.0.0.1 -p 8202
docker compose exec -T mtk-redis redis-cli -p 8203 -a "${REDIS_PASSWORD}" \
  --no-auth-warning ping

echo "Restoration completed from $BACKUP_DATE"
```

### 2.2 恢复验证

```bash
cd /opt/hivemtk
set -a
. ./.env
set +a

# 检查数据库完整性
# 表名以 GORM TableName() 为准：没有 users 表（实测 ERROR: relation "users" does not exist），
# 账号表是 system_users
docker compose exec -T mtk-postgres psql \
  -U "${POSTGRES_USER:-admin}" -d "${USER_DB_NAME:-user_db}" -h 127.0.0.1 -p 8202 \
  -c "SELECT count(*) FROM system_users;"

# 检查功能完整性
# /healthz 只回存活（{"data":{"status":"alive",...}}），不看依赖；
# 带依赖详情的是 /health（database / redis / inference / embedding），人工巡检看这个。
# 注意：本仓路由前缀是 /api/<domain>，不存在 /api/v1/*，也不存在 teams 路由
curl -s http://127.0.0.1:8204/health | python3 -m json.tool

# 检查日志（config.yaml logging.file = logs/user-server.log，相对 user-server 工作目录；
# 旧写法 /var/log/hivemtk/app.log 在本仓没有任何产出点）
tail -n 50 /opt/hivemtk/user-server/logs/user-server.log
```

> `pg_restore` 恢复到空库时，若备份来自装过 `pg_jieba` / 自建 `jiebacfg` 文本搜索配置的实例
> （`pgvector/pgvector:pg15` 镜像**不带**该扩展，且本仓没有任何安装它的地方：
> `grep -rniI "pg_jieba\|jiebacfg" . --exclude-dir=.git` 在代码/迁移/脚本里零命中，
> 只有一句宣传文案提到它，配置是当年在容器里手工编译的），
> 失败的不止是索引语句，而是**整张 `knowledge_chunks` 表连同它的数据**。
>
> 实测本机（备份= 351 张 public 表 / `knowledge_chunks` 372 行）恢复到干净空库的结果：
> `pg_restore` 共 19 条语句失败（stderr 108 行），链条是
> `CREATE EXTENSION pg_jieba` 失败 ⇒ `jiebacfg` 不存在 ⇒
> `CREATE TABLE public.knowledge_chunks` 失败（它的 STORED 生成列 `content_tsv_jieba` 引用
> `jiebacfg`，所以是建表本身过不去）⇒ 这张表的序列、约束、**`COPY`（372 行）**、
> 10 条索引、触发器全部连带失败。恢复后 public 表 = 350 张，缺的就是 `knowledge_chunks`。
> 其余表不受影响（`system_users` 12 行完整回来）。
>
> ⇒ 两件事必须做对：
> 1. **恢复前**在目标实例上装好 `pg_jieba` 并建出 `jiebacfg`（本仓不提供该配置，需自行编译安装，
>    参考容器内 `apt-get install build-essential cmake` + pg_jieba 源码，装完重启容器）；
>    否则宁可把这次恢复判为失败，也不要「恢复完再补」——业务读知识分片的接口会直接报表不存在。
> 2. **恢复后**不能只看表数下界。§4.1 的校验已改成「备份库与恢复库的表名集合做差」，
>    就是为这一条服务的（旧的 `-gt 300` 断言在本机实测下 350 张照样放行，是一张表的丢失读成绿）。
>
> 核对报错条目时同理：除分词扩展与 `knowledge_chunks` 一族之外还有别的对象失败，视为恢复失败。


## 3. 故障场景处理

### 3.1 主机故障

```
1. 在新主机上安装依赖
2. 克隆项目代码
3. 复制备份文件
4. 执行恢复流程
5. 启动服务
6. 验证功能
```

步骤 1-2 与 5 的具体命令不在本文范围，走
[DEPLOYMENT_GUIDE.md](../DEPLOYMENT_GUIDE.md)（依赖、构建、推理栈）与
[MERCHANT_DEPLOYMENT.md](MERCHANT_DEPLOYMENT.md)（商户侧初始化）；
第 4 步用本文 §2.1。
注意新主机要装与容器同大版本的 PostgreSQL 客户端工具（本机实测：宿主机 Homebrew
`pg_dump` 16.14 产出的 `-Fc` 文件，容器内 `pg_restore` 15.18 读不了；
备份统一在容器内跑就没这个问题）。

### 3.2 数据误删

没有 WAL 归档（见 §1.1），所以只能走「最近一次全量备份 → 临时库 → 回捞」这条路，
恢复点粒度 = 上一次 `pg_dump` 的时间。

```bash
cd /opt/hivemtk && set -a && . ./.env && set +a
LATEST=$(ls -td /var/backups/hivemtk/*/ | head -1)

# 1. 停应用写入（宿主机进程，不是容器）
pkill -f 'user-server/bin/user-server' || true

# 2. 从最近备份恢复到一个临时库（不碰生产库）
docker compose exec -T mtk-postgres psql \
  -U "${POSTGRES_USER:-admin}" -d postgres -h 127.0.0.1 -p 8202 \
  -c "CREATE DATABASE user_db_recovery;"
cat "$LATEST/postgres.dump" | docker compose exec -T mtk-postgres \
  pg_restore -U "${POSTGRES_USER:-admin}" -h 127.0.0.1 -p 8202 \
  -d user_db_recovery --no-owner --no-privileges

# 3. 按主键把丢的行捞回生产库（示例表 system_users，主键 id；表名以 GORM TableName() 为准）
#    同一 PG 实例的两个库之间不能直接 JOIN，走「导出 → 生产库暂存表 → 按主键补缺」，
#    全程不覆盖生产库已有的行。以下三段均在临时库上实测通过。
docker compose exec -T mtk-postgres psql -U "${POSTGRES_USER:-admin}" \
  -d user_db_recovery -h 127.0.0.1 -p 8202 \
  -c "COPY system_users TO STDOUT" > /tmp/recovered_system_users.tsv

docker compose exec -T mtk-postgres psql -U "${POSTGRES_USER:-admin}" \
  -d "${USER_DB_NAME:-user_db}" -h 127.0.0.1 -p 8202 \
  -c "CREATE TABLE system_users_staging (LIKE system_users INCLUDING DEFAULTS);"

docker compose exec -T mtk-postgres psql -U "${POSTGRES_USER:-admin}" \
  -d "${USER_DB_NAME:-user_db}" -h 127.0.0.1 -p 8202 \
  -c "COPY system_users_staging FROM STDIN" < /tmp/recovered_system_users.tsv

# 只补生产库里不存在的 id；跑完人工抽查再删暂存表
docker compose exec -T mtk-postgres psql -U "${POSTGRES_USER:-admin}" \
  -d "${USER_DB_NAME:-user_db}" -h 127.0.0.1 -p 8202 \
  -c "INSERT INTO system_users SELECT * FROM system_users_staging s
        WHERE NOT EXISTS (SELECT 1 FROM system_users t WHERE t.id = s.id);"

# 4. 验证后拆掉临时库
docker compose exec -T mtk-postgres psql \
  -U "${POSTGRES_USER:-admin}" -d postgres -h 127.0.0.1 -p 8202 \
  -c "DROP DATABASE user_db_recovery;"
```

### 3.3 数据库损坏

`pg_resetwal` 会丢掉 WAL 尾部未提交的事务，属最后手段；跑之前必须先停容器并**备份整个
`mtk_user_pg_data` 卷**，否则一次修坏就再无退路。容器内 `PGDATA=/var/lib/postgresql/data`
（实测 `SHOW data_directory;` 同值）。

```bash
cd /opt/hivemtk && set -a && . ./.env && set +a

# 1. 停服务 + 备份数据卷（宿主机留一份原始卷内容）
pkill -f 'user-server/bin/user-server' || true
docker compose stop mtk-postgres
docker run --rm -v mtk_user_pg_data:/from -v /var/backups/hivemtk:/to \
  alpine:3 tar czf /to/pg_data_emergency_$(date +%Y%m%d_%H%M%S).tar.gz -C /from .

# 2. 修 WAL：postgres 进程停了就没有容器可以 exec（docker exec 要求容器在跑），
#    所以用同一个镜像起一次性容器挂同一卷跑 pg_resetwal；
#    必须 -u postgres —— 以 root 跑 initdb/pg_resetwal 会被拒（实测 cannot be run as root）
docker run --rm \
  -u postgres \
  -v mtk_user_pg_data:/var/lib/postgresql/data \
  --entrypoint pg_resetwal \
  pgvector/pgvector:pg15 -D /var/lib/postgresql/data --force

# 3. 起 PG，验证能否打开
docker compose up -d mtk-postgres
docker compose exec -T mtk-postgres \
  pg_isready -U "${POSTGRES_USER:-admin}" -h 127.0.0.1 -p 8202

# 4. 能起 → 立刻 pg_dump 一份干净导出；起不来 → 直接走 §2.1 从备份恢复
docker compose exec -T mtk-postgres pg_dump -U "${POSTGRES_USER:-admin}" \
  -h 127.0.0.1 -p 8202 -d "${USER_DB_NAME:-user_db}" -Fc \
  > /var/backups/hivemtk/after_resetwal_$(date +%Y%m%d_%H%M%S).dump
```

## 4. 备份验证

### 4.1 每周验证

```bash
#!/bin/bash
# /opt/hivemtk/scripts/verify-backup.sh
# 每周执行一次

set -euo pipefail
cd /opt/hivemtk
set -a
. ./.env
set +a

BACKUP_ROOT="/var/backups/hivemtk"
VERIFY_DB=user_db_verify
LATEST=$(ls -td "$BACKUP_ROOT"/*/ | head -1)

echo "Verifying backup: $LATEST"

# 起数据层（服务名 mtk-postgres，不是 postgres）
docker compose up -d mtk-postgres

# 等待 PostgreSQL 就绪，而不是猜一个固定 sleep
until docker compose exec -T mtk-postgres \
        pg_isready -U "${POSTGRES_USER:-admin}" -h 127.0.0.1 -p 8202 -q; do
  sleep 2
done

PG="docker compose exec -T mtk-postgres psql -U ${POSTGRES_USER:-admin} -h 127.0.0.1 -p 8202"

# pg_restore 不会自己建库 —— 旧写法连 hivemtk_clean 都没创建过，必然失败
$PG -d postgres -q -c "DROP DATABASE IF EXISTS $VERIFY_DB;" \
            -c "CREATE DATABASE $VERIFY_DB;"

# 尝试恢复：走 stdin。
# 不要用 -e / --exit-on-error —— 本机 user_db 带 pg_jieba 相关对象（镜像不带该扩展），
# 加了 -e 会在第一条 jieba 错误就中止，库只恢复出几十张表；
# 默认 continue-on-error + 下面的表数/行数断言才是可用的校验方式。
cat "$LATEST/postgres.dump" | docker compose exec -T mtk-postgres \
  pg_restore -U "${POSTGRES_USER:-admin}" -h 127.0.0.1 -p 8202 \
  -d "$VERIFY_DB" --no-owner --no-privileges

# 检查数据量（没有 users 表，账号表是 system_users）
COUNT=$($PG -d "$VERIFY_DB" -tA -c "SELECT count(*) FROM system_users;")
TABLES=$($PG -d "$VERIFY_DB" -tA \
  -c "SELECT count(*) FROM information_schema.tables WHERE table_schema='public';")

echo "system_users count after restore: $COUNT (public tables: $TABLES)"

# 关键一步：拿「备份来源库」和「恢复出来的库」的表名集合做差。
# 只看表数下界会把整张表没建起来读成绿 —— 本机实测缺 pg_jieba 时恢复出 350 张、
# 生产库 351 张，少的正是 knowledge_chunks（连 372 行一起丢），
# 而 -gt 300 这种断言照样放行。表名集合做差才有牙。
tables_of() {
  $PG -d "$1" -tA \
    -c "SELECT table_name FROM information_schema.tables WHERE table_schema='public' ORDER BY 1;" \
    | grep .
}
MISSING=$(comm -23 <(tables_of "${USER_DB_NAME:-user_db}") <(tables_of "$VERIFY_DB") | paste -sd, -)
if [ -n "$MISSING" ]; then
  echo "❌ 恢复后缺少这些表：$MISSING"
  echo "   先看是不是分词扩展（pg_jieba / jiebacfg）没装 —— 见 §2.2 的连带失败链条"
fi

# 清理
$PG -d postgres -q -c "DROP DATABASE IF EXISTS $VERIFY_DB;"

# 实测基线（2026-10-09，本机 user_db → 空库）：350 张 public 表 + system_users 12 行；
# 目标库装了 pg_jieba 时应当与生产库同为 351 张。
# 判据是「没有缺表」而不是「表够多」：TABLES 只作为下限兜底。
if [ "$COUNT" -gt "0" ] && [ "$TABLES" -gt "300" ] && [ -z "$MISSING" ]; then
  echo "✅ Backup verification PASSED"
else
  echo "❌ Backup verification FAILED"
  exit 1
fi

echo "[$(date)] Backup verification completed"
```

### 4.2 定时任务

```cron
# 每周日 03:00 执行
0 3 * * 0 root /opt/hivemtk/scripts/verify-backup.sh >> /var/log/hivemtk-verify.log 2>&1
```

---

*最后更新: 2026-10-09（按代码与本机 8202 上的 user_db 逐条重跑校正命令、服务名、端口、卷名与表名）*
