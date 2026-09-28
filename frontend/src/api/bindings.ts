import type { AgentHistoryItem, AgentMessage, AgentSnapshot } from './agents'
import type {
    CloseAgentRequest,
    CloseAgentResponse,
    PauseAgentRequest,
    PauseAgentResponse,
    SendInputRequest,
    SendInputResponse,
} from './commands'
import type { ModelConfigDocument, ModelOption, SaveModelConfigResponse } from './models'
import type { CreateProjectRequest, CreateProjectResponse, ProjectSummary } from './projects'
import type { CreateSessionRequest, CreateSessionResponse, SessionSnapshot, SessionSummary } from './sessions'

type ProjectBinding = {
    ListProjects(): Promise<ProjectSummary[]>
    CreateProject(request: CreateProjectRequest): Promise<CreateProjectResponse>
    SelectProjectPath(): Promise<string>
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
    PauseAgent(request: PauseAgentRequest): Promise<PauseAgentResponse>
    CloseAgent(request: CloseAgentRequest): Promise<CloseAgentResponse>
}

type ModelBinding = {
    ListModels(): Promise<ModelOption[]>
    GetModelConfig(): Promise<ModelConfigDocument>
    SaveModelConfig(request: ModelConfigDocument): Promise<SaveModelConfigResponse>
    SetProviderKey(providerId: string, value: string): Promise<void>
    ClearProviderKey(providerId: string): Promise<void>
    ListProviderModels(providerID: string): Promise<string[]>
}

export class BindingUnavailableError extends Error {
    constructor() {
        super('binding unavailable')
        this.name = 'BindingUnavailableError'
    }
}

type WailsBindings = {
    ProjectBindings?: ProjectBinding
    SessionBindings?: SessionBinding
    AgentBindings?: AgentBinding
    CommandBindings?: CommandBinding
    ModelBindings?: ModelBinding
}

type WailsWindow = Window & {
    go?: {
        bindings?: WailsBindings
    }
}

function bindingNamespace(): WailsBindings {
    const bindings = (window as WailsWindow).go?.bindings
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
    const bindings = (window as WailsWindow).go?.bindings
    return Boolean(
        bindings?.ProjectBindings &&
        bindings.SessionBindings &&
        bindings.AgentBindings &&
        bindings.CommandBindings &&
        bindings.ModelBindings,
    )
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
