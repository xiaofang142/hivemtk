package db

// 生产句柄的池上界必须真的落到 database/sql 句柄上（§23.8 登记的零覆盖面①，本轮补齐）。
//
// 为什么原先一片绿却等于没测：`InitDB` 里那四行 `sqlDB.Set*` 从头到尾没有用例经过——
// `InitDB` 要 DSN、要真库、还要 panic 路径，测试全部走 `testutil` 自己 open 的句柄，
// 于是"配置里的 200"与"句柄上真正生效的数"之间那段接线**从未被断言过**。
// 这段接线恰好是 CI 名额门（`scripts/check-ci-pg-capacity.py`）C5 那条下限的来源：
// 它拿 `DefaultPoolConfig.MaxOpenConns` 当"单个二进制可能同时持有的连接上界"。
// 如果这行 Set 被挪走或被 if 包住，配置文件仍是 200、门仍绿，真实句柄却是 Go 默认的 2——
// 表现是吞吐掉到看不见而不是报错，属于最难被发现的那类回归。
//
// 口径边界（免得读成"整条 InitDB 都测了"）：
//   - 句柄由 `sql.OpenDB` 配一枚**永不拨号**的 connector 得到，不连库、不读 env、不碰共享夹具；
//     所以这条腿在任何机器上都可跑，也永远不会因 PG 没起而假红。
//   - `sql.DB.Stats()` 只把 `MaxOpenConns` 暴露出来，因此"落到句柄"这一断言只对得上界成立；
//     idle／两个 lifetime 的**取值**由 `normalizePool` 的表测覆盖，不假装量到了句柄上。
//   - 兜底分支（配置留空时补默认）是 `normalizePool` 的全部职责，逐格点名补出来的数，
//     else 分支（配置齐全时原样穿过）单独一格——否则"整块被删"与"条件写反"都能靠半张表混过去。
//
// 牙齿（四刀逐格放过、且在本轮重建后的树上按同样的形状复跑过一遍；
// 放刀方式＝临时改 db.go 后复跑本文件，每刀改完立刻按 md5 比回基线字节）：
//   K1 摘掉 `SetMaxOpenConns` 那一行 ⇒ Stats() 读回 Go 默认的 0（不限），第一格红；
//   K2 把 `MaxOpenConns = 20` 兜底改成 0 ⇒ 第二格红且点名 0；
//   K3 把 `ConnMaxLifetime` 兜底秒数改成 60 ⇒ 第三格红；
//   K4 摘掉 `if poolConfig.MaxIdleConns == 0 { poolConfig = config.DefaultPoolConfig }` 整条
//     ⇒ 红在**第一格**「整条留空⇒采用生产默认表」（本轮实测：不是第四格——else 腿看的是
//     "配置齐全不许被覆盖"，兜底整条没了它照样原样穿过；拿期望推格子会点错名）。

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"testing"
	"time"

	"hivemtk-user/internal/config"
)

// noDialConnector 让 sql.OpenDB 给出一个真句柄而不需要真库：Connect 永远失败，
// 本文件里没有任何方法会去触发它（Stats/Set* 都不拨号）。
type noDialConnector struct{}

func (noDialConnector) Connect(context.Context) (driver.Conn, error) {
	return nil, errors.New("本文件只量池参数，不许拨号")
}

func (noDialConnector) Driver() driver.Driver { return noDialDriver{} }

type noDialDriver struct{}

func (noDialDriver) Open(string) (driver.Conn, error) { return nil, errors.New("同上") }

func TestPoolBoundLandsOnSQLHandle(t *testing.T) {
	sqlDB := sql.OpenDB(noDialConnector{})
	t.Cleanup(func() { _ = sqlDB.Close() })

	// 前置：没调 applyPool 之前句柄必须是 Go 的默认值。这里默认值是 **0＝不限连接**
	// （`database/sql` 只对 idle 侧默认 2，open 侧默认不限），实测于本工具链。
	// 不写这一句，下面的断言可能只是在测"句柄本来就报这个数"，而不是测那行 Set。
	// 顺带说清这条腿为什么值得存在：不限＝CI 那台 100 名额的容器最先撞的形态（§23.21 第 2 段）。
	if got := sqlDB.Stats().MaxOpenConnections; got != 0 {
		t.Fatalf("夹具前置不成立：未配置句柄的默认上界应为 0（不限），实读 %d", got)
	}

	applyPool(sqlDB, config.DefaultPoolConfig)

	if got, want := sqlDB.Stats().MaxOpenConnections, config.DefaultPoolConfig.MaxOpenConns; got != want {
		t.Errorf("配置写的上界 %d 没落到句柄上，实读 %d", want, got)
	}
}

func TestNormalizePoolFallbacks(t *testing.T) {
	cases := []struct {
		name  string
		in    config.PoolConfig
		want  config.PoolConfig
		where string
	}{
		{
			name:  "整条留空⇒采用生产默认表",
			in:    config.PoolConfig{},
			want:  config.DefaultPoolConfig,
			where: "MaxIdleConns==0 的兜底",
		},
		{
			name:  "只缺 MaxOpenConns⇒补 20",
			in:    config.PoolConfig{MaxIdleConns: 5, ConnMaxIdleTime: 60, ConnMaxLifetime: 900},
			want:  config.PoolConfig{MaxIdleConns: 5, MaxOpenConns: 20, ConnMaxIdleTime: 60, ConnMaxLifetime: 900},
			where: "MaxOpenConns==0 的兜底",
		},
		{
			name: "只缺 ConnMaxLifetime⇒补 30 分钟",
			in:   config.PoolConfig{MaxIdleConns: 5, MaxOpenConns: 7, ConnMaxIdleTime: 60},
			want: config.PoolConfig{MaxIdleConns: 5, MaxOpenConns: 7, ConnMaxIdleTime: 60,
				ConnMaxLifetime: int((30 * time.Minute).Seconds())},
			where: "ConnMaxLifetime==0 的兜底",
		},
		{
			name:  "四项齐全⇒原样穿过（else 腿）",
			in:    config.PoolConfig{MaxIdleConns: 11, MaxOpenConns: 22, ConnMaxIdleTime: 33, ConnMaxLifetime: 44},
			want:  config.PoolConfig{MaxIdleConns: 11, MaxOpenConns: 22, ConnMaxIdleTime: 33, ConnMaxLifetime: 44},
			where: "配置齐全时不许被默认表覆盖",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := normalizePool(tc.in)
			if got != tc.want {
				t.Errorf("%s：期望 %+v，实得 %+v", tc.where, tc.want, got)
			}
		})
	}
}
