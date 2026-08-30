import { getSystemBinding } from './bindings'

export type StartupIssue = {
    code: string
    path?: string
    message: string
}

export type HealthSnapshot = {
    ready: boolean
    issue?: StartupIssue
}

export function loadReadiness() {
    return getSystemBinding().Readiness()
}
