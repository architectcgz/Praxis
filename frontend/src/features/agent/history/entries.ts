import type { Entry } from '../../../api'
export function mergeEntries(current: Entry[], incoming: Entry[]): Entry[] {
    return [...new Map([...current, ...incoming].map((entry) => [entry.id, entry])).values()]
}
