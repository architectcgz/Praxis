import type { AgentHistoryItem, AgentHistoryPage } from '../../api'

export function historyItemKey(item: AgentHistoryItem): string {
    return item.message ? `message:${item.sequence}` : `${item.kind}:${item.task?.id}`
}

/** 最新页与已加载记录之间出现跨度时补齐中间页，旧页游标不会越过未读取记录。 */
export async function loadLatestHistory(
    agentId: string,
    current: AgentHistoryItem[],
    readPage: (id: string, before?: string) => Promise<AgentHistoryPage>,
): Promise<AgentHistoryPage> {
    let page = await readPage(agentId)
    const keys = new Set(current.map(historyItemKey))
    const items = [...page.items]
    while (current.length > 0 && page.nextCursor && !page.items.some((item) => keys.has(historyItemKey(item)))) {
        page = await readPage(agentId, page.nextCursor)
        items.unshift(...page.items)
    }
    return { items, nextCursor: page.nextCursor }
}
