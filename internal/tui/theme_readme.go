package tui

// ThemeReadmeTemplate 返回写入 ~/.suna/themes/ 的主题模板。
// 用 README 而不是示例 .toml：示例文件会被当成有效主题加载并出现在选择列表里，
// 而 README 只提供可复制的模板，不产生副作用。
func ThemeReadmeTemplate() string { return themeReadmeTemplate }

const themeReadmeTemplate = "# Suna 主题\n" +
	"\n" +
	"把任意 `.toml` 文件放在本目录下即可新增主题，文件名（不含扩展名）就是主题名。\n" +
	"改完保存后，在 Suna 配置页打开 Theme 列表即可看到（无需重启）。\n" +
	"\n" +
	"## 颜色格式\n" +
	"\n" +
	"只接受 `#rrggbb`。只有十六进制才能反推出确定的 RGB，从而精确计算对比度；\n" +
	"ANSI 索引（`\"14\"` 这类）的真实 RGB 由终端主题决定，Suna 无法测量，因此不支持。\n" +
	"\n" +
	"主题会自动适配深色/浅色终端：与终端匹配的一侧**原样使用**，\n" +
	"不匹配的一侧只调整对比度不足的颜色（保留色相与色温）。\n" +
	"所以只需要按你自己的终端设计一套即可，不必写两份。\n" +
	"\n" +
	"## 字段\n" +
	"\n" +
	"必填 5 个语义色：\n" +
	"\n" +
	"- `accent`：主题主色。标题、光标、spinner、logo、标签底色。**最影响整体观感**。\n" +
	"- `success`：agent 消息、成功状态、diff 新增行。\n" +
	"- `info`：用户消息、输入框提示符。\n" +
	"- `warning`：工具名、Guard 警告。\n" +
	"- `error`：错误消息、diff 删除行。\n" +
	"\n" +
	"可选 4 个中性色，不写则按终端背景派生（推荐不写，派生值天然协调）：\n" +
	"\n" +
	"- `text`：正文（用户要读的内容）。\n" +
	"- `muted`：弱化文字（帮助、描述、元信息、工具详情正文）。\n" +
	"- `dim`：装饰（分隔线、边框、字形）。\n" +
	"- `surface`：表面背景（代码块）。\n" +
	"\n" +
	"## 模板一：深色终端（取自内置 default）\n" +
	"\n" +
	"```toml\n" +
	"[colors]\n" +
	"accent  = \"#ceb6f9\"   # 紫：品牌色\n" +
	"success = \"#a0cf6e\"   # 黄绿：agent 消息、成功\n" +
	"info    = \"#a7c1fa\"   # 蓝：用户消息、提示符\n" +
	"warning = \"#e4ba7e\"   # 橙黄：工具名、警告\n" +
	"error   = \"#faacba\"   # 粉红：错误、删除行\n" +
	"```\n" +
	"\n" +
	"## 模板二：VSCode Dark+ 风格（蓝 accent + 暖色系）\n" +
	"\n" +
	"工具感强、中性克制，适合习惯 VSCode 编辑器配色的用户。\n" +
	"\n" +
	"```toml\n" +
	"[colors]\n" +
	"accent  = \"#6cb6e8\"   # 蓝：品牌色\n" +
	"success = \"#5ad4bc\"   # 青绿：agent 消息、成功\n" +
	"info    = \"#9cdcfe\"   # 浅蓝：用户消息、提示符\n" +
	"warning = \"#dcdcaa\"   # 米黄：工具名、警告\n" +
	"error   = \"#f47c7c\"   # 鲑红：错误、删除行\n" +
	"\n" +
	"text    = \"#d4d4d4\"   # 亮灰：正文\n" +
	"muted   = \"#9a9a9a\"   # 中灰：弱化文字\n" +
	"dim     = \"#7a7a7a\"   # 深灰：装饰\n" +
	"surface = \"#252526\"   # 深灰：代码块背景\n" +
	"```\n" +
	"\n" +
	"## 说明\n" +
	"\n" +
	"- `accent` 决定主题的整体观感，建议选一个与终端背景对比充足的强调色。\n" +
	"- 色块上的文字色（工具标签、Guard 标签）由 Suna 按背景亮度自动计算，无需配置。\n" +
	"- 解析失败的主题会出现在列表里并标注原因，不影响其他主题和 Suna 启动。\n"
