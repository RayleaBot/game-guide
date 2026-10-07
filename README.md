# 游戏攻略

RayleaBot 官方插件 · `raylea.game-guide`

查询《崩坏：星穹铁道》角色攻略图。发送角色名后，插件从米游社检索攻略帖，把图按来源顺序合并转发给当前会话。

## 功能

- 按角色正式名或别名查询攻略图，例如「昔涟」「饮月」「鸭鸭」
- 发送「角色列表」查看当前已适配角色
- 首次查询会提示正在获取；同一角色后续查询优先使用本地缓存
- 攻略图以合并转发发送，一张图一条
- 未收录的名字仍会按你输入的关键词去米游社搜索

## 安装

本插件独立发布，不随 RayleaBot 主程序打包。安装后默认停用，需要在插件列表里**启用**才会响应命令。

使用前请确认：

- 机器人能访问米游社（`bbs-api.miyoushe.com` 等）
- 协议适配器支持合并转发
- 角色列表图片依赖 RayleaBot 的图片渲染环境

### 插件商店

1. 打开 Web 管理面，进入 [插件商店](https://github.com/RayleaBot/RayleaBot/blob/main/docs/user/management-surface.md)（`/plugins/store`）。
2. 找到 **游戏攻略**，安装与当前系统匹配的版本。
3. 安装前确认：插件会作为本机原生程序运行。
4. 到 **插件列表**（`/plugins`）启用 `raylea.game-guide`。

### 本地安装包

也可以在插件列表中安装本仓库 [GitHub Release](https://github.com/RayleaBot/game-guide/releases) 里对应平台的 ZIP：

| 平台 | 资源 |
| --- | --- |
| Windows x64 | `windows-x64` |
| Linux x64 | `linux-x64` |
| macOS arm64 | `macos-arm64` |

## 使用方法

命令前缀以管理面 **插件设置** 为准。以下示例使用 `*` 和 `/`；使用 `*` 或全角 `＊` 时，需要在命令前缀中配置对应字符。

| 命令 | 权限 | 说明 |
| --- | --- | --- |
| `*角色名攻略` | 所有人 | 按角色名或别名查询攻略图 |
| `/游戏攻略 角色名` | 所有人 | 同上 |
| `*角色列表` | 所有人 | 查看当前已适配角色 |

```text
你：*昔涟攻略
机器人：收到，正在获取「昔涟」攻略图，请稍候…
机器人：（合并转发，内含攻略图）

你：/游戏攻略 饮月
机器人：（丹恒•饮月 的攻略图）

你：*角色列表
机器人：（已适配角色一览图）
```

常用别名可以直接用，例如：

| 你发送 | 实际角色 |
| --- | --- |
| `*饮月攻略` | 丹恒•饮月 |
| `*鸭鸭攻略` | 布洛妮娅 |
| `*大黑塔攻略` | 大黑塔 |
| `*火主攻略` | 开拓者•存护 |
| `*忘归人攻略` | 忘归人 |

完整名单以 `*角色列表` 为准。角色表会随版本更新。

### 查询过程

1. 机器人先回复「正在获取」。
2. 本地已有该角色缓存时，直接发送缓存图。
3. 没有缓存时，按角色名和别名在米游社搜索攻略帖，下载图片后缓存再发送。
4. 找不到图时回复「没有找到「角色名」的星穹铁道攻略图。」

单次最多汇总 4 个来源、合计 120 张图；超过 100 张时拆成多条合并转发。

## 说明

- 攻略图来自米游社公开帖，内容由原作者发布，插件不做攻略对错判断。
- 角色名写在「攻略」前面，例如 `*昔涟攻略`。只发 `*攻略` 不会查询。
- 别名匹配忽略大小写和多余空白。中英文、常见错字和花名能匹配到内置表时，会按正式名去搜。
- 第一次查询依赖外网，可能需要等待；之后同一角色通常更快。
- 若提示发送失败，检查合并转发是否可用，以及机器人是否被禁言或限流。
- 角色列表图片发不出来时，检查渲染环境；攻略图本身不走渲染，走合并转发。

## 开发

插件以独立 Go 模块和独立 GitHub Release 发布。角色数据编译进后端，角色列表模板由统一构建器自动发现并进入 artifact 精确清单。

### 目录结构

```text
game-guide/
  cmd/game-guide/                    进程入口
  internal/plugin/                   角色检索、米游社拉取、缓存和发送
  internal/assets/characters.json    角色正式名、slug 与别名
  templates/character-list/          角色列表渲染模板
  info.json                          manifest v4 与命令声明
```

### 本地联调

1. 将本仓库路径写入 RayleaBot 根目录下被 Git 忽略的 `plugin-workspace.local.json`：

```json
{
  "workspace_version": "2",
  "plugins": [
    {
      "path": "../RayleaBotPlugins/game-guide"
    }
  ]
}
```

2. 在 **RayleaBot 主仓库根目录** 启动：

```powershell
$env:RAYLEA_PLUGIN_DEV = "watch"
$env:RAYLEA_SERVER_RELOAD = "watch"
.\start.bat
```

启动器会连接本地 Go SDK、构建当前平台 artifact，并通过离线 `plugin dev-sync` 同步到 `plugins/installed/`。

### 测试与构建

```powershell
go test -race ./...
$env:RAYLEA_PLUGIN_BUILD_USE_WORKSPACE = "1"
go run github.com/RayleaBot/RayleaBot/sdk/go/cmd/raylea-plugin build-go --plugin . --backend ./cmd/game-guide --target windows-x64 --out dist
```

### 发布

`v*` 标签对应的发布工作流使用固定 SDK 引用构建 Windows x64、Linux x64 和 macOS arm64 ZIP，并创建 GitHub Release。[plugin-catalog](https://github.com/RayleaBot/plugin-catalog) 定时读取当前 Release，校验 artifact 并记录各平台 ZIP 的 SHA-256。

本地联调与商店分发说明见 [插件商店与独立开发](https://github.com/RayleaBot/RayleaBot/blob/main/docs/plugin/store-and-development.md)。

## License

[MIT](./LICENSE)
