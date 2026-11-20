package repository

import (
	"testing"

	"gorm.io/gorm"
)

func TestReplayConstructors_PreserveHandleAndNil(t *testing.T) {
	sessions, messages := NewReplayRepositories(nil)
	if sessions != nil || messages != nil {
		t.Fatal("空延迟仓储不应装配可用的回扫资格仓储")
	}
	delayed := NewDelayedOutboundRepositoryForReplay(nil)
	if delayed == nil || delayed.db != nil {
		t.Fatal("空离线仓储应保留未初始化的延迟仓储语义")
	}

	database := &gorm.DB{}
	sessions, messages = NewReplayRepositories(NewDelayedOutboundRepository(database))
	if sessions == nil || messages == nil || sessions.db != database || messages.db != database {
		t.Fatal("资格仓储必须沿用延迟仓储的数据库句柄")
	}
	delayed = NewDelayedOutboundRepositoryForReplay(NewBridgeOfflineReplayRepositoryWithDB(database))
	if delayed == nil || delayed.db != database {
		t.Fatal("离线回扫装配必须沿用原数据库句柄")
	}
}
