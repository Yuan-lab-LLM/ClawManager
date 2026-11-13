---
name: "ClawManager"
description: "采用 Fluent 2 Web 设计语言的 Agent 管理与协作平台；2026-11-12 批准的目标规范"
colors:
  primary: "#0f6cbd"
  focus: "#0f6cbd"
  primary-hover: "#115ea3"
  primary-pressed: "#0c3b5e"
  primary-selected: "#0f548c"
  on-primary: "#ffffff"
  selected: "#ebf3fc"
  ink: "#242424"
  text: "#424242"
  muted: "#616161"
  disabled: "#bdbdbd"
  canvas: "#f5f5f5"
  surface: "#ffffff"
  subtle: "#fafafa"
  hover: "#f5f5f5"
  pressed: "#e0e0e0"
  border: "#d1d1d1"
  border-subtle: "#e0e0e0"
  border-accessible: "#616161"
  success: "#0e700e"
  success-background: "#f1faf1"
  warning: "#bc4b09"
  warning-background: "#fff9f5"
  error: "#b10e1c"
  error-background: "#fdf3f4"
  data-blue: "#2587ed"
  data-blue-text: "#176fc6"
  data-blue-fill: "#e5f2ff"
  data-cost: "#c45300"
  data-busy: "#008d8b"
  data-idle: "#16a561"
  data-sleeping: "#149ec6"
  data-starting: "#c17f10"
  data-failed: "#e65161"
  data-offline: "#8391a3"
  data-unknown: "#697789"
  heat-0: "#ebedf0"
  heat-1: "#ace5bd"
  heat-2: "#46ad72"
  heat-3: "#216e39"
typography:
  brand:
    fontFamily: "\"Segoe UI\", -apple-system, BlinkMacSystemFont, \"PingFang SC\", \"Microsoft YaHei\", sans-serif"
    fontSize: "16px"
    fontWeight: 600
    lineHeight: 1.375
  title:
    fontFamily: "\"Segoe UI\", -apple-system, BlinkMacSystemFont, \"PingFang SC\", \"Microsoft YaHei\", sans-serif"
    fontSize: "16px"
    fontWeight: 600
    lineHeight: 1.375
  subtitle:
    fontFamily: "\"Segoe UI\", -apple-system, BlinkMacSystemFont, \"PingFang SC\", \"Microsoft YaHei\", sans-serif"
    fontSize: "20px"
    fontWeight: 600
    lineHeight: 1.3
  page-title:
    fontFamily: "\"Segoe UI\", -apple-system, BlinkMacSystemFont, \"PingFang SC\", \"Microsoft YaHei\", sans-serif"
    fontSize: "24px"
    fontWeight: 600
    lineHeight: 1.3333333333333333
  body:
    fontFamily: "\"Segoe UI\", -apple-system, BlinkMacSystemFont, \"PingFang SC\", \"Microsoft YaHei\", sans-serif"
    fontSize: "14px"
    fontWeight: 400
    lineHeight: 1.4285714285714286
  body-strong:
    fontFamily: "\"Segoe UI\", -apple-system, BlinkMacSystemFont, \"PingFang SC\", \"Microsoft YaHei\", sans-serif"
    fontSize: "14px"
    fontWeight: 600
    lineHeight: 1.4285714285714286
  label:
    fontFamily: "\"Segoe UI\", -apple-system, BlinkMacSystemFont, \"PingFang SC\", \"Microsoft YaHei\", sans-serif"
    fontSize: "12px"
    fontWeight: 400
    lineHeight: 1.3333333333333333
  metric:
    fontFamily: "\"Segoe UI\", -apple-system, BlinkMacSystemFont, \"PingFang SC\", \"Microsoft YaHei\", sans-serif"
    fontSize: "32px"
    fontWeight: 600
    lineHeight: 1.25
rounded:
  control: "4px"
  panel: "8px"
  dialog: "12px"
  small: "2px"
  circular: "10000px"
spacing:
  micro: "2px"
  tight: "4px"
  control-gap: "8px"
  cell: "12px"
  group: "16px"
  section: "20px"
  content: "24px"
  wide: "32px"
  major: "48px"
components:
  button-primary:
    backgroundColor: "{colors.primary}"
    textColor: "{colors.on-primary}"
    typography: "{typography.body}"
    rounded: "{rounded.control}"
    padding: "0 12px"
    height: "32px"
  button-primary-hover:
    backgroundColor: "{colors.primary-hover}"
  button-primary-pressed:
    backgroundColor: "{colors.primary-pressed}"
  button-secondary:
    backgroundColor: "{colors.surface}"
    textColor: "{colors.ink}"
    typography: "{typography.body}"
    rounded: "{rounded.control}"
    padding: "0 12px"
    height: "32px"
  field:
    backgroundColor: "{colors.surface}"
    textColor: "{colors.ink}"
    typography: "{typography.body}"
    rounded: "{rounded.control}"
    padding: "0 12px"
    height: "32px"
  navigation-item:
    textColor: "{colors.text}"
    typography: "{typography.body}"
    rounded: "{rounded.control}"
    padding: "0 12px"
    height: "40px"
  navigation-item-selected:
    backgroundColor: "{colors.selected}"
    textColor: "{colors.primary}"
    rounded: "{rounded.control}"
  panel:
    backgroundColor: "{colors.surface}"
    rounded: "{rounded.panel}"
    padding: "16px"
  table-row:
    typography: "{typography.body}"
    textColor: "{colors.text}"
    height: "44px"
  shell-sidebar:
    backgroundColor: "{colors.surface}"
    width: "224px"
  shell-header:
    backgroundColor: "{colors.surface}"
    height: "48px"
  agent-type-icon:
    size: "32px"
---

# Design System: ClawManager

## Overview

**设计方向：Fluent 2 Web · 清晰、轻盈、有层次的 Agent 工作台。**

自 **2026-11-12** 起，CM 整体采用 [Microsoft Fluent 2](https://fluent2.microsoft.design/) 设计语言。适用于个人 Dashboard、实例与工作台、团队与对话、资源、技能、设置、Console、Runtime 及管理员页面，以及所有共享控件和浮层。通过明确的信息层级、稳定的操作位置和有意义的可视化，让用户快速继续工作并掌握 Agent 的运行情况。

本文是**已确定的改造目标**，不是当前线上界面的实现报告。本次更新规范，不代表 Fluent UI 依赖已安装、所有页面已迁移或已部署。原方案 08「标准企业表格」及其对比图、实施计划保留为历史资料；发生冲突时，以本文为准，不继续从旧方案引入新视觉规则。

采用 Fluent 2 的视觉基础、交互语义和可访问性要求，结合 CM 已确定的产品布局。保留现有路由、API、权限、数据刷新、实例生命周期、Lite/Pro 边界和团队工作流。桌面 Web 使用默认 Web 蓝色品牌体系；保留 CM 原始彩色 Logo 和运行时真实品牌图标。

**规范来源与实现边界：**

- 基础规范以 Fluent 2 官方的颜色、字体、布局、形状、层级、动效、图标和可访问性文档为依据，各节附直接来源。查阅日期：2026-11-12。
- 精确 Web Token 对照 Microsoft 官方 [brandWeb 色阶](https://raw.githubusercontent.com/microsoft/fluentui/master/packages/tokens/src/global/brandColors.ts)和[浅色语义映射](https://raw.githubusercontent.com/microsoft/fluentui/master/packages/tokens/src/alias/lightColor.ts)。后续实施应固定所采用的依赖版本，升级时复核映射，不能从上游 master 自动改写产品主题。
- 侧栏宽度、页面密度、Dashboard 编排、图表配色和现有响应断点是 **CM 的产品约定**，不声称它们是微软统一要求。frontmatter 是目标 Token 的主来源；`.impeccable/design.json` 补充映射、动效、阴影和文档预览。

## Colors

用中性色组织信息，用 Web 蓝表达交互，用语义色表达反馈。图表可以鲜明，页面外壳保持安静。Fluent 2 区分中性、品牌及共享颜色，并强调颜色的语义用途。[官方颜色规范](https://fluent2.microsoft.design/color)

### 品牌与中性色

| CM Token | Fluent Web 对应 | 用途 |
| --- | --- | --- |
| `primary` | `colorBrandBackground` / `colorBrandForeground1` | 主要按钮、激活指示；具体前景/背景按组件语义选择 |
| `primary-hover` / `primary-pressed` / `primary-selected` | `colorBrandBackgroundHover` / `Pressed` / `Selected` | 主要按钮状态；不以统一透明度代替 |
| `selected` | `colorBrandBackground2` | 浅蓝选中背景；结合指示条、字重或勾选表达状态 |
| `ink` / `text` / `muted` | `colorNeutralForeground1` / `2` / `3` | 标题与主数据、正文、辅助信息 |
| `surface` / `subtle` / `canvas` | `colorNeutralBackground1` / `2` / `3` | 工作面、弱分区、页面画布 |
| `hover` / `pressed` | `colorNeutralBackground1Hover` / `Pressed` | 中性控件交互反馈 |
| `border` / `border-subtle` | `colorNeutralStroke1` / `2` | 面板边界、表格分隔，不独自承担控件识别 |
| `border-accessible` | `colorNeutralStrokeAccessible` | 需要明确边界的输入与交互元素 |
| `disabled` | `colorNeutralForegroundDisabled` | 真正不可用的控件，不用于正常辅助文字 |

前景链接宜使用 `primary-hover` 对应的 `colorBrandForegroundLink`；背景与前景 Token 即使当前颜色相同也保留语义。颜色通过公共主题入口消费，禁止各页复制散落的十六进制值。Token 组织遵循全局值与语义别名分层，先提供浅色主题，为后续深色和高对比主题留出映射入口；不因此宣称已有主题切换。[官方 Design Tokens](https://fluent2.microsoft.design/design-tokens)

### 反馈色与数据色

反馈色使用 `success`、`warning`、`error` 及各自浅背景，分别对应 Fluent 浅色主题的成功、警告、危险前景与背景。它们用于消息、校验、状态标签；必须同时提供文字或图标。[官方状态映射](https://raw.githubusercontent.com/microsoft/fluentui/master/packages/tokens/src/statusColorMapping.ts)、[浅色状态别名](https://raw.githubusercontent.com/microsoft/fluentui/master/packages/tokens/src/alias/lightColorPalette.ts)

以下是 **CM 数据可视化扩展**，保留用户已确认的鲜明图表方向；它们不是微软默认状态 Token：

| 数据含义 | Token | 配合表达 |
| --- | --- | --- |
| 费用历史曲线 | `data-cost` | 独立右轴、虚线与费用币种，不把不同币种合并 |
| Token 历史主曲线 | `data-blue` / `data-blue-fill` | 曲线、浅面积、当前值、日期与单位 |
| 图表蓝色数值 | `data-blue-text` | 可读数值，避免直接用浅曲线色写小字 |
| 正在执行任务 / 在线空闲 | `data-busy` / `data-idle` | 分开计数，不能把所有运行实例都视为执行中 |
| 休眠 / 启动中 | `data-sleeping` / `data-starting` | 状态图例、数量和筛选入口 |
| 故障 / 离线 / 未知 | `data-failed` / `data-offline` / `data-unknown` | 独立标签与原因；未知不能归为正常 |
| 个人活动日历 | `heat-0` → `heat-3` | 灰 → 浅绿 → 中绿 → 深绿，配范围图例和日期详情 |

资源表盘采用绿、橙、红区间突出接近容量上限的风险，同时显示数值、单位和可读的范围说明。阈值必须对应实际配额及告警策略，不把视觉分段伪装为后端告警规则。状态图、费用图和容量图有不同单位，不共用一根含糊坐标轴。

常规文字对背景至少 4.5:1，大字至少 3:1；识别控件、焦点或理解图表所必需的图形至少 3:1。浅面积和热力图浅格仅作辅助，提供文字、图例及可访问数据；若某个彩色标记单独承担含义，需调整深度或加高对比轮廓。禁止只靠红绿区分结果。[官方可访问性规范](https://fluent2.microsoft.design/accessibility)

## Typography

采用系统无衬线字体栈，优先 Segoe UI，按平台回退至系统字体，中文使用 PingFang SC / Microsoft YaHei；不额外下载或捆绑商业字体。正文以 14px / 20px 为基准，标题采用 600 字重。以下层级对照 [Fluent 2 Web 字体规范](https://fluent2.microsoft.design/typography)，实际 Token 在 frontmatter 中定义。

| CM 角色 | 字号 / 行高 | 用途 |
| --- | --- | --- |
| `label` | 12 / 16 | 辅助说明、时间、图例；不作为全站正文 |
| `body` / `body-strong` | 14 / 20 | 正文、表格、按钮、字段与导航；主数据可用 600 字重 |
| `title` / `brand` | 16 / 22 | 卡片、区域标题、品牌名 |
| `subtitle` | 20 / 26 | 欢迎语、对话或复杂弹窗标题 |
| `page-title` | 24 / 32 | 确有层级需要的独立详情标题 |
| `metric` | 32 / 40 | Dashboard 当前值和重要总量，配单位及时间范围 |

管理概览的页面名称在顶栏表达，正文不重复大标题；区域和弹窗保留语义标题，页面仍有正确的可访问名称。不能用全局 10–12px 字号挤出一屏效果。数值使用 `font-variant-numeric: tabular-nums`，单位和小数位一致；代码、终端、路径等内容按实际需要使用等宽字体，不把普通标签全部等宽化。

## Layout

使用顶栏、左侧导航和铺满可用宽度的主工作区。基于 Fluent 2 的 **4px 基础间距**建立节奏，2px 仅用于图标和边线微调；常用间距为 4 / 8 / 12 / 16 / 20 / 24 / 32 / 48px。[官方布局规范](https://fluent2.microsoft.design/layout)

### CM 外壳与密度

| 项目 | 默认值 | 使用要求 |
| --- | --- | --- |
| 顶栏 | 高 48px | 页面位置、必要的全局操作、账户菜单 |
| 侧栏 | 展开 224px；折叠 64px | 折叠按钮保持在侧栏与内容交界，保留状态持久化 |
| 页面内边距 | 桌面 24px；窄屏 16px | 不限制成居中的窄阅读列 |
| 面板间距 / 内边距 | 16px / 16px | 同行卡片外边框顶底对齐，标题包含在同一布局层级 |
| 控件 | 默认高 32px；紧凑 24px；大号 40px | 同组同高；紧凑模式不能牺牲字号、焦点和点击区域 |
| 导航项 | 高 40px | 图标、文本、选中指示保持稳定位置 |
| 表格行 | 默认 44px；密集 36px | 页面统一选择密度，不按内容随意变化 |

操作工具栏保持紧凑，桌面优先单行；搜索默认图标入口，展开后有清晰输入与关闭行为。搜索、筛选与批量创建各自归属明确；窄屏允许次要项收起，主要创建/提交动作保持可达。管理概览不新增刷新按钮或“更新于”标签，继续使用已有自动同步，错误时提供重试。

### Dashboard 信息结构

沿用用户已确认的个人 Dashboard 结构，用 Fluent 2 的表面、字阶和控件统一表达，不复制 GitHub 个人页或 Windows 桌面布局。

1. **欢迎区**：头像与简短欢迎语，用户名使用更大字级、强调字重及品牌前景色，和欢迎语分开；不显示任务计数或辅助解释。
2. **首行运行概览**：Token 与费用用双色双轴平滑曲线同时显示，保留当前数字及币种，不能切换后才看到另一类数据；资源用 CPU、内存、Workspace 存储三个同时可见的表盘；Agent 状态用足够大的环图 + 中心“运行数量 / 总数量” + 各状态数字。三类信息分别成区，不做切换才能看到的混合卡片。
3. **继续工作与置顶 Agent**：最近真实使用的 Agent 和记录来源；优先可操作的进行中、失败待处理、新完成任务。置顶最多展示六个入口，类型图标 32px。两侧面板整体等高、顶底边框对齐，不能仅对齐标题文本。
4. **个人活动**：年度活动热力图，灰与分级绿色表示强度；日期可查看实际活动，范围和计数口径一致。

桌面 1440×900、1280×800 以一屏容纳主要概览为目标，通过去重、控制条数、局部展开和合理留白实现。保留重要图表的面积、可读字体及入口。长列表、详细事件和完整历史进入详情；窄屏、浏览器缩放、字体放大允许自然纵向滚动，禁止用页面 `overflow: hidden` 裁掉内容来验收“一屏”。

### 响应式与内容边界

外壳沿用 768px 响应边界；团队工作区现有 1248px / 1540px **容器阈值**继续按其嵌入行为处理。这些是 CM 兼容约定，不是 Fluent 的统一断点。Dashboard 按可用容器重排，不压缩成细小图表。表格仅在自身容器横向滚动，页面外壳不横向溢出。

表格单元格始终单行：名称与 ID 同行，状态与短原因同行；长内容省略并提供键盘可达的完整信息入口。团队对话和用户正文正常换行，不将单行规则扩展到聊天内容。移动端触控目标以至少 44px 为 CM 目标，图标本身可更小，但点击区域不能相互覆盖。

## Elevation & Depth

用表面明度、边界和少量阴影表达层级。常规表格和静态分区保持清晰平稳；可交互卡片有轻微抬升，弹出层明显高于工作面。Fluent 2 使用环境光和方向光组合的六级阴影。[官方层级规范](https://fluent2.microsoft.design/elevation)

| Token | CM 用途 |
| --- | --- |
| `shadow2` | 可交互卡片的基础层，可按页面需要选用 |
| `shadow4` | 卡片悬停与轻浮起 |
| `shadow8` | 小型 Popover、局部浮动工具 |
| `shadow16` | 菜单、选择列表等浮层 |
| `shadow28` | 覆盖式侧抽屉 |
| `shadow64` | 模态 Dialog |

精确 CSS 见 sidecar，依据官方[阴影组合](https://raw.githubusercontent.com/microsoft/fluentui/master/packages/tokens/src/utils/shadows.ts)和浅色主题的环境光/方向光值；不要用不同的透明度冒充同一个 Token。模态遮罩为 `rgba(0,0,0,0.4)`。卡片不必全部套阴影，不能靠不断加深阴影制造信息优先级。浮层以不透明可读表面为默认，不将 Mica、Acrylic、全屏玻璃模糊或装饰性渐变当作 Fluent Web 的必要条件。

## Shapes

常用控件 4px、面板 8px、Dialog 12px，头像为圆形；小标签可用 2px。圆角与元素尺度一致，禁止所有容器统一变成胶囊。对应 React Token 分别为 `borderRadiusMedium`、`borderRadiusXLarge`、`borderRadius2XLarge`；注意 React 的 `borderRadiusLarge` 实际是 6px。[官方形状规范](https://fluent2.microsoft.design/shapes)、[React 圆角 Token](https://raw.githubusercontent.com/microsoft/fluentui/master/packages/tokens/src/global/borderRadius.ts)

图形轮廓、分隔线和焦点边界保持细而明确。常规边线 1px，选中指示和焦点可以更强；高对比模式使用系统颜色，不依赖阴影和浅背景区分结构。

## Components

### 公共实现策略

继续使用 React、TypeScript、React Router、React Query、现有状态管理与 Tailwind/CSS。后续逐步采用或封装 [Fluent UI React v9](https://fluent2.microsoft.design/components/web/react) 的语义 Token 与合适控件；接入前核对当前 React 版本、主题 Provider、Portal、样式顺序与现有调用方。规范更新不直接安装依赖，不重写业务状态机，也不引入另一套通用表格配置平台。优先改公共主题与组件，再迁移页面。

### 操作、输入和选择

- 同一命令组只设一个明显的 Primary 动作；其余使用 Secondary、Subtle 或 Transparent。导航使用 Link，切换操作表达选中状态。保留 Hover、Pressed、Focus、Disabled、Loading，不能用统一 opacity 处理所有状态。[Button](https://fluent2.microsoft.design/components/web/react/core/button/usage)
- 表单字段有持久标签、说明、错误位置与可访问关联，Placeholder 不代替标签。默认 32px，错误同时显示说明和可理解的修复路径。选择表单值用 Select / Combobox；Menu 用于立即执行的命令或导航，不能混淆语义。[Menu](https://fluent2.microsoft.design/components/web/react/core/menu/usage)
- 只给原生 select 外框上色不能算完成弹出列表统一。需要可控浮层时使用经过验证的组件；保留原生实现时如实说明平台差异，不能破坏原生键盘操作。
- `--cm-focus` 默认引用品牌蓝，高对比模式引用系统 `Highlight`。键盘焦点采用清晰的双层轮廓，例如 2px 品牌蓝色轮廓配 2px 表面色隔离层；在不同背景下均可见。复合搜索框在外层统一显示蓝色焦点轮廓，内部输入不叠加黑色或第二层轮廓；键盘焦点仍须清晰可见。输入激活下划线不能替代 `:focus-visible`。禁用控件有真实 disabled 语义；正常文字不使用禁用灰。

### 导航、表格和卡片

侧栏采用中性图标与浅蓝选中表面，选中态增加指示或字重，折叠后保留 Tooltip 与可访问名称。菜单、Popover、抽屉和页面共用同一主题与字体。

表格默认保留原生语义；仅在确实需要单元格导航等丰富交互时采用 DataGrid，并完整实现相应键盘模型。排序、筛选、分页、批量选择和权限行为保持真实。可点击行中的复选框、链接和菜单不互相劫持事件。

卡片围绕一个任务或对象组织，标题、内容和底部操作位置稳定。Agent 卡片优先展示身份、状态和继续入口，避免堆叠 Runtime、套餐、ID 和说明文字。整卡可点击时，内部菜单与置顶操作保持独立语义，不嵌套交互元素。[Card](https://fluent2.microsoft.design/components/web/react/core/card/usage)

### 图标、头像和品牌

系统导航及动作以 Fluent System Icons 为目标：常规态使用 Regular，选中或强调态按语义使用 Filled；默认 20px，紧凑行可用 16px，重要操作可用 24px。迁移期间现有 Lucide 可暂留，已迁移的同类组件统一图标体系，不在同一工具栏混用不同视觉重量。[官方图标规范](https://fluent2.microsoft.design/iconography)

**Agent 类型使用真实品牌图标**，不替换为通用显示器或系统图标：OpenClaw、Hermes、OpenCode、DeepSeek Harness 等复用现有资产与类型映射；继续工作与置顶区域均用 32px 容器，保留原始宽高比及颜色，不滤镜染蓝、不裁切 Logo。未知类型有清晰回退和可访问名称。品牌图与系统图标承担不同角色，二者可以共存。

### 图表、状态与数据真实性

Token 和费用曲线有时间范围、轴/单位、悬停与键盘可达的数据详情；当前值与选定时间范围一致。费用区分币种，区分实际账单与估算。曲线缺测保留断点或“暂无数据”，不补零制造趋势。 用量卡片独立切换时间范围，只请求用量接口；保留其他区域的 DOM、交互状态和数据，局部加载时保留原图并提供短暂过渡，失败在卡片内重试。双轴采用蓝色 Token 实线与橙色费用虚线，平滑曲线经过真实样本且不超出相邻值范围。主卡片移除采集状态、统计口径和底部解释段落，保留日期、单位、币种、明确的状态标签以及错误；不完整费用以“小计”字段名表达，估算值用 ≈ 标明。必要的来源和口径放在详情中。

资源表盘同时显示 CPU、内存、Workspace 存储的已用 / 可用、单位和百分比；标题明确是配额占用、分配容量还是实际使用率。不能将分配量当实测消耗，不能把实例配额汇总称为集群物理容量。获取失败或暂不支持明确显示未知，不显示 0%。

Agent 环图中心显示“运行数量 / 总数量”，下方标注“运行 / 总数”；运行数量合计忙碌、空闲与已确认运行三类，总数量使用完整状态聚合，不受列表展示条数限制。中心保持单行；空数据为 `0 / 0`，统计不可用为 `— / —`。配完整图例与各状态数量，可进入相应列表。运行状态与任务状态分开：运行中不等于正在执行任务，休眠不等于失败；任务进行中、新完成与失败依赖真实事件或可靠信号。继续工作按真实使用记录排序，不按创建时间伪造最近使用。

活动热力图按个人身份、日期和时区统计，提供强度图例、日期详情及键盘替代入口。图表同时提供简短文字概览或数据表，不能让 SVG/Canvas 成为唯一的数据入口。异步同步时保持布局、图例顺序和焦点稳定，不自动重排用户正在操作的卡片。

### 消息、弹窗与工作台

MessageBar 展示当前页面或区域内需要处理的状态，错误有恢复动作；Toast 用于短暂完成反馈，不能成为失败信息的唯一存留位置。[MessageBar](https://fluent2.microsoft.design/components/web/react/core/messagebar/usage)

Dialog 管理焦点、Escape 与关闭后的焦点返回；危险动作说明对象与后果。长内容局部滚动，主操作可达；避免嵌套模态。非阻断说明使用 Popover，附属详情可用 Drawer。[Dialog](https://fluent2.microsoft.design/components/web/react/core/dialog/usage)、[Drawer](https://fluent2.microsoft.design/components/web/react/core/drawer/usage)

团队对话、实例工作台、编辑器、终端及 Console 沿用各自信息结构，用共享外壳、工具栏和焦点规则连接；不能为统一卡片外观破坏长会话、代码阅读、流式输出或终端尺寸。个人数据与平台管理数据遵循现有权限范围，不能混合呈现为用户拥有的资源。

实例工作台顶部保留可重命名的实例名称和生命周期状态，去掉返回按钮及类型、模式、Runtime、Workspace 大小、更新时间的元信息行。名称内联编辑，Enter/保存提交，Escape/取消返回；失败保留输入，保存仅更新名称，不重启实例。Share Link 旁提供技能管理按钮，在弹窗内管理；启动/停止、重启、配置环境变量并重启、删除收进“运行管理”菜单，删除仍需确认。不在工作台底部显示技能折叠面板或 Session Token Usage。桌面采用“实例列表 / Agent / 工作区”三栏，默认比例 **1.2:6.8:2**；两个分隔条支持拖动与键盘调宽，调整宽度不重载 Agent 会话。实例列表沿用 Fluent 中性表面和选中蓝，使用真实类型图标、名称、状态、服务端搜索与分页，可直接切换当前用户有权访问的实例。实例状态由原生类型图标表达，行背景保持中性：运行中原图轻呼吸；停止灰化并在一次熄灭动效后静止；故障保留静止的原图，仅放大、缩小红色三角感叹号，不晃动图标、不加背后红圈；休眠采用睡姿、Zz 和缓慢呼吸。动效尊重减少动态效果并在离屏、页面隐藏时暂停。当前选中项保留柔和的蓝色渐变高亮与 1px 半透明渐变轮廓；状态保留可访问名称和悬停文字。实例列表标题栏仅保留列表隐藏按钮，不放重复的全屏或展开管理按钮；隐藏后在 Agent 标题栏的实例状态后提供恢复列表按钮，与名称、状态同行，不单独占用一行；按钮不依赖原生页面加载或实例运行状态，Agent 全屏时仍可达，恢复列表同时退出全屏并将焦点返回列表标题栏，保留列表位置。实例列表顶部第一项为“创建实例”，第二项为“实例管理”，随后显示搜索和真实实例；空列表时主窗口显示创建提示及按钮，右上角不放创建入口。切换时使用短时、账户与查询隔离的无凭据列表快照消除空列表，并保留后台同步；详情、原生页面内容及工作区使用短暂淡入，尊重减少动态效果设置。原生文档隔离保持有效。当前实例已删除时清理该账户的导航快照，转到剩余可访问且非删除中的实例；没有剩余实例则在主窗口显示创建提示。元数据 404 的恢复不等待状态请求，服务错误保留重试路径。批量删除部分失败时，在管理窗口列出实例名称与原因并保留失败项选择，不用原生 alert 仅展示汇总数量。隐藏工具区后保留实例列表并将剩余空间分给 Agent；恢复时保持各栏最小宽度。窄屏用弹窗选择实例，Agent 与工具上下排列，保持可用高度。实例或账户切换必须隔离旧请求、弹窗、分享凭据和编辑状态，保留浏览器预览的文档隔离机制：普通运行、休眠、停止和故障实例切换不因访问元数据触发整页重载，仅实际打开 credentialless 预览时绑定当前实例的文档分区；已使用预览的文档切换到其他实例时仍建立新分区。

### 统一创建实例

创建采用单页表单和右侧实时预览，不再使用三步向导。Agent 类型按当前已启用镜像和平台分发能力平铺，默认提供 OpenClaw、Hermes、OpenCode、DeepSeek Harness；镜像自动选择，不展示镜像选择字段。运行模式下方左侧提供“批量创建”开关；批量仅支持 Lite，显示名称前缀、数量、起始序号。负责人和描述选填。Lite 隐藏资源与初始化配置；Pro 展示 CPU、内存、存储，保留配额、GPU 等既有专用能力。

高级配置与实例信息采用同级标题、相同左边界；导入配置是展开后的第一项，单按钮选择文件，选中后显示文件名并可更换、移除；导入能力遵循实际 Runtime 支持范围。环境变量用多行 KEY=value，提供多项示例与错误定位，右侧使用 Key/Value 两列表格展示并遮蔽敏感值。技能从当前账号可用列表勾选，支持检索和取消，已选项同步右侧；正式流程不显示示例技能。部分失败保留每项真实结果和已经创建的实例入口；后续导入失败只重试导入，不重新创建成功实例。

### 动效与可访问性

动效帮助理解变化：控件反馈 100–150ms，卡片/图表局部变化 150–200ms，抽屉和 Dialog 200–300ms。使用 Fluent duration 与 curve Token，具体值见 sidecar；避免夸张弹跳、全页依次入场、常驻呼吸灯及数据数字不停翻动。[官方动效规范](https://fluent2.microsoft.design/motion)、[duration](https://raw.githubusercontent.com/microsoft/fluentui/master/packages/tokens/src/global/durations.ts)、[curve](https://raw.githubusercontent.com/microsoft/fluentui/master/packages/tokens/src/global/curves.ts)

图表初次显现或数据变化可短暂过渡；仪表盘指针只随真实数据变化，不循环摆动。`prefers-reduced-motion: reduce` 关闭位移、描边和数值滚动等非必要动画，直接呈现最终值；后台或不可见区域暂停持续动画，不延迟交互。

所有交互提供键盘路径、可见焦点、正确名称与合理的焦点返回。支持文本放大 200% 和页面缩放后的重排，在等效 320px 宽度下内容可用；保留必要二维数据的局部滚动。`forced-colors` 下边界、焦点和状态仍可辨识。加载、空态、权限不足、错误和长内容均是正式状态，不以预览常量代替。

### 迁移与验收

迁移顺序：公共 Token 与外壳 → 按钮/表单/菜单/浮层 → 表格与管理页 → Dashboard 与工作台 → 团队及管理员边界场景。每批页面完成后检查公共调用方，再继续扩展。旧页面尚未迁移不构成新页面沿用旧样式的理由；详细任务由后续实施计划安排，不把历史 08 计划当作 Fluent 2 迁移完成清单。

实际改 UI 时检查 1440、1280、768、390 宽度，补充 320px 重排与用户实际大屏；Dashboard 核对 1440×900、1280×800。覆盖中英文与日/韩/德长文本、加载/空/错误/禁用/长名称、键盘/焦点返回、缩放、减少动态效果、真实数据同步和关键工作流。文档校验不能替代后续页面的浏览器验收。

构建与部署继续遵循仓库 AGENTS：本地仅准备、校验和传输源码及部署脚本；应用构建、容器镜像及所需二进制在 **10.130.81.54** 执行，不占用用户本机进行构建。

## Do's and Don'ts

- **Do** 将本文和公共主题作为 CM 整体 UI 的统一依据，使用 Fluent 2 的语义层级及完整交互状态。
- **Do** 保留彩色品牌、真实 Agent 类型图标和鲜明而可读的数据图表，让颜色服务于操作和信息。
- **Do** 保留顶部位置导航、紧凑工具栏、单行表格、侧栏边缘折叠按钮及已有自动同步。
- **Do** 让 Token、资源和 Agent 状态分别表达；让继续工作与置顶面板整体对齐。
- **Do** 明确数据范围、单位、币种与缺测状态，保留业务权限和真实状态语义。
- **Don't** 继续混用旧 08、Ant Design Pro 或任意页面自建的主题 Token；历史材料不作为新的视觉权威。
- **Don't** 把 Fluent 简化成换蓝色、全屏玻璃、紫色主题或大面积装饰性渐变。
- **Don't** 将真实 Logo 换成通用图标，或为了密度把正文缩小、裁掉内容、把重要环图缩成装饰。
- **Don't** 在管理概览重复大标题、设计解释、刷新按钮或更新时间；不要把批量创建藏进筛选。
- **Don't** 使用仅靠颜色、仅鼠标可达、失去焦点或没有文字替代的数据图表。
- **Don't** 把已更新的规范、文档预览或待迁移组件写成已经上线的实现。
