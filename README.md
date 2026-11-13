# go-static-checker

## 用途

读取 TOML 规则，检查 Go 项目的直接导入是否符合分层约束。首轮实现支持单模块生产代码，输出文本或 JSON 报告。

## 运行

需要 Go 1.27.3 或更高版本。

```sh
go build -o bin/go-static-checker ./cmd/go-static-checker
./bin/go-static-checker -config rules.toml
./bin/go-static-checker -config rules.toml -format json
```

仓库内的 `rules.toml` 检查工具自身的 CLI、检查器和配置层。复制配置后，修改 `project.root` 和各层规则即可检查其他项目。`root` 相对于配置文件所在目录，必须包含 `go.mod`。CI 请运行编译后的程序，`go run` 不会原样传递程序的退出码。

## 配置

所有示例字段均须显式填写。空数组合法；未知字段、缺失字段和无效枚举值均报错。

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

## 报告和退出码

| 退出码 | 含义 |
|---|---|
| 0 | 检查完成，没有 error 级违规 |
| 1 | 检查完成，存在 error 级违规 |
| 2 | 配置错误、项目加载失败或检查未完成 |

JSON 报告包含 `environment`、`build_tags`、`packages`、`files` 和 `diagnostics`。每条诊断包含规则 ID、稳定诊断代码、严重程度、消息、文件行列、来源及目标包和层名。文件路径相对于项目根目录。包级诊断的文件为空、行列为 0。

诊断代码包括 `forbidden_layer_dependency`、`forbidden_external_dependency`、`forbidden_stdlib_dependency` 和 `unclassified_package`。工具按文件、位置和规则排序，便于 CI 比较。运行失败时错误写入 stderr，不输出成功报告。

## 检查边界

- 按当前 Go 工具链的 GOOS、GOARCH、CGO_ENABLED 和配置中的构建标签选择源码。报告记录实际环境。
- 不检查 `_test.go`；`include_tests = true` 会报错。生成代码、空白导入、点导入和别名导入均检查。
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

首轮完成分层约束的完整检查流程。后续 PR 可依次增加测试包支持、多模块支持、文件布局规则和基于类型信息的调用规则。新增规则类型前，先定义配置语义、诊断代码和正反例。
