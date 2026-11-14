# go-static-checker

## 用途

读取 TOML 规则，检查 Go 项目的分层依赖、源码格式、注释和文件位置。支持单模块项目，格式与注释规则可包含测试源码，输出文本或 JSON 报告。

## 运行

需要 Go 1.27.3 或更高版本。

```sh
go build -o bin/go-static-checker ./cmd/go-static-checker
./bin/go-static-checker -config rules.toml
./bin/go-static-checker -config rules.toml -format json
```

仓库内的 `rules.toml` 用于工具自身的 CI 检查。复制配置后，修改 `project.root` 和规则即可检查其他项目。`root` 相对于配置文件所在目录，也支持绝对路径，必须包含 `go.mod`。CI 请运行编译后的程序，`go run` 不会原样传递程序的退出码。

## 配置

除注明为可选的字段外，示例字段均须显式填写。未知字段、缺失字段、规则类型不适用的字段和无效枚举值均报错。`schema_version = 1` 的分层配置保持兼容；新增规则使用独立的参数表。

### 拆分规则文件

主文件可以用顶层 `includes` 引用多个规则文件。命令行仍使用 `-config rules.toml`，工具合并所有规则后只加载一次项目。

主文件示例：

```toml
schema_version = 1
includes = ["rules/format.toml", "rules/layout.toml"]

[project]
root = "."
include_tests = false
build_tags = []
```

`rules/format.toml` 示例：

```toml
schema_version = 1

[[rules]]
id = "FMT001"
kind = "source_format"
description = "限制源码行宽"
severity = "error"

[rules.format]
packages = ["..."]
max_line_length = 120
tab_width = 4
file_name_pattern = ""
package_name_pattern = ""
```

`rules/layout.toml` 示例：

```toml
schema_version = 1

[[rules]]
id = "LAYOUT001"
kind = "file_layout"
description = "必须包含根目录说明文件"
severity = "error"

[rules.layout]
required_files = ["README.md"]
required_directories = []
```

每份文件都必须声明 `schema_version = 1`。只有主文件可以声明 `[project]`；被引用文件填写 `[[rules]]`，也可以通过自己的顶层 `includes` 继续引用其他文件。`includes` 必须写在表定义之前，避免意外成为 `[project]` 或规则表中的字段。

`includes` 中的相对路径基于声明它的文件所在目录，也支持绝对路径。通过符号链接引用文件时，相对路径基于链接所在目录。规则里的包路径仍基于模块，文件路径仍基于主文件的 `project.root`；把规则移到子目录不会改变检查范围。工具不自动搜索子目录配置，也不展开通配符或环境变量。

加载顺序为当前文件的规则在先，再按 `includes` 数组顺序递归加载。规则 ID 必须全局唯一，没有覆盖或优先级机制。重复引用（包括规范化路径和符号链接指向同一文件）、循环引用、缺失文件及无效配置都会返回退出码 `2`，错误包含文件路径和引用链。

单个文件可以只负责引用其他文件，但合并后至少需要一条规则。省略 `includes` 或设为 `[]` 时，保留原有单文件用法。

### 分层依赖

```toml
schema_version = 1

[project]
root = "."
include_tests = false
build_tags = []

[[rules]]
id = "ARCH001"
kind = "layer_dependencies"
description = "业务层只能依赖领域层"
severity = "error"
unmatched = "error"
allow_same_layer = true
allow_stdlib = true
external_default = "deny"

[[rules.layers]]
name = "service"
packages = ["internal/service/..."]
allow = ["domain"]
allow_external = []

[[rules.layers]]
name = "domain"
packages = ["internal/domain/..."]
allow = []
allow_external = []
```

`packages` 匹配模块相对包路径；`allow_external` 匹配完整导入路径。支持精确路径、表示模块根包的 `.`、匹配所有包的 `...`，以及匹配某个包和子包的 `/...` 后缀。匹配按路径段区分，`service/...` 不匹配 `servicekit`。不支持其他通配符。

每个包必须归属一层。未归层包产生 `unclassified_package` 诊断，涉及它的依赖边跳过层级判定；同时继续检查其他包。匹配多个层属于配置错误。

同层依赖由 `allow_same_layer` 控制。跨层依赖必须出现在来源层的 `allow` 中，授权不会传递。标准库由 `allow_stdlib` 控制。外部依赖匹配 `allow_external` 后放行，其余由 `external_default` 的 `allow` 或 `deny` 决定。

`severity` 支持 `error` 和 `warning`。`unmatched` 当前仅支持 `error`，表示未归层必须产生诊断；诊断严重程度由规则的 `severity` 决定。

### 自定义源码格式

将下面的规则追加到同一配置文件。`source_format` 可以单独使用，也可以与分层规则组合。

```toml
[[rules]]
id = "FMT001"
kind = "source_format"
description = "限制行宽和命名"
severity = "error"

[rules.format]
packages = ["internal/...", "cmd/..."]
max_line_length = 120
tab_width = 4
file_name_pattern = '[a-z][a-z0-9_]*\.go'
package_name_pattern = '[a-z][a-z0-9]*'
```

| 字段 | 语义 |
|---|---|
| `packages` | 非空的模块相对包路径列表，匹配语义与分层规则一致；`["..."]` 检查全部包 |
| `max_line_length` | 最大行宽，非负整数；`0` 关闭行宽检查 |
| `tab_width` | 制表位间隔，必须为正整数；例如 `4` 表示 Tab 前进到下一个 4 的倍数位置 |
| `file_name_pattern` | 文件基本名的 Go 正则，包含 `.go` 扩展名；空字符串关闭检查 |
| `package_name_pattern` | `package` 声明名称的 Go 正则；空字符串关闭检查 |

所有字段必须填写，且至少启用一项检查。正则始终匹配整个名称，无须手动添加首尾锚点。TOML 单引号字符串中的反斜杠不需要转义。正则无效会返回退出码 `2`。

行宽按 Unicode 码点计数，中文字符计为 1，组合字符分别计数；这不是终端显示宽度。Tab 按制表位展开，LF 和 CRLF 行尾不计入行宽。注释、字符串和生成代码同样检查。每个超长行报告一次，定位到第一个超限字符。诊断列号是从 1 开始的 UTF-8 字节位置，与 Go 源码位置一致。

文件名问题定位到 `1:1`，包名问题定位到各文件的包名标识符。格式诊断使用真实文件行列，忽略 `//line` 的位置重映射。默认只检查当前构建环境选中的生产文件，可用下面的可选字段包含测试源码。工具只读取源码，不自动修复；本规则不检查 `gofmt`、函数名、类型名或变量名。

### 声明间空行

以下规则要求相邻函数、方法和结构体声明之间至少有一行空行，包含 `_test.go`。独立配置这条规则，可避免将其他格式约束同时施加到测试文件。

```toml
[[rules]]
id = "FMT002"
kind = "source_format"
description = "相邻函数、方法和结构体声明之间至少保留一行空行"
severity = "error"

[rules.format]
packages = ["..."]
max_line_length = 0
tab_width = 4
file_name_pattern = ""
package_name_pattern = ""
min_blank_lines_between_declarations = 1
include_tests = true
```

`min_blank_lines_between_declarations` 为可选的非负整数，省略或设为 `0` 时关闭空行检查。只检查文件顶层相邻的函数、方法、结构体声明，以及 `type (...)` 内相邻的结构体声明。包含结构体的类型声明组在顶层也参与检查。其他声明会中断相邻关系。结构体字段、函数体内的局部类型和匿名结构体不参与检查。

空行须位于上一声明末尾与下一声明的文档注释之间。只有空格或 Tab 的行也计为空行；注释行和块注释内部的空白行不计入。支持 LF 和 CRLF。违规定位到后一声明的真实行列，诊断代码为 `missing_declaration_blank_line`。

`rules.format.include_tests` 为可选布尔值，默认 `false`。设为 `true` 后，仅当前格式规则增加对匹配包中的测试源码的检查，包括外部测试包和只有测试文件的包。`packages` 仍匹配所属目录对应的模块相对包路径，诊断的 `source_package` 也使用这个所属包路径。测试文件由 `go list` 按当前平台与构建标签选择，同一文件只计数一次。

测试源码只解析语法，不加载测试依赖图或生成测试入口。分层规则仍只检查生产代码，`project.include_tests` 仍须为 `false`。测试文件是否可编译由 `go test` 验证；选中测试源码的解析失败会返回退出码 `2`。

### 必需文件与目录

```toml
[[rules]]
id = "LAYOUT001"
kind = "file_layout"
description = "项目必须包含说明文档和入口目录"
severity = "error"

[rules.layout]
required_files = ["README.md", "cmd/server/main.go"]
required_directories = ["internal/domain", "internal/service"]
```

两个数组必须显式填写，且合计至少包含一条路径。路径相对于 `project.root`，使用 `/` 分隔。只支持精确路径，拒绝绝对路径、`..`、通配符、重复路径和同一路径同时要求为文件与目录。必需文件须为普通文件，必需目录须为目录，空目录也合法。此规则声明必需项，不禁止其他文件或目录。

文件结构检查不受构建标签影响。路径不存在或类型不符会产生规则诊断，行列为 `0`。权限等读取错误会返回退出码 `2`。符号链接可指向项目内部，指向项目外部会导致检查未完成。所有规则仍共享项目加载前置检查：目标必须是可加载的单模块 Go 项目。

### 注释预算、说明和格式

完整配置见根目录 [rules.toml](rules.toml) 中的 `COMMENT001` 至 `COMMENT004`。每种规则使用独立的 `[rules.comments]`，所有行为字段必须显式填写。

| 规则类型 | 检查内容 |
|---|---|
| `comment_budget` | 每个选中声明和 package 的注释行数预算 |
| `required_comments` | 选中声明是否有紧邻的非空说明 |
| `comment_format` | 声明说明的名称前缀、块内注释格式和同行注释 |
| `comment_line_length` | 含注释的完整物理行是否超过指定列 |

共同字段为 `packages`、`include_tests`、`include_generated` 和 `exclude_go_directives`。包路径沿用分层规则的匹配语义。测试源码按构建条件选取；生成代码按 Go 的 `Code generated ... DO NOT EDIT.` 标记识别。排除 Go 指令时，`//go:`、`//line`、`/*line` 和旧式 `// +build` 不参与预算或说明检查。仓库的列宽规则仍检查指令行。

前三种规则还要求 `declarations` 和 `visibility`。声明类型支持 `function`、`method`、`struct` 和 `interface`；可见性支持 `all`、`exported` 和 `unexported`。只有预算规则支持 `package`，且必须使用 `visibility = "all"`。函数、方法和类型按文件顶层声明检查；局部类型和匿名函数计入外层声明。

预算使用 `count_unit = "physical_lines"`、`denominator = "comment_and_code_lines"` 和 `rounding = "ceil"`。令 C 为注释行数，K 为有效代码行数，实际允许值为：

```text
max(min_allowed_comment_lines, ceil((C + K) * max_comment_percent / 100))
```

仓库配置为 20%，向上取整且至少允许 1 行。C 不超过允许值即通过。因此，3 行代码加 1 行注释通过，3 行代码加 2 行注释失败。取整和最低额度允许实际占比超过 20%，最低额度不要求必须写注释。

注释按物理行去重，块注释内部空行也计入。有效代码行排除空白、纯注释和仅含 `exclude_delimiter_only_lines` 中分隔符的行。代码与注释同行时，两类各计 1 行；字符串中的注释标记不计为注释。声明说明计入对应声明，字段说明计入所属结构体或接口。package 汇总同一导入路径的选中文件，含文件头和声明说明；外部测试包单独汇总。各块独立检查，不因 package 通过而豁免声明。

`required_comments` 目前只支持 `position = "doc"`。`require_nonempty = true` 要求说明含非空文本，块内或同行注释不能替代声明说明。

`comment_format` 的 `doc.style` 和 `body.style` 目前只支持 `line`，即双斜杠注释。`doc.require_name_prefix` 检查首行是否以声明名称开头；`require_text_after_name` 进一步要求名称后有空白和非空说明。字段说明按块内注释处理。`body.line_pattern` 对去掉标记和一个可选空格后的文本做整串正则匹配。`trailing.allowed = false` 禁止声明内部及结束位置的同行注释。

`comment_line_length` 使用 `positions` 选择 `doc`、`body`、`trailing` 和 `file`。仓库选择全部位置，`max_column = 80`，第 80 列可用。宽度包括缩进、标记和同一行代码。`column_metric` 目前只支持 `unicode_codepoints`；`tab_width` 控制制表位，CRLF 不计入宽度。每行只报告一次，诊断定位使用 UTF-8 字节列号。

### Markdown 位置

`file_policy` 根据扩展名检查文件位置，使用 `[rules.files]`。仓库的 `DOC001` 仅允许根目录 `README.md`，子目录中的 README、`docs`、`vendor` 和其他隐藏目录中的 Markdown 都会违规。仅 `.git` 目录树被排除。

`extensions` 选择扩展名，`extension_case_sensitive = false` 同时匹配 `.md` 和 `.MD`。路径始终精确区分大小写。`allowed_paths` 放行单个相对路径，`allowed_directories` 放行目录树，`excluded_directories` 跳过目录树。目录按路径段匹配，`docs` 不匹配 `docs-old`。三个数组都必须填写，可以为空。

目前只支持 `default = "deny"` 和 `follow_directory_symlinks = false`。扫描相对于 `project.root`，不受 Go 构建标签影响。不遍历目录符号链接；文件链接自身或最终目标的扩展名匹配时，按链接所在位置检查。断链、权限等读取错误返回退出码 `2`。此规则控制允许位置，必需文件仍由 `file_layout` 指定。

## 报告和退出码

| 退出码 | 含义 |
|---|---|
| 0 | 检查完成，没有 error 级违规 |
| 1 | 检查完成，存在 error 级违规 |
| 2 | 配置错误、项目加载失败或检查未完成 |

JSON 报告包含 `environment`、`build_tags`、`packages`、`files` 和 `diagnostics`。每条诊断包含规则 ID、稳定诊断代码、严重程度、消息、文件行列、来源及目标包和层名。文件路径相对于项目根目录。包级诊断的文件为空、行列为 0。

| 规则类型 | 诊断代码 |
|---|---|
| 分层依赖 | `forbidden_layer_dependency`、`forbidden_external_dependency`、`forbidden_stdlib_dependency`、`unclassified_package` |
| 源码格式 | `line_too_long`、`invalid_file_name`、`invalid_package_name`、`missing_declaration_blank_line` |
| 文件结构 | `missing_required_file`、`missing_required_directory`、`invalid_required_file`、`invalid_required_directory` |
| 注释 | `comment_budget_exceeded`、`missing_required_comment`、`invalid_comment_format`、`comment_line_too_long` |
| 文件位置 | `forbidden_file_location` |

工具按文件、位置、规则 ID 和诊断代码排序，便于 CI 比较。格式诊断的目标包和层名为空；文件结构诊断的来源包也为空。运行失败时错误写入 stderr，不输出成功报告。

## 检查边界

- 按当前 Go 工具链的 GOOS、GOARCH、CGO_ENABLED 和配置中的构建标签选择源码。报告记录实际环境。
- 分层规则不检查 `_test.go`，`project.include_tests = true` 会报错。格式和注释规则分别用 `rules.format.include_tests`、`rules.comments.include_tests` 选择测试源码。注释规则可排除生成代码，其他源码规则照常检查。
- 关闭 workspace 联动。项目目录下发现嵌套 `go.mod` 即报错，扫描跳过 `.git` 和 `vendor`。
- 根据加载器提供的模块身份识别外部依赖，支持本地 `replace`。只检查本模块的直接导入，不递归约束第三方内部依赖。
- cgo 的 `C` 导入跳过，不检查 C 侧依赖。
- 使用只读模块加载，不自动修改目标项目的 `go.mod`。依赖需可下载或已缓存。
- 解析有效源码以获取导入位置，不做完整类型检查。请另行运行 `go test` 和 `go vet`；本工具不能证明代码可编译，也不覆盖其他构建环境。

项目加载使用 [go/packages](https://pkg.go.dev/golang.org/x/tools/go/packages)，配置解析使用 [BurntSushi/toml](https://pkg.go.dev/github.com/BurntSushi/toml)。

## 开发和后续迭代

```sh
go test -race ./...
go vet ./...
go run ./cmd/go-static-checker -config rules.toml
```

当前已支持分层依赖、自定义源码格式、注释约束、文件位置和必需文件结构。后续 PR 可增加测试依赖检查、多模块支持和基于类型信息的调用规则。新增规则类型前，先定义配置语义、诊断代码和正反例。
