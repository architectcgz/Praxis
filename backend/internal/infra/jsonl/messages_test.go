package jsonl

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"praxis/internal/contracts"
	agentmodel "praxis/internal/core/agent"
	sessionmodel "praxis/internal/core/session"
)

func userMessage(id string) sessionmodel.MessageData {
	return sessionmodel.MessageData{
		ID:         id,
		Role:       sessionmodel.RoleUser,
		AuthorKind: sessionmodel.AuthorUser,
		Blocks:     []sessionmodel.Block{{Kind: sessionmodel.BlockText, Text: id}},
	}
}
func TestMessageStreamsOrderingIdempotencyAndIsolation(t *testing.T) {
	f := newStoreFixture(t)
	ctx := context.Background()
	private, err := agentmodel.NewAgent("private", "session", agentmodel.DefinitionDelegate, contracts.ProfileDelegate, time.Now())
	requireOK(t, err)
	requireOK(t, f.store.InTx(ctx, func(txCtx context.Context) error { return f.repos.Agents.Save(txCtx, private) }))
	first, err := f.repos.Messages.Append(ctx, f.agent, userMessage("first"))
	requireOK(t, err)
	_, err = f.repos.Messages.Append(ctx, private, userMessage("private"))
	requireOK(t, err)
	second, err := f.repos.Messages.Append(ctx, f.agent, userMessage("second"))
	requireOK(t, err)
	seq := f.store.sequences["session"]
	repeated, err := f.repos.Messages.Append(ctx, f.agent, userMessage("first"))
	requireOK(t, err)
	if repeated.Sequence != first.Sequence || f.store.sequences["session"] != seq {
		t.Fatal("幂等消息不能再次追加")
	}
	conflict := userMessage("first")
	conflict.Blocks[0].Text = "不同正文"
	if _, err := f.repos.Messages.Append(ctx, f.agent, conflict); !errors.Is(err, contracts.ErrRequestConflict) {
		t.Fatal(err)
	}
	requireOK(t, f.store.InTx(ctx, func(txCtx context.Context) error {
		return f.repos.Sessions.Rename(txCtx, "session", "新名称", time.Now())
	}))
	stream, err := f.repos.Messages.LoadMessages(ctx, "session", "agent", 1)
	requireOK(t, err)
	if len(stream.Messages) != 1 || stream.Messages[0].ID != "second" || stream.SequenceBoundary != second.Sequence || stream.SequenceBoundary == f.store.sequences["session"] {
		t.Fatalf("边界必须来自消息流: %+v", stream)
	}
	page, err := f.repos.Messages.List(ctx, f.agent, first.Sequence, 1)
	requireOK(t, err)
	if len(page) != 1 || page[0].ID != "second" {
		t.Fatal(page)
	}
	stream, err = f.repos.Messages.LoadMessages(ctx, "session", "private", 0)
	requireOK(t, err)
	if len(stream.Messages) != 1 || stream.Messages[0].ID != "private" {
		t.Fatal(stream)
	}
	stream.Messages[0].Blocks[0].Text = "不能污染仓库"
	stream, err = f.repos.Messages.LoadMessages(ctx, "session", "private", 0)
	requireOK(t, err)
	if stream.Messages[0].TextContent() != "private" {
		t.Fatal("返回对象未隔离")
	}
	f.restart(t)
	stream, err = f.repos.Messages.LoadMessages(ctx, "session", "agent", 0)
	requireOK(t, err)
	if len(stream.Messages) != 2 || stream.Messages[0].ID != "first" || stream.Messages[1].ID != "second" {
		t.Fatal(stream)
	}
}

func TestMessageIndexCacheBoundAndReadOnly(t *testing.T) {
	f := newStoreFixture(t)
	ctx := context.Background()
	for i := 0; i < 5; i++ {
		_, err := f.repos.Messages.Append(ctx, f.agent, userMessage(fmt.Sprint(i)))
		requireOK(t, err)
	}
	f.restart(t)
	f.store.messages = newMessageCache(2)
	path, err := f.store.path("session")
	requireOK(t, err)
	before, err := os.Stat(path)
	requireOK(t, err)
	for range 2 {
		stream, err := f.repos.Messages.LoadMessages(ctx, "session", "agent", 0)
		requireOK(t, err)
		if len(stream.Messages) != 5 || f.store.messages.entries > 2 || len(f.store.messages.sessions) != 0 {
			t.Fatal("超长 Session 索引不得突破缓存上限")
		}
	}
	after, err := os.Stat(path)
	requireOK(t, err)
	if after.Size() != before.Size() {
		t.Fatal("普通消息读取不得写入日志")
	}
	f.store.messages = newMessageCache(10)
	_, err = f.repos.Messages.LoadMessages(ctx, "session", "agent", 0)
	requireOK(t, err)
	if f.store.messages.entries != 5 {
		t.Fatal("热 Session 应复用索引")
	}
	_, err = f.repos.Messages.Append(ctx, f.agent, userMessage("new"))
	requireOK(t, err)
	if f.store.messages.entries != 6 {
		t.Fatal("已加载索引未随提交更新")
	}
}

func TestMessageCacheEvictsAcrossSessions(t *testing.T) {
	f := newStoreFixture(t)
	ctx := context.Background()
	f.store.messages = newMessageCache(3)
	for _, scope := range []string{"second", "third"} {
		session, err := sessionmodel.NewSession(contracts.SessionID(scope), "project", "workspace", time.Now())
		requireOK(t, err)
		agent, err := agentmodel.NewAgent(contracts.AgentID(scope), session.ID, agentmodel.DefinitionPrimary, contracts.ProfilePrimary, time.Now())
		requireOK(t, err)
		requireOK(t, f.store.InTx(ctx, func(txCtx context.Context) error {
			if err := f.repos.Sessions.Save(txCtx, session); err != nil {
				return err
			}
			return f.repos.Agents.Save(txCtx, agent)
		}))
		for i := range 2 {
			_, err := f.repos.Messages.Append(ctx, agent, userMessage(fmt.Sprint(i)))
			requireOK(t, err)
		}
		_, err = f.repos.Messages.LoadMessages(ctx, session.ID, agent.ID, 0)
		requireOK(t, err)
		if f.store.messages.entries > 3 {
			t.Fatal("缓存总条目数越界")
		}
	}
	if f.store.messages.sessions["second"] != nil || f.store.messages.sessions["third"] == nil {
		t.Fatal("缓存未按最近访问淘汰")
	}
	stream, err := f.repos.Messages.LoadMessages(ctx, "second", "second", 0)
	requireOK(t, err)
	if len(stream.Messages) != 2 || f.store.messages.entries > 3 {
		t.Fatal("淘汰后重建错误")
	}
}
