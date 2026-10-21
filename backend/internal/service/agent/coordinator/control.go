package coordinator

import "praxis/internal/identity"

func (s *Coordinator) Pause(agentID identity.AgentID, executionID identity.AgentExecutionID) bool {
	return s.activeTasks.Pause(agentID, executionID)
}

func (s *Coordinator) Stop(agentID identity.AgentID, executionID identity.AgentExecutionID) bool {
	return s.activeTasks.Stop(agentID, executionID)
}
