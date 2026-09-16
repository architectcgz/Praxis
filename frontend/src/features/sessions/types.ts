export type StreamingOutput = {
    executionId: string
    content: string
}

export type PendingUserMessage = {
    requestId: string
    executionId: string
    sessionId: string
    agentId: string
    content: string
    at: string
}
