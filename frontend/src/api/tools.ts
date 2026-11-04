export type BashArguments = {
    command: string
    path: string
    timeout?: number
}

export type ReadFileArguments = {
    path: string
    offset?: number
    limit?: number
}

export type ApplyPatchArguments = {
    patch: string
    path: string
}

/** 内置工具准备后的参数契约；原始模型请求不保证符合该结构。 */
export type ToolCallInput =
    | { name: 'bash'; arguments: BashArguments }
    | { name: 'read_file'; arguments: ReadFileArguments }
    | { name: 'apply_patch'; arguments: ApplyPatchArguments }

/** 后端已完成参数校验，这里只负责将冻结 JSON 转换为对应工具的参数类型。 */
export function parseToolCallInput(name: string, argumentsJSON: string): ToolCallInput {
    switch (name) {
        case 'bash': return { name, arguments: JSON.parse(argumentsJSON) as BashArguments }
        case 'read_file': return { name, arguments: JSON.parse(argumentsJSON) as ReadFileArguments }
        case 'apply_patch': return { name, arguments: JSON.parse(argumentsJSON) as ApplyPatchArguments }
        default: throw new Error(`Unsupported tool: ${name}`)
    }
}
