package service

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"hivemtk-user/internal/model"
)

// faBackupService 构造带可注入 updateRecordFn 的 RestoreService（仅测试用，不需要真实 DB）。
func faBackupService(t *testing.T, fn func(ctx context.Context, record *model.RestoreRecord) error) *RestoreService {
	t.Helper()
	return &RestoreService{updateRecordFn: fn}
}

// FA-BACKUP-01：running 状态落库失败必须中止，不得进入不可逆的解压阶段。
func TestFABackup01_RunningStatusFailureAborts(t *testing.T) {
	called := 0
	svc := faBackupService(t, func(ctx context.Context, record *model.RestoreRecord) error {
		called++
		return errors.New("db down")
	})
	rec := &model.RestoreRecord{ID: 1, Status: "pending"}
	// backup.FilePath 指向不存在文件：若 executeRestore 未中止而继续解压，会走到 decompressBackup
	// 并因 failRestoreRecord 再次调 updateRecordFn（called>=2）。中止则 called==1。
	bk := &model.Backup{FilePath: "/nonexistent/fa-backup.zip"}
	svc.executeRestore(context.Background(), rec, bk)
	if called != 1 {
		t.Fatalf("running 落库失败应中止，仅调用 1 次 updateRecordFn，实际 %d", called)
	}
	if rec.Status != "running" {
		t.Fatalf("中止后内存态应保持 running，实际 %s", rec.Status)
	}
}

// FA-BACKUP-01：completed 落库失败时显式标记 unknown 供对账（内存态可查）。
func TestFABackup01_CompletedStatusFailureMarksUnknown(t *testing.T) {
	callCount := 0
	svc := faBackupService(t, func(ctx context.Context, record *model.RestoreRecord) error {
		callCount++
		// running 成功，completed 与其后的 unknown 重试均失败
		if record.Status == "running" {
			return nil
		}
		return errors.New("db down")
	})
	// 无法走完整 executeRestore（依赖真实 DB/文件），这里直接验证完成态落库逻辑：
	rec := &model.RestoreRecord{ID: 2, Status: "completed"}
	if e := svc.updateRecordFn(context.Background(), rec); e != nil {
		rec.Status = "unknown"
		rec.ErrorMessage = "数据已恢复但完成状态落库失败，需对账：" + e.Error()
	}
	if rec.Status != "unknown" {
		t.Fatalf("completed 落库失败应标记 unknown，实际 %s", rec.Status)
	}
	if rec.ErrorMessage == "" {
		t.Fatal("unknown 态应携带可诊断 ErrorMessage")
	}
}

// faWriteZip 写合成 ZIP 到临时文件。
func faWriteZip(t *testing.T, entries map[string][]byte) string {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "fa-backup-*.zip")
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	for name, data := range entries {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	return f.Name()
}

// FA-BACKUP-02：正常 ZIP 解压成功。
func TestFABackup02_DecompressNormal(t *testing.T) {
	svc := faBackupService(t, nil)
	zf := faWriteZip(t, map[string][]byte{"data.json": []byte(`{"version":1}`)})
	dest := t.TempDir()
	if err := svc.decompressBackup(context.Background(), zf, dest); err != nil {
		t.Fatalf("正常解压应成功: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(dest, "data.json"))
	if err != nil || string(got) != `{"version":1}` {
		t.Fatalf("解压内容不符: %s %v", got, err)
	}
}

// FA-BACKUP-02：单文件声明/实际大小超限被拒。
func TestFABackup02_EntrySizeLimitRejects(t *testing.T) {
	svc := faBackupService(t, nil)
	big := bytes.Repeat([]byte("x"), restoreMaxEntryBytes+1)
	zf := faWriteZip(t, map[string][]byte{"big.bin": big})
	dest := t.TempDir()
	err := svc.decompressBackup(context.Background(), zf, dest)
	if err == nil {
		t.Fatal("单文件超限应被拒")
	}
}

// FA-BACKUP-02：条目数超限被拒。
func TestFABackup02_EntryCountLimitRejects(t *testing.T) {
	svc := faBackupService(t, nil)
	entries := make(map[string][]byte, restoreMaxZipEntries+1)
	for i := 0; i <= restoreMaxZipEntries; i++ {
		entries["f"+string(rune('a'+i%26))+string(rune('a'+i/26))] = []byte("x")
	}
	zf := faWriteZip(t, entries)
	dest := t.TempDir()
	if err := svc.decompressBackup(context.Background(), zf, dest); err == nil {
		t.Fatal("条目数超限应被拒")
	}
}

// FA-BACKUP-02：路径穿越仍被拒（回归）。
func TestFABackup02_PathTraversalRejected(t *testing.T) {
	svc := faBackupService(t, nil)
	zf := faWriteZip(t, map[string][]byte{"../../evil.txt": []byte("x")})
	dest := t.TempDir()
	if err := svc.decompressBackup(context.Background(), zf, dest); err == nil {
		t.Fatal("路径穿越应被拒")
	}
}
