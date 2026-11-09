package testutil

import (
	"errors"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5/pgconn"
)

type testDBConfig struct {
	dsn      string
	host     string
	port     string
	database string
}

// readTestDBConfig 必须在 TCP 探测、咨询锁和任何 DDL 之前运行。
// 这里只允许测试基名；槽位/PID 名只能由已验证的基名在内部生成。
func readTestDBConfig() (testDBConfig, error) {
	if name := os.Getenv("POSTGRES_TEST_DBNAME"); os.Getenv("POSTGRES_TEST_DSN") == "" && name != "" && name != "user_db_test" {
		return testDBConfig{}, errors.New("POSTGRES_TEST_DBNAME must be user_db_test")
	}
	// service 文件会把显式配置重定向到未预检的实例。
	if os.Getenv("PGSERVICE") != "" || os.Getenv("PGSERVICEFILE") != "" {
		return testDBConfig{}, errors.New("PGSERVICE/PGSERVICEFILE are not supported by the test entry")
	}
	// 口令缺失在组 DSN 之前拦截：缺配置快速失败，不让"没配口令"流成
	// "用占位口令断连"的混淆报错。
	//
	// 仅限未显式给 DSN 的路径。POSTGRES_TEST_DSN 是"完整连接串，显式覆盖
	// 一切"（见 testdb.go 顶部），URL 式 DSN 可自带 userinfo 口令；此时
	// 环境口令为空是合法配置，前置门跳过，由下面 DSN 解析后的 cfg.Password
	// 检查兜底（缺口令仍报同一句"password is missing"）。F-4-1（2026-11-09）。
	if os.Getenv("POSTGRES_TEST_DSN") == "" &&
		os.Getenv("POSTGRES_TEST_PASSWORD") == "" && os.Getenv("POSTGRES_PASSWORD") == "" {
		return testDBConfig{}, errors.New("test password is missing; set POSTGRES_TEST_PASSWORD or POSTGRES_PASSWORD")
	}
	fields, err := testDSNFields(getTestDSN(testDBPortCandidates()[0]))
	if err != nil {
		return testDBConfig{}, err
	}
	if fields["dbname"] != "user_db_test" {
		return testDBConfig{}, errors.New("test DSN database must be user_db_test")
	}
	if fields["password"] == "" {
		return testDBConfig{}, errors.New("test password is missing; set POSTGRES_TEST_PASSWORD or POSTGRES_PASSWORD")
	}
	if fields["port"] == "0" {
		return testDBConfig{}, errors.New("test entry requires one TCP host and a nonzero port")
	}
	if fields["passfile"] != "" {
		return testDBConfig{}, errors.New("external password files are not allowed by the test entry")
	}
	// 显式填齐身份字段，阻止 PG* 或 .pgpass 把测试配置改到另一个实例。
	for key, fallback := range map[string]string{"host": "127.0.0.1", "port": "8217", "user": "admin", "sslmode": "disable", "TimeZone": "Asia/Shanghai", "connect_timeout": "2", "passfile": ""} {
		if _, ok := fields[key]; !ok {
			fields[key] = fallback
		}
	}
	keys := make([]string, 0, len(fields))
	for key := range fields {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		if key == "TimeZone" {
			// GORM 的 postgres Dialector 另用正则读取时区，不解析引号。
			// 只允许可直接表示的时区名，避免它把引号传给 PostgreSQL。
			if fields[key] == "" || strings.ContainsAny(fields[key], " \t\r\n'\\&") {
				return testDBConfig{}, errors.New("invalid test timezone (details redacted)")
			}
			parts = append(parts, key+"="+fields[key])
			continue
		}
		parts = append(parts, key+"="+testDSNQuote(fields[key]))
	}
	dsn := strings.Join(parts, " ")
	cfg, err := pgconn.ParseConfigWithOptions(dsn, pgconn.ParseConfigOptions{
		ConnStringAllowedKeys: []string{"host", "port", "user", "password", "dbname", "sslmode", "sslrootcert", "sslcert", "sslkey", "connect_timeout", "TimeZone", "application_name", "passfile"},
	})
	if err != nil {
		return testDBConfig{}, errors.New("invalid POSTGRES_TEST_DSN or connection fields (details redacted)")
	}
	if cfg.Database != "user_db_test" {
		return testDBConfig{}, errors.New("test DSN database must be user_db_test")
	}
	if cfg.Password == "" {
		return testDBConfig{}, errors.New("test password is missing; set POSTGRES_TEST_PASSWORD or POSTGRES_PASSWORD")
	}
	if cfg.Host == "" || strings.HasPrefix(cfg.Host, "/") || strings.Contains(cfg.Host, ",") || cfg.Port == 0 {
		return testDBConfig{}, errors.New("test entry requires one TCP host and a nonzero port")
	}
	for _, fallback := range cfg.Fallbacks {
		if fallback.Host != cfg.Host || fallback.Port != cfg.Port {
			return testDBConfig{}, errors.New("multi-endpoint test DSN is not allowed")
		}
	}
	// [AV-02 接手] 实例钉扎（AV-01 移交加固①）：host 只允许 loopback
	// （127.0.0.1/localhost），port 只允许专用快测实例（8217 /
	// USER_POSTGRES_TEST_HOST_PORT）。此前 DBNAME/口令/单端点等已过检；
	// 钉扎放在最后，保持既有错误优先级不变（bad port 先在 pgconn 解析处报
	// invalid，multi-endpoint 先于钉扎报出）。业务端口（8202/8232）在这里被拒。
	if cfg.Host != "127.0.0.1" && cfg.Host != "localhost" {
		return testDBConfig{}, errors.New("test entry is pinned to the dedicated fast instance (host 127.0.0.1)")
	}
	fastPorts := map[string]bool{"8217": true}
	if p := os.Getenv("USER_POSTGRES_TEST_HOST_PORT"); p != "" {
		fastPorts[p] = true
	}
	if !fastPorts[strconv.Itoa(int(cfg.Port))] {
		return testDBConfig{}, errors.New("test entry is pinned to the dedicated fast instance (port 8217 / USER_POSTGRES_TEST_HOST_PORT)")
	}
	return testDBConfig{dsn: dsn, host: cfg.Host, port: strconv.Itoa(int(cfg.Port)), database: cfg.Database}, nil
}

// testDSNFields 仅做语法解析，不访问网络/文件；重复参数拒绝，避免不同
// 消费者按首值/末值解释同一 DSN。数据库/口令来自显式输入而不是 PG* 环境。
func testDSNFields(dsn string) (map[string]string, error) {
	fields := make(map[string]string)
	add := func(key, value string) error {
		if key == "database" {
			key = "dbname"
		}
		if strings.EqualFold(key, "timezone") {
			key = "TimeZone"
		}
		if _, exists := fields[key]; exists {
			return errors.New("duplicate test DSN parameter (details redacted)")
		}
		fields[key] = value
		return nil
	}
	if strings.HasPrefix(dsn, "postgres://") || strings.HasPrefix(dsn, "postgresql://") {
		u, err := url.Parse(dsn)
		if err != nil || u.Fragment != "" {
			return nil, errors.New("invalid test DSN URL (details redacted)")
		}
		fields["host"] = u.Hostname()
		if u.Port() != "" {
			fields["port"] = u.Port()
		}
		fields["dbname"] = strings.TrimPrefix(u.Path, "/")
		if u.User != nil {
			fields["user"] = u.User.Username()
			fields["password"], _ = u.User.Password()
		}
		query, err := url.ParseQuery(u.RawQuery)
		if err != nil {
			return nil, errors.New("invalid test DSN query (details redacted)")
		}
		for key, values := range query {
			for _, value := range values {
				if err := add(key, value); err != nil {
					return nil, err
				}
			}
		}
		return fields, nil
	}
	for rest := strings.TrimSpace(dsn); rest != ""; rest = strings.TrimSpace(rest) {
		eq := strings.IndexByte(rest, '=')
		if eq < 1 {
			return nil, errors.New("invalid test DSN parameter (details redacted)")
		}
		key := strings.TrimSpace(rest[:eq])
		rest = strings.TrimLeft(rest[eq+1:], " \t\r\n")
		var value strings.Builder
		quoted := strings.HasPrefix(rest, "'")
		if quoted {
			rest = rest[1:]
		}
		closed := !quoted
		for len(rest) > 0 {
			ch := rest[0]
			if quoted && ch == '\'' {
				rest, closed = rest[1:], true
				break
			}
			if !quoted && strings.ContainsRune(" \t\r\n", rune(ch)) {
				break
			}
			rest = rest[1:]
			if ch == '\\' {
				if rest == "" {
					return nil, errors.New("invalid test DSN escape (details redacted)")
				}
				ch, rest = rest[0], rest[1:]
			}
			value.WriteByte(ch)
		}
		if !closed || (quoted && rest != "" && !strings.ContainsRune(" \t\r\n", rune(rest[0]))) {
			return nil, errors.New("invalid test DSN quoting (details redacted)")
		}
		if err := add(key, value.String()); err != nil {
			return nil, err
		}
	}
	return fields, nil
}

func testDSNQuote(value string) string {
	return "'" + strings.NewReplacer(`\`, `\\`, "'", `\'`).Replace(value) + "'"
}
