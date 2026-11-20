package applypatch

const maxPatchBytes = 512 * 1024

const Name = "apply_patch"

const Description = "在已授权的项目目录内应用一个或多个文件补丁。" +
	"patch 必须使用 *** Begin Patch、*** Update File、*** Add File、*** Delete File 和 *** End Patch 格式；" +
	"path 可选，省略时按项目根目录解析。所有目标文件会先完成格式、内容和写入范围校验，" +
	"任一目标无效时不会写入文件。"

const InputSchema = `{
  "type": "object",
  "properties": {
    "patch": {
      "type": "string",
      "minLength": 1,
      "maxLength": 524288,
      "description": "使用 apply_patch 格式描述的文件补丁"
    },
    "path": {
      "type": "string",
      "description": "补丁目标根目录，可以是绝对路径或相对于项目根目录的路径；省略时使用项目根目录"
    }
  },
  "required": ["patch"],
  "additionalProperties": false
}`
