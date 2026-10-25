package toolexecution

import executionmodel "praxis/internal/core/toolexecution"

// Approval 复用活跃工具的快照，不提供持久化审批查询或恢复。
type Approval = executionmodel.Approval
