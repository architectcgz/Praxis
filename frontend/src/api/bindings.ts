import type { AgentHistoryItem, AgentMessage, AgentSnapshot } from './agents'
import type { ControlRequest, ControlResponse, SendInputRequest, SendInputResponse } from './commands'
import type { ModelConfigDocument, ModelOption, SaveModelConfigResponse } from './models'
import type { CreateProjectRequest, CreateProjectResponse, ProjectSummary } from './projects'
import type { CreateSessionRequest, CreateSessionResponse, SessionSnapshot, SessionSummary } from './sessions'
import type { HealthSnapshot } from './system'

type SystemBinding = {
    Readiness(): Promise<HealthSnapshot>
}

type ProjectBinding = {
    ListProjects(): Promise<ProjectSummary[]>
    CreateProject(request: CreateProjectRequest): Promise<CreateProjectResponse>
}

type SessionBinding = {
    ListSessions(projectID: string): Promise<SessionSummary[]>
    CreateSession(request: CreateSessionRequest): Promise<CreateSessionResponse>
    GetSession(sessionID: string): Promise<SessionSnapshot>
}

type AgentBinding = {
    GetAgent(agentID: string): Promise<AgentSnapshot>
    ListAgentMessages(agentID: string): Promise<AgentMessage[]>
    ListAgentHistory(agentID: string): Promise<AgentHistoryItem[]>
}

type CommandBinding = {
    SendInput(request: SendInputRequest): Promise<SendInputResponse>
    RequestControl(request: ControlRequest): Promise<ControlResponse>
}

type ModelBinding = {
    ListModels(): Promise<ModelOption[]>
    GetModelConfig(): Promise<ModelConfigDocument>
    SaveModelConfig(request: Omit<ModelConfigDocument, 'profileNames'>): Promise<SaveModelConfigResponse>
    ListProviderModels(providerID: string): Promise<string[]>
}

export class BindingUnavailableError extends Error {
    constructor() {
        super('binding unavailable')
        this.name = 'BindingUnavailableError'
    }
}

type WailsBindings = {
    SystemBindings?: SystemBinding
    ProjectBindings?: ProjectBinding
    SessionBindings?: SessionBinding
    AgentBindings?: AgentBinding
    CommandBindings?: CommandBinding
    ModelBindings?: ModelBinding
}

type WailsWindow = Window & {
    go?: {
        app?: WailsBindings
    }
}

function bindingNamespace(): WailsBindings {
    const bindings = (window as WailsWindow).go?.app
    if (!bindings) {
        throw new BindingUnavailableError()
    }
    return bindings
}

function requireBinding<T>(binding: T | undefined): T {
    if (!binding) {
        throw new BindingUnavailableError()
    }
    return binding
}

export function isBindingAvailable() {
    const bindings = (window as WailsWindow).go?.app
    return Boolean(
        bindings?.SystemBindings &&
        bindings.ProjectBindings &&
        bindings.SessionBindings &&
        bindings.AgentBindings &&
        bindings.CommandBindings &&
        bindings.ModelBindings,
    )
}

export function getSystemBinding() {
    return requireBinding(bindingNamespace().SystemBindings)
}

export function getProjectBinding() {
    return requireBinding(bindingNamespace().ProjectBindings)
}

export function getSessionBinding() {
    return requireBinding(bindingNamespace().SessionBindings)
}

export function getAgentBinding() {
    return requireBinding(bindingNamespace().AgentBindings)
}

export function getCommandBinding() {
    return requireBinding(bindingNamespace().CommandBindings)
}

export function getModelBinding() {
    return requireBinding(bindingNamespace().ModelBindings)
}
