import type { HistoryMessage, ModelRequestActivity } from '../../../api'

export function mergeMessages(current: HistoryMessage[], incoming: HistoryMessage[]): HistoryMessage[] {
    if (!incoming.length) return current
    return [...new Map([...current, ...incoming].map((message) => [message.id, message])).values()]
}

export type RequestActivities = ReadonlyMap<string, ModelRequestActivity>

/** 分页可能先读到结果，再读到更早的开始事实。 */
export function mergeRequests(current: RequestActivities, incoming: ModelRequestActivity[]): RequestActivities {
    if (!incoming.length) return current
    const requests = new Map(current)
    for (const request of incoming) {
        const previous = requests.get(request.requestId)
        if (!previous || request.historyEnd > previous.historyEnd) requests.set(request.requestId, request)
    }
    return requests
}
