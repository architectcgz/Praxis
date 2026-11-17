package session

import (
	"context"
	"fmt"
	"praxis/internal/clock"
	agentmodel "praxis/internal/core/agent"
	sessionentry "praxis/internal/core/entry"
	modelapi "praxis/internal/core/model"
	providerapi "praxis/internal/core/provider"
	"praxis/internal/core/security"
	"praxis/internal/core/tool"
	"praxis/internal/identity"
	"slices"
	"sync"
)

// EntryLog 绑定会话日志，实际文件写入、索引和解码由存储实现完成。
type EntryLog interface {
	CreateSubagent(context.Context, identity.AgentID) error
	AppendEntry(context.Context, identity.AgentID, sessionentry.ContextEntry) (bool, error)
	Sync(context.Context) error
	Node(context.Context, string) (sessionentry.Node, error)
	IndexedNodes(context.Context, []string) ([]sessionentry.Node, error)
	PathNodes(context.Context, string) ([]sessionentry.Node, error)
	Entries(context.Context, []string) ([]*sessionentry.ContextEntry, error)
	Requests(context.Context, []identity.TurnID) ([]sessionentry.ModelRequestRecord, error)
	Run(context.Context, identity.RunID) (sessionentry.RunRecord, error)
}

// Configuration 属于已打开的会话；发布后只读，只有显式 reload 才整体替换。
// Provider 持有同一批次的凭据，配置不写入会话日志，也不通过 DTO 暴露。
type Configuration struct {
	Models             *modelapi.Catalog
	Definitions        []agentmodel.AgentDefinition
	Tools              tool.ToolCatalog
	Provider           providerapi.Provider
	GlobalApprovalMode security.ApprovalMode
	ApprovalMode       security.ApprovalMode
}

func (c *Configuration) Definition(id identity.AgentDefinitionID) (agentmodel.AgentDefinition, error) {
	for _, definition := range c.Definitions {
		if definition.ID == id {
			return definition.Snapshot(), nil
		}
	}
	return agentmodel.AgentDefinition{}, fmt.Errorf("agent definition %q is not configured", id)
}

// ModelChoice 表达用户选择的模型和推理等级；查询结果全空表示尚未选择。
type ModelChoice struct {
	ProviderID     string
	ModelID        string
	ReasoningLevel string
}

// Session 持有会话数据；提交锁保证接纳与同步追加顺序，数据锁保护内存配置与状态。
// 日志写入失败不撤销内存更新；发布后的 Entry 及其内部载荷只读。
// 模型请求、工具执行和 Hook 不持有会话锁。
type Session struct {
	commitMu      sync.Mutex
	mu            sync.RWMutex
	configuration *Configuration
	choice        ModelChoice
	data          *Data
	log           EntryLog
	clock         clock.Clock
	ids           identity.IDGenerator
}

// New 接管完成索引与必要恢复的数据；发布实例后不再直接修改 data。
func New(data *Data, log EntryLog, clock clock.Clock, ids identity.IDGenerator) *Session {
	return &Session{
		data:  data,
		log:   log,
		clock: clock,
		ids:   ids,
	}
}

func (s *Session) Configure(config *Configuration, choice ModelChoice) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.configuration = config
	s.choice = choice
}

func (s *Session) Configuration() *Configuration {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.configuration
}

func (s *Session) Choice() ModelChoice {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.choice
}

func (s *Session) RunConfiguration() (*Configuration, modelapi.Config, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	model, err := s.configuration.Models.Get(s.choice.ProviderID, s.choice.ModelID, s.choice.ReasoningLevel)
	return s.configuration, model, err
}

func (s *Session) Definition(id identity.AgentDefinitionID) (agentmodel.AgentDefinition, error) {
	return s.Configuration().Definition(id)
}

func (s *Session) Empty() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.data.HeadEntryID == ""
}

// syncHistory 由提交边界调用。追加曾报告失败但后来确认完整落盘的不可变 Entry，
// 在索引补齐后释放保留引用；缺少完整记录的接纳结果继续由会话持有。
func (s *Session) syncHistory(ctx context.Context) error {
	if err := s.log.Sync(ctx); err != nil {
		return err
	}
	s.mu.RLock()
	ids := make([]string, 0, len(s.data.PendingEntries))
	for id := range s.data.PendingEntries {
		ids = append(ids, id)
	}
	s.mu.RUnlock()
	if len(ids) == 0 {
		return nil
	}
	nodes, err := s.log.IndexedNodes(ctx, ids)
	if err != nil {
		return err
	}
	s.mu.Lock()
	for _, node := range nodes {
		s.data.releaseEntry(node.ID)
	}
	s.mu.Unlock()
	return nil
}

func capped[T any](values []T, limit int) []T {
	if limit <= 0 {
		return values
	}
	return values[:min(len(values), limit)]
}

func sorted[T any](values []T, compare func(T, T) int) []T {
	slices.SortFunc(values, compare)
	return values
}
