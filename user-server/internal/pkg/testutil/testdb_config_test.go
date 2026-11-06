package testutil

import (
	"context"
	"net"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

func testDBConfigEnv(t *testing.T) {
	t.Helper()
	for _, key := range []string{"POSTGRES_TEST_DSN", "POSTGRES_TEST_HOST", "POSTGRES_TEST_PORT", "POSTGRES_TEST_USER", "POSTGRES_TEST_PASSWORD", "POSTGRES_TEST_DBNAME", "POSTGRES_TEST_SSLMODE", "POSTGRES_PASSWORD", "USER_POSTGRES_TEST_HOST_PORT", "USER_POSTGRES_HOST_PORT", "DB_PORT", "PGPASSWORD", "PGDATABASE", "PGHOST", "PGPORT", "PGUSER", "PGSERVICE", "PGSERVICEFILE", "PGPASSFILE"} {
		t.Setenv(key, "")
	}
	t.Setenv("POSTGRES_TEST_PASSWORD", "test-secret")
}

func TestNewTestDBRejectsUnsafeBeforeNetwork(t *testing.T) {
	for _, tc := range []struct{ name, key, value, want string }{
		{"business name", "POSTGRES_TEST_DBNAME", "user_db", "must be user_db_test"},
		{"business URL", "POSTGRES_TEST_DSN", "postgres://admin:test-secret@127.0.0.1:1/user_db", "must be user_db_test"},
		{"missing password", "POSTGRES_TEST_PASSWORD", "", "password is missing"},
		// [AV-02 接手] 实例钉扎生效后，POSTGRES_TEST_PORT=1 在配置门就被拒
		// （仍在任何连网之前 fail closed，子进程同样不 SKIP），want 相应改为钉扎报错。
		{"explicit unreachable", "POSTGRES_TEST_PORT", "1", "pinned"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			testDBConfigEnv(t)
			t.Setenv("POSTGRES_TEST_HOST", "127.0.0.1")
			t.Setenv("POSTGRES_TEST_PORT", "1")
			t.Setenv(tc.key, tc.value)
			t.Setenv("TESTDB_CONFIG_CHILD", "reject")
			cmd := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestTestDBConfigChild$", "-test.v")
			output, err := cmd.CombinedOutput()
			if err == nil || !strings.Contains(string(output), tc.want) || strings.Contains(string(output), "SKIP") {
				t.Fatalf("child must fail closed with %q, got err=%v output=%s", tc.want, err, output)
			}
			if strings.Contains(string(output), "test-secret") {
				t.Fatal("child disclosed credentials")
			}
		})
	}
}

func TestTestDBConfigChild(t *testing.T) {
	if os.Getenv("TESTDB_CONFIG_CHILD") == "reject" {
		NewTestDB(t)
	}
}

// TestTestDBAuthenticationPreflight 只读：可在任何建删之前定向运行。
// 缺少配置即失败，不用 Skip 冒充认证通过。
func TestTestDBAuthenticationPreflight(t *testing.T) {
	cfg, err := readTestDBConfig()
	if err != nil {
		t.Fatal(err)
	}
	conn, err := pgconn.Connect(t.Context(), testDSNWithDatabase(cfg.dsn, "postgres"))
	if err != nil {
		t.Fatal(safeTestDBError(err))
	}
	defer func() { _ = conn.Close(context.Background()) }()
	results, err := conn.Exec(t.Context(), "SELECT 1, current_database(), current_user, inet_server_addr(), inet_server_port(); SELECT rolcreatedb, rolsuper FROM pg_roles WHERE rolname = current_user").ReadAll()
	if err != nil {
		t.Fatal(safeTestDBError(err))
	}
	if len(results) != 2 || len(results[0].Rows) != 1 || string(results[0].Rows[0][0]) != "1" || string(results[0].Rows[0][1]) != "postgres" {
		t.Fatal("unexpected preflight identity")
	}
	t.Logf("read-only authentication: database=%s user=%s server=%s:%s configured=%s:%s createdb=%s superuser=%s", results[0].Rows[0][1], results[0].Rows[0][2], results[0].Rows[0][3], results[0].Rows[0][4], cfg.host, cfg.port, results[1].Rows[0][0], results[1].Rows[0][1])
}

func TestTestDBExplicitPortDoesNotFallback(t *testing.T) {
	testDBConfigEnv(t)
	t.Setenv("POSTGRES_TEST_PORT", "1")
	t.Setenv("USER_POSTGRES_HOST_PORT", "8202")
	t.Setenv("DB_PORT", "8232")
	got := testDBPortCandidates()
	if len(got) != 1 || got[0] != "1" {
		t.Fatalf("explicit port must be the only candidate, got %v", got)
	}
}

func TestTestDBDefaultsToDedicatedInstance(t *testing.T) {
	testDBConfigEnv(t)
	t.Setenv("USER_POSTGRES_HOST_PORT", "8202")
	t.Setenv("DB_PORT", "8232")
	got := testDBPortCandidates()
	if len(got) != 1 || got[0] != "8217" {
		t.Fatalf("default must not probe business endpoints, got %v", got)
	}
}

func TestEndpointCacheDoesNotIgnoreChangedConfiguration(t *testing.T) {
	testDBConfigEnv(t)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	endpointMu.Lock()
	oldHost, oldPort, oldCached := endpointHost, endpointPort, endpointCached
	oldConfig := endpointConfig
	endpointCached = false
	endpointMu.Unlock()
	t.Cleanup(func() {
		endpointMu.Lock()
		endpointHost, endpointPort, endpointCached = oldHost, oldPort, oldCached
		endpointConfig = oldConfig
		endpointMu.Unlock()
	})
	_, port, err := net.SplitHostPort(listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	// [AV-02 接手] 钉扎生效后随机端口必须同时声明为快测实例端口
	// （USER_POSTGRES_TEST_HOST_PORT）才能通过配置门；语义不变：
	// 配置变化后缓存端点不得顶替新配置。
	t.Setenv("USER_POSTGRES_TEST_HOST_PORT", port)
	t.Setenv("POSTGRES_TEST_PORT", port)
	if _, _, err := resolveTestDBEndpoint(); err != nil {
		t.Fatal(err)
	}
	t.Setenv("USER_POSTGRES_TEST_HOST_PORT", "1")
	t.Setenv("POSTGRES_TEST_PORT", "1")
	if _, _, err := resolveTestDBEndpoint(); err == nil {
		t.Fatal("changed explicit configuration was silently replaced with a cached endpoint")
	}
}

func TestMaskedDSNDoesNotDiscloseCredentials(t *testing.T) {
	for _, dsn := range []string{
		"postgres://admin:secret-token@127.0.0.1:8217/user_db_test",
		"host=127.0.0.1 port=8217 user=admin password='secret token' dbname=user_db_test",
		"host=127.0.0.1 password=secret-token password=other-secret dbname=user_db_test",
		"password='secret-token",
	} {
		got := maskedDSN(dsn)
		if strings.Contains(got, "secret") || strings.Contains(got, "token") {
			t.Errorf("credential disclosed in diagnostic: %q", got)
		}
	}
}

func TestReadTestDBConfig(t *testing.T) {
	tests := []struct {
		name string
		env  map[string]string
		want string
	}{
		{"default", nil, ""},
		// [AV-02 接手] 钉扎后显式端口只有在声明为快测实例端口
		// （USER_POSTGRES_TEST_HOST_PORT）时才放行；纯 POSTGRES_TEST_PORT=8227 被拒。
		{"explicit declared port", map[string]string{"USER_POSTGRES_TEST_HOST_PORT": "8227", "POSTGRES_TEST_PORT": "8227"}, ""},
		{"undeclared port", map[string]string{"POSTGRES_TEST_PORT": "8227"}, "pinned"},
		{"business port", map[string]string{"POSTGRES_TEST_PORT": "8202"}, "pinned"},
		{"foreign host", map[string]string{"POSTGRES_TEST_HOST": "db.example.internal"}, "pinned"},
		{"business dsn port", map[string]string{"POSTGRES_TEST_DSN": "postgres://admin:test-secret@127.0.0.1:8202/user_db_test?sslmode=disable"}, "pinned"},
		{"url", map[string]string{"POSTGRES_TEST_DSN": "postgres://admin:test-secret@localhost:8217/user_db_test?sslmode=disable"}, ""},
		{"quoted", map[string]string{"POSTGRES_TEST_PASSWORD": "secret ' with \\ spaces"}, ""},
		{"business database", map[string]string{"POSTGRES_TEST_DBNAME": "user_db"}, "must be user_db_test"},
		{"injection", map[string]string{"POSTGRES_TEST_DBNAME": "user_db_test\"; DROP DATABASE user_db;--"}, "must be user_db_test"},
		{"url business database", map[string]string{"POSTGRES_TEST_DSN": "postgres://admin:test-secret@localhost:8217/user_db"}, "must be user_db_test"},
		{"missing password", map[string]string{"POSTGRES_TEST_PASSWORD": ""}, "password is missing"},
		{"bad port", map[string]string{"POSTGRES_TEST_PORT": "secret-token"}, "invalid"},
		{"zero port", map[string]string{"POSTGRES_TEST_PORT": "0"}, "nonzero port"},
		{"multiple hosts", map[string]string{"POSTGRES_TEST_DSN": "host=localhost,127.0.0.1 port=8217,8202 user=admin password=test-secret dbname=user_db_test sslmode=disable"}, "multi-endpoint"},
		{"unknown key", map[string]string{"POSTGRES_TEST_DSN": "host=localhost port=8217 password=test-secret dbname=user_db_test service=secret-token"}, "invalid"},
		{"foreign passfile", map[string]string{"POSTGRES_TEST_DSN": "host=localhost port=8217 password=test-secret dbname=user_db_test passfile=/private/secret-token"}, "not allowed"},
		{"identity changed", map[string]string{"POSTGRES_TEST_DSN": "host=localhost port=8217 password=test-secret dbname=user_db_test hostaddr=127.0.0.1"}, "invalid"},
		// [AV-02 接手] 钉扎不改变"DSN 显式字段优先于环境"的语义，但 DSN 的
		// host/port 同样要落在钉扎允许集内，这里补声明 8227 为快测实例端口。
		{"dsn overrides env", map[string]string{"POSTGRES_TEST_DSN": "postgres://admin:test-secret@localhost:8227/user_db_test?sslmode=disable", "POSTGRES_TEST_HOST": "other", "POSTGRES_TEST_PORT": "1", "POSTGRES_TEST_DBNAME": "user_db", "USER_POSTGRES_TEST_HOST_PORT": "8227"}, ""},
		{"duplicate database", map[string]string{"POSTGRES_TEST_DSN": "host=localhost port=8217 password=test-secret dbname=user_db dbname=user_db_test"}, "duplicate"},
		{"missing dsn password ignores ambient", map[string]string{"POSTGRES_TEST_DSN": "host=localhost port=8217 user=admin dbname=user_db_test", "PGPASSWORD": "ambient-secret"}, "password is missing"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testDBConfigEnv(t)
			for key, value := range tt.env {
				t.Setenv(key, value)
			}
			cfg, err := readTestDBConfig()
			if tt.want != "" {
				if err == nil || !strings.Contains(err.Error(), tt.want) {
					t.Fatalf("want error %q, got %v", tt.want, err)
				}
				if strings.Contains(err.Error(), "secret-token") || strings.Contains(err.Error(), "ambient-secret") {
					t.Fatal("error disclosed credentials")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if cfg.database != "user_db_test" {
				t.Fatalf("wrong database %q", cfg.database)
			}
			for _, name := range []string{"postgres", "user_db_test_slot2", "user_db_test_12345"} {
				parsed, parseErr := pgconn.ParseConfig(testDSNWithDatabase(cfg.dsn, name))
				if parseErr != nil || parsed.Database != name {
					t.Fatalf("database switch to %s failed (parse success %t)", name, parseErr == nil)
				}
			}
		})
	}
}

func TestTestDSNFieldsPreserveQuotedAndURLValues(t *testing.T) {
	for _, dsn := range []string{
		"host = localhost port=8217 password='space \\' quote \\\\ slash' dbname=user_db_test",
		"postgresql://admin:space%20%27%20quote%20%5C%20slash@localhost:8217/user_db_test?sslmode=disable",
	} {
		fields, err := testDSNFields(dsn)
		if err != nil {
			t.Fatal(err)
		}
		if fields["password"] != "space ' quote \\ slash" {
			t.Fatal("password quoting changed its value")
		}
	}
}

func TestTestDSNFieldsRejectMalformed(t *testing.T) {
	for _, dsn := range []string{"password='open", "password=x\\", "host localhost", "password='a'b", "postgres://u:secret-token@h/db?x=%zz", "dbname=a database=b"} {
		if _, err := testDSNFields(dsn); err == nil {
			t.Error("malformed/ambiguous DSN was accepted")
		}
	}
}
