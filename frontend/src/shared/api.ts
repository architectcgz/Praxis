import {EventsOn} from '../../wailsjs/runtime/runtime'

export const AGENT_OUTPUT_EVENT_NAME = 'praxis:agent-output'

export type StartupIssue = {
    code: string
    path?: string
    message: string
}

export type HealthSnapshot = {
    ready: boolean
    issue?: StartupIssue
}

export const API_ERROR_CODES = {
    invalidRequest: 'invalid_request',
    validation: 'validation_error',
    invalidTransition: 'invalid_transition',
    notReady: 'orchestration_not_ready',
    bindingUnavailable: 'binding_unavailable',
    notFound: 'not_found',
    agentExecuting: 'agent_executing',
    agentUnavailable: 'agent_unavailable',
    requestNotFound: 'request_not_found',
    requestConflict: 'request_conflict',
    workspaceConflict: 'workspace_conflict',
    alreadySettled: 'already_settled',
    workQueueEmpty: 'work_queue_empty',
    workItemActive: 'work_item_active',
    alreadyDelivered: 'already_delivered',
    projectWorkspaceInvalid: 'project_workspace_invalid',
    requestCanceled: 'request_canceled',
    requestTimeout: 'request_timeout',
    executionContract: 'execution_contract_error',
    executionPolicyBlocked: 'execution_policy_blocked',
    executionApprovalRequired: 'execution_approval_required',
    executionStorage: 'execution_storage_error',
    executionProvider: 'execution_provider_error',
    executionTool: 'execution_tool_error',
    executionResourceLimit: 'execution_resource_limit',
    executionBusy: 'execution_busy',
    executionClosed: 'execution_closed',
    executionInterrupted: 'execution_interrupted',
    internal: 'internal_error',
} as const

export type ApiErrorCode = typeof API_ERROR_CODES[keyof typeof API_ERROR_CODES]

export function isApiErrorCode(value: string): value is ApiErrorCode {
    return Object.values(API_ERROR_CODES).includes(value as ApiErrorCode)
}

export type SessionSnapshot = {
    id: string
    goal: string
    workspaceKey: string
    createdAt: string
    updatedAt: string
    groups: GroupSnapshot[]
    agents: AgentSnapshot[]
}

export type SessionSummary = {
    id: string
    goal: string
    workspaceKey: string
    createdAt: string
    updatedAt: string
}

export type CreateProjectRequest = {projectName: string; goal: string}
export type CreateProjectResponse = {
    sessionId: string
    agentId: string
    goal: string
    workspaceKey: string
}
export type CreateSessionRequest = {workspaceKey: string; goal: string}
export type CreateSessionResponse = {
    sessionId: string
    agentId: string
    goal: string
    workspaceKey: string
}

type GroupSnapshot = {id: string; primaryAgentId: string; maxConcurrent: number}

export type AgentSnapshot = {
    id: string
    sessionId: string
    groupId: string
    profile: string
    state: string
    currentExecutionId: string
    executionIds: string[]
    executions: ExecutionSnapshot[]
    waitConditionIds: string[]
    deliveryIds: string[]
    controlRequestIds: string[]
}

export type ExecutionSnapshot = {
    id: string
    reason: string
    status: string
    outcome: string
    failureCode: string
    createdAt: string
    startedAt: string
    settledAt: string
}

export type AgentMessage = {
    sequence: number
    at: string
    executionId: string
    role: string
    content: string
}

export type AgentHistoryItem = {
	kind: 'message' | 'execution'
	sequence: number
	at: string
	message?: AgentMessage
	execution?: ExecutionSnapshot
}

export type AgentOutputEvent = {
	kind: 'text_delta' | 'settled'
	agentId: string
	executionId: string
	text: string
}

export type ReasoningOption = {
    supported: boolean
    levels: string[]
    default: string
}

export type ModelOption = {
    id: string
    label: string
    providerLabel: string
    reasoning: ReasoningOption
    defaultProfiles: string[]
}

type SendInputRequest = {
	agentId: string
	requestId: string
	content: string
	modelId: string
	reasoning: string
	sandboxMode: string
    approvalMode: string
    revision: string
}

type SendInputResponse = {executionId: string; existingRequest: boolean; activationError?: string}
type ControlRequest = {id: string; agentId: string; kind: 'pause' | 'close'}
type ControlResponse = {requestId: string; status: string}

type AppBinding = {
	Readiness(): Promise<HealthSnapshot>
	ListModels(): Promise<ModelOption[]>
	ListSessions(): Promise<SessionSummary[]>
    CreateProject(request: CreateProjectRequest): Promise<CreateProjectResponse>
    CreateSession(request: CreateSessionRequest): Promise<CreateSessionResponse>
    GetSession(sessionID: string): Promise<SessionSnapshot>
    GetAgent(agentID: string): Promise<AgentSnapshot>
    ListAgentMessages(agentID: string): Promise<AgentMessage[]>
    ListAgentHistory(agentID: string): Promise<AgentHistoryItem[]>
    SendInput(request: SendInputRequest): Promise<SendInputResponse>
    RequestControl(request: ControlRequest): Promise<ControlResponse>
}

export class BindingUnavailableError extends Error {
    constructor() {
        super('binding unavailable')
        this.name = 'BindingUnavailableError'
    }
}

type WailsWindow = Window & {
    go?: {app?: {App?: AppBinding}}
}

function binding(): AppBinding {
    const app = (window as WailsWindow).go?.app?.App
    if (!app) {
        throw new BindingUnavailableError()
    }
    return app
}

export function isBindingAvailable() {
    return Boolean((window as WailsWindow).go?.app?.App)
}

export function loadReadiness() {
	return binding().Readiness()
}

export function listModels() {
    return binding().ListModels().then(normalizeModelCatalog)
}

function normalizeModelCatalog(value: unknown): ModelOption[] {
    if (!Array.isArray(value)) {
        throw new Error('The model catalog response is invalid.')
    }
    return value.map((model) => {
        if (!isRecord(model) || !isRecord(model.reasoning)) {
            throw new Error('The model catalog response is invalid.')
        }
        const reasoning = model.reasoning
        if (typeof reasoning.supported !== 'boolean') {
            throw new Error('The model catalog response is invalid.')
        }
        const levels = reasoning.levels == null && !reasoning.supported
            ? []
            : stringArray(reasoning.levels)
        return {
            id: stringValue(model.id),
            label: stringValue(model.label),
            providerLabel: stringValue(model.providerLabel),
            reasoning: {
                supported: reasoning.supported,
                levels,
                default: stringValue(reasoning.default),
            },
            defaultProfiles: stringArray(model.defaultProfiles),
        }
    })
}

function isRecord(value: unknown): value is Record<string, unknown> {
    return typeof value === 'object' && value !== null
}

function stringValue(value: unknown): string {
    if (typeof value !== 'string') {
        throw new Error('The model catalog response is invalid.')
    }
    return value
}

function stringArray(value: unknown): string[] {
    if (!Array.isArray(value) || !value.every((item) => typeof item === 'string')) {
        throw new Error('The model catalog response is invalid.')
    }
    return value
}

export function listSessions() {
    return binding().ListSessions()
}

export function createProject(request: CreateProjectRequest) {
    return binding().CreateProject(request)
}

export function createSession(request: CreateSessionRequest) {
    return binding().CreateSession(request)
}

export function getSession(sessionID: string) {
    return binding().GetSession(sessionID)
}

export function getAgent(agentID: string) {
    return binding().GetAgent(agentID)
}

export function listAgentMessages(agentID: string) {
    return binding().ListAgentMessages(agentID)
}

export function listAgentHistory(agentID: string) {
    return binding().ListAgentHistory(agentID)
}

export function sendInput(request: SendInputRequest) {
    return binding().SendInput(request)
}

export function requestControl(request: ControlRequest) {
    return binding().RequestControl(request)
}

export function subscribeAgentOutput(listener: (event: AgentOutputEvent) => void) {
	if (!isBindingAvailable()) {
		return () => {}
	}
	return EventsOn(AGENT_OUTPUT_EVENT_NAME, (value: unknown) => {
		const event = parseAgentOutputEvent(value)
		if (event) {
			listener(event)
		}
	})
}

function parseAgentOutputEvent(value: unknown): AgentOutputEvent | null {
	if (!isRecord(value) || typeof value.agentId !== 'string' ||
		typeof value.executionId !== 'string' || typeof value.text !== 'string') {
		return null
	}
	if (value.kind !== 'text_delta' && value.kind !== 'settled') {
		return null
	}
	return {
		kind: value.kind,
		agentId: value.agentId,
		executionId: value.executionId,
		text: value.text,
	}
}
