package service

// write_claim_error_a12b_test.go — §8.3-20：claimWriteSlot 拿到的 repo 报错不得被吞成「放行」。
//
// 立项理由（注码实证过的失效形态）：把 write_ledger.go 里 `if err != nil { return err }`
// 注成「Warnf 一句后 return nil」，服务层原有六条腿全绿——S1 走的是 holder != nil（别人占着坑）、
// S4 走的是 writeClaimRepo == nil（没接线），其余走 happy path，没有一条从 repo 侧注入
// ClaimWriteSlot 的报错。并发/跨进程下占坑报错被吞、本腿照常下发，即双发。
// 本腿用「只替 ClaimWriteSlot 一个方法」的假 repo 注入报错（形状同 cancelOnFindRepo /
// flakyStepRepo，其余调用一律走真 repo），断三件事：
// ① comment_send 一帧都不下发；② 步判 failed；③ 红因透出报错原文、且不被改写成
// 「未接线」那句别的结论——「闸门没接线」（S4 有腿）与「占坑报错」是两种故障，
// 运维处置不同，两条腿各钉各的文案，不合并。

import (
	"context"
	"errors"
	"strings"
	"testing"

	"hivemtk-user/internal/browser_automation/model"
	"hivemtk-user/internal/browser_automation/repository"
)

type claimErrRepo struct {
	repository.BrowserWriteClaimRepository
	err error
}

func (c *claimErrRepo) ClaimWriteSlot(context.Context, uint, uint, uint, string) (*model.BrowserWriteClaim, error) {
	return nil, c.err
}

func TestWriteClaimRepoErrorRefusesDispatch(t *testing.T) {
	exec, bundle, frames := newA12Exec(t, happyReply)
	task, session := bundle.seedTask(t, commentSendSteps, false)
	claimErr := errors.New("claim write slot failed: connection reset by peer")
	exec.SetWriteClaimRepository(&claimErrRepo{BrowserWriteClaimRepository: bundle.claimRepo, err: claimErr})

	a12Run(t, exec, task, session)
	// ① 占坑报错 = 拿不到独占结论，一帧都不许下发
	if n := frames.countOf("comment_send"); n != 0 {
		t.Fatalf("comment_send 到线 %d want 0——占坑报错被吞成放行：并发下没人知道另一条腿在做什么，本腿照常下发即双发", n)
	}
	// ② 步判红，不是悄悄跳过
	row := readStepState(t, bundle, session.ID, 1)
	if row.Status != "failed" {
		t.Errorf("步 status=%s want failed——占不了坑必须判红（免检的条件只能是「不是写步」）", row.Status)
	}
	// ③ 文案透出报错原文，且与「未接线」结论可辨
	if !strings.Contains(row.ErrorMsg, claimErr.Error()) {
		t.Errorf("步红因必须透出占坑报错原文（吞掉或改写就查不到是哪一层报错），got %q", row.ErrorMsg)
	}
	if strings.Contains(row.ErrorMsg, "未接线") {
		t.Errorf("占坑报错被写成了「闸门未接线」那句别的结论（两条故障运维处置不同），got %q", row.ErrorMsg)
	}
	if row.SubmitState != "" {
		t.Errorf("submit_state=%q want 空——被拒发的步不该留下尝试凭据", row.SubmitState)
	}
}
