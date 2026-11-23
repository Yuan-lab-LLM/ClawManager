---
name: "ClawManager"
description: "ClawManager 的 Fluent 2 Web 默认外观、公共组件和 UI 验收规范"
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
  danger-action: "#c50f1f"
  danger-action-hover: "#b10e1c"
  danger-action-pressed: "#960b18"
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
    fontFamily: "'Segoe UI', 'Segoe UI Web (West European)', -apple-system, BlinkMacSystemFont, Roboto, 'Helvetica Neue', sans-serif"
    fontSize: "16px"
    fontWeight: 600
    lineHeight: 1.375
  title:
    fontFamily: "'Segoe UI', 'Segoe UI Web (West European)', -apple-system, BlinkMacSystemFont, Roboto, 'Helvetica Neue', sans-serif"
    fontSize: "16px"
    fontWeight: 600
    lineHeight: 1.375
  subtitle:
    fontFamily: "'Segoe UI', 'Segoe UI Web (West European)', -apple-system, BlinkMacSystemFont, Roboto, 'Helvetica Neue', sans-serif"
    fontSize: "20px"
    fontWeight: 600
    lineHeight: 1.4
  page-title:
    fontFamily: "'Segoe UI', 'Segoe UI Web (West European)', -apple-system, BlinkMacSystemFont, Roboto, 'Helvetica Neue', sans-serif"
    fontSize: "24px"
    fontWeight: 600
    lineHeight: 1.3333333333333333
  body:
    fontFamily: "'Segoe UI', 'Segoe UI Web (West European)', -apple-system, BlinkMacSystemFont, Roboto, 'Helvetica Neue', sans-serif"
    fontSize: "14px"
    fontWeight: 400
    lineHeight: 1.4285714285714286
  body-strong:
    fontFamily: "'Segoe UI', 'Segoe UI Web (West European)', -apple-system, BlinkMacSystemFont, Roboto, 'Helvetica Neue', sans-serif"
    fontSize: "14px"
    fontWeight: 600
    lineHeight: 1.4285714285714286
  label:
    fontFamily: "'Segoe UI', 'Segoe UI Web (West European)', -apple-system, BlinkMacSystemFont, Roboto, 'Helvetica Neue', sans-serif"
    fontSize: "12px"
    fontWeight: 400
    lineHeight: 1.3333333333333333
  metric:
    fontFamily: "'Segoe UI', 'Segoe UI Web (West European)', -apple-system, BlinkMacSystemFont, Roboto, 'Helvetica Neue', sans-serif"
    fontSize: "32px"
    fontWeight: 600
    lineHeight: 1.25
rounded:
  control: "4px"
  panel: "8px"
  dialog: "8px"
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
    typography: "{typography.body-strong}"
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
    typography: "{typography.body-strong}"
    rounded: "{rounded.control}"
    padding: "0 12px"
    height: "32px"
  button-danger:
    backgroundColor: "{colors.danger-action}"
    textColor: "{colors.on-primary}"
    typography: "{typography.body-strong}"
    rounded: "{rounded.control}"
    padding: "0 12px"
    height: "32px"
  button-danger-hover:
    backgroundColor: "{colors.danger-action-hover}"
  button-danger-pressed:
    backgroundColor: "{colors.danger-action-pressed}"
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
    typography: "{typography.body-strong}"
    rounded: "{rounded.control}"
  panel:
    backgroundColor: "{colors.surface}"
    rounded: "{rounded.panel}"
    padding: "16px"
  table-header:
    backgroundColor: "{colors.subtle}"
    textColor: "{colors.muted}"
    typography: "{typography.body-strong}"
    padding: "0 12px"
    height: "44px"
  table-row:
    backgroundColor: "{colors.surface}"
    textColor: "{colors.ink}"
    typography: "{typography.body}"
    padding: "0 12px"
    height: "44px"
  table-row-compact:
    backgroundColor: "{colors.surface}"
    textColor: "{colors.ink}"
    typography: "{typography.body}"
    padding: "0 8px"
    height: "36px"
  table-row-selected:
    backgroundColor: "{colors.selected}"
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

**默认外观为 Fluent 2 Web 浅色主题 `webLightTheme`，交互强调色为官方 Web 蓝。** 使用中性表面、清晰字阶、紧凑但可读的控件和分明的浮层，让用户快速继续工作并掌握 Agent 运行情况。

本文适用于个人 Dashboard、实例与工作台、团队与对话、资源、技能、设置、Console、Runtime 及管理员页面，以及所有共享控件和浮层。自 2026-11-12 起采用 Fluent 2 Web，2026-11-20 增补官方 React 组件基准。预览使用示例数据，页面实施状态以逐页验收记录为准。原方案 08「标准企业表格」及其对比图、实施计划归档。

### 规则与参考来源

| 决策 | 依据 |
| --- | --- |
| 页面结构、操作位置、信息密度与数据口径 | 按本文的 CM 规则执行。保留现有路由、API、权限、数据刷新、实例生命周期、Lite/Pro 边界和团队工作流。 |
| 组件变体、状态、字体、形状和浮层 | 设计参考 Fluent 2 Web UI Kit，代码按项目锁定的 Fluent UI React v9 组件及主题验收。局部样式按[已确认的外观例外](#已确认的外观例外)执行。 |
| 应用专有表面、图表和品牌 | 使用 CM 公共语义 Token、已批准的数据色及原始品牌资产。文档设置有差异时以本文为准。 |

新建及迁移页面必须执行本规范。旧页面的实现用于核对现有行为，视觉差异纳入迁移。设计稿和 PR 应注明共享组件、变体与主要状态。官方组件确有能力缺口时，在 PR 中记录缺口、对应公共实现和验证结果。稳定的系统级变更同步更新 `DESIGN.md` 和 `.impeccable/design.json`。

设计和开发先从[组件选型目录](#组件选型目录)确认组件及其适用场景，再查该组件的变体、状态、Token 与交互规则。组合成页面时参考[关键页面的组合规则](#关键页面的组合规则)，动效参数见[场景、时长与缓动](#场景时长与缓动)。

### 已确认的外观例外

**北向门户配色例外（2026-11-16 用户指定）：** `/ieisystem/*` 保留以 32443 为参考的蓝色门户、Runtime 类型色、淡紫 Team 标识及成员/成长语义色。通过北向路由的文档标记限定配色，覆盖该页面及其挂载到 body 的浮层；离开北向路由时移除。CM 管理端的公共 Fluent Token、布局和主题保持不变，原生 Runtime iframe 内的页面也不受这些样式影响。

**Team 对话样式例外（2026-11-17 用户指定）：** CM 与北向的 Team 对话采用已确认的“清润蓝青”局部配色，保留白色阅读区、旧版标题图标、22px 外框与 16–18px 面板圆角。桌面标题与 Team 编号同行，不重复展示在线人数和正常“运行中”标记；启动、停止、故障等必要反馈保留。文件入口放在同一标题栏右侧；窄屏允许标题元信息换行。成员健康待机状态显示“在线”，调度就绪显示“待命”，不改底层 idle 协议值或在线判定；成员栏标签与数量保持同一行。只在 Team 工作区及其成员详情、流程提示中定义局部变量，不改公共 Fluent Token、其他页面或原生 Runtime。保留原成员栏、分页、输入区、文件浏览/预览/编辑、历史定位、完整 Kanban 及 1248/1540px 容器阈值；同一份看板在缩略与完整形态之间切换，不能重复展示。渐变仅用于标题、选中和活动节点等局部强调，焦点轮廓保持可见。这一例外不改变 API、权限、数据与 Team 调度。

### Fluent 2 Web 样例索引

以下页码来自用户提供的《Microsoft Fluent 2 Web》PDF，更新记录截至 2025-09-25。设计稿沿用套件的组件、变体和变量。实现时对照同一组件的 React API 与浏览器渲染。[Microsoft Fluent 2 Web UI Kit](https://fluent2.microsoft.design/get-started/design)

第 3 页 Fluent Theme Designer 用于核对品牌和中性色阶。涉及新色板时，从种子色生成整组色阶，用同一组件状态样例验证对比度。第 4–54 页的组件样例都可从[组件选型目录](#组件选型目录)找到对应规则。

### Fluent 2 Web React 参考版本

组件规则核对使用 `@fluentui/react-components@9.75.2`、`@fluentui/react-icons@2.0.347` 和 `@fluentui/react-charts@9.3.29`。实际接入时以项目锁定并完成兼容性验证的版本为准。

## Colors

用中性色组织信息，用 Web 蓝表达交互，用语义色表达反馈。图表可以鲜明，页面外壳保持安静。Fluent 2 区分中性、品牌及共享颜色，并强调颜色的语义用途。[官方颜色规范](https://fluent2.microsoft.design/color)

### 主题与 Token 的使用顺序

1. **应用主题**通过根级 `FluentProvider` 注入 `webLightTheme`。接入时锁定已验证的版本，升级后复核 Token 和组件行为。[官方 React 接入方式](https://fluent2.microsoft.design/get-started/develop)
2. **官方组件**使用公开的 `appearance`、`size`、slot 和内建状态。CM 自有表面和共享控件通过公共 `--cm-*` 语义变量消费对应 Fluent alias。既有 CSS 保持使用同一组语义名，迁移时在公共样式层衔接。
3. **业务页面**使用共享组件及语义变量。颜色、描边、圆角、阴影和焦点由公共主题提供。自有图表、状态和品牌资源使用各自的公共语义。页面需要新的视觉角色时先确认共享组件或 Token 能否表达，再决定是否新增公共角色。

本文开头的 YAML 记录默认浅色主题和 CM 自有组件的对照值，`.impeccable/design.json` 补充别名、层级和组件参考版本。组件运行时以公开 API 和解析后的 Token 为准。页面样式中的固定色值、跨页面复制的主题变量，以及针对组件库内部类名的覆盖，均属于评审阻断项。

今后新增配色时在主题层提供完整色阶和前景／背景配对。新增设计体系时在公共组件层处理形状、字阶、状态和浮层差异。业务页面保持原有组件语义、路由与数据状态，运行时主题切换按独立功能验收。

### 品牌与中性色

| CM Token | Fluent Web 对应 | 用途 |
| --- | --- | --- |
| `primary` / `on-primary` | `colorBrandBackground` / `colorNeutralForegroundOnBrand` | 主按钮的背景及内容配对 |
| `primary-hover` / `primary-pressed` / `primary-selected` | `colorBrandBackgroundHover` / `colorBrandBackgroundPressed` / `colorBrandBackgroundSelected` | 品牌控件各自的悬停、按下与持续选中状态 |
| `selected` | `colorBrandBackground2` | 自有控件的浅蓝选中背景，结合指示条、字重或勾选表达状态 |
| 链接 / 品牌前景 | `colorBrandForegroundLink` / `colorBrandForeground1` | 分别用于链接和品牌色文字或图标 |
| 键盘焦点 | `colorStrokeFocus1` / `colorStrokeFocus2` | 优先用组件内建焦点。自有控件按相邻表面使用对应描边，强制色使用系统色 |
| `ink` / `text` / `muted` | `colorNeutralForeground1` / `2` / `3` | 标题与主数据、正文、辅助信息 |
| `surface` / `subtle` / `canvas` | `colorNeutralBackground1` / `2` / `3` | 工作面、弱分区、页面画布 |
| `hover` / `pressed` | `colorNeutralBackground1Hover` / `colorNeutralBackground1Pressed` | 中性控件交互反馈 |
| `border` / `border-subtle` | `colorNeutralStroke1` / `2` | 面板边界、表格分隔，不独自承担控件识别 |
| `border-accessible` | `colorNeutralStrokeAccessible` | 需要明确边界的输入与交互元素 |
| `disabled` | `colorNeutralForegroundDisabled` | 真正不可用的控件，不用于正常辅助文字 |

颜色通过公共主题入口取值，前景、表面、描边和状态各自使用对应语义别名。本阶段以 Web 浅色主题及系统强制色验收。`focus` 是 CM 自有样式的对照项，官方组件沿用内建的双层焦点描边。[官方 Design Tokens](https://fluent2.microsoft.design/design-tokens) · [Web 颜色别名](https://fluent2.microsoft.design/color-tokens/)

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

按 WCAG 2.2 AA 核对实际渲染色对。正常文字至少 4.5:1，大字至少 3:1，识别控件、焦点或理解图表所必需的图形至少 3:1。文字对比覆盖悬停、按下与选中等适用状态，真正禁用的控件按 WCAG 例外处理。浅面积和热力图浅格仅作辅助，提供文字、图例及可访问数据。独立承担含义的标记通过调整深度或增加高对比轮廓达到要求。[Fluent 可访问性规范](https://fluent2.microsoft.design/accessibility) · [WCAG 2.2](https://www.w3.org/TR/WCAG22/)

## Typography

字体继承 `webLightTheme` 的 `fontFamilyBase`，让系统为中文选择适合的回退字体。使用 Fluent Text 预设或对应的字号、行高和字重 Token。以 14px / 20px 正文为基准，用字阶区分主指标、区块标题和辅助信息。[Fluent 2 Web 字体规范](https://fluent2.microsoft.design/typography)

| CM 角色 | 字号 / 行高 | 用途 |
| --- | --- | --- |
| `label` | 12 / 16 | 辅助说明、时间、图例；不作为全站正文 |
| `body` / `body-strong` | 14 / 20 | 正文、表格、按钮、字段与导航；主数据可用 600 字重 |
| `title` / `brand` | 16 / 22 | 卡片、区域标题、品牌名 |
| `subtitle` | 20 / 28 | 对话或复杂弹窗标题 |
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
| 控件 | 默认高 32px，紧凑 24px，大号 40px | 同组同高。紧凑尺寸用于空间有限的桌面场景，触控目标单独按可达面积验收 |
| 导航项 | 高 40px | 图标、文本、选中指示保持稳定位置 |
| 表格行 | 默认 44px；密集 36px | 页面统一选择密度，不按内容随意变化 |

操作工具栏保持紧凑，桌面优先单行；搜索默认图标入口，展开后有清晰输入与关闭行为。搜索、筛选与批量创建各自归属明确；窄屏允许次要项收起，主要创建/提交动作保持可达。管理概览不新增刷新按钮或“更新于”标签，继续使用已有自动同步，错误时提供重试。

管理概览先呈现最常扫读的用量和实例指标，其余数据按趋势、资源、分布归组。使用 16px 区块标题、24–32px 主值和较轻的辅助文字。用组间间距区分区块，用浅分隔线组织同组内容。实例管理把查询工具、Lite/Pro 摘要和数据表按任务顺序纵向排列，保留单行数据和局部滚动。普通表面使用官方中性色，浮层保留组件自带的高度差。

个人设置入口位于右上角账户下拉菜单，与语言、控制台切换、退出登录同级，点击进入现有 `/settings` 页面。个人桌面侧栏及窄屏导航不重复展示设置入口；共享账户菜单在个人区和 Console 均提供该入口，Console 的 `/admin/settings` 系统设置保持独立。

版本号位于侧栏顶部 ClawManager 名称下方，与名称左对齐，不占用底部导航空间；保持原有 48px 品牌区高度。长版本号省略并保留完整构建信息的悬停入口，侧栏折叠时只显示 Logo；手机导航抽屉与 Console 共用同样的品牌及版本布局。

运行时概览工具栏右侧按“实例调度”主按钮、“…”更多菜单的顺序排列；不单列“Pod 维护”按钮，维护入口归入更多菜单。实例调度沿用既有能力、禁用条件和安全预检。

运行时槽位在正常启动预留、或已核实同一归属和代际的启动状态报告时差期间显示“启动”。该进度仅用于展示，继续保留原始对账、绑定确认和操作预检；未知、真实归属冲突及失败不伪装为启动。Skill Hub 正文直接从工作页签与列表开始，页面位置由顶栏表达。

### Dashboard 信息结构

沿用用户已确认的个人 Dashboard 结构，用 Fluent 2 的表面、字阶和控件统一表达，不复制 GitHub 个人页或 Windows 桌面布局。

1. **首行运行概览**：页面正文直接显示概览卡片。Token 与费用用双色双轴平滑曲线同时显示，保留当前数字及币种，不能切换后才看到另一类数据；资源用 CPU、内存、Workspace 存储三个同时可见的表盘；Agent 状态用足够大的环图 + 中心“运行数量 / 总数量” + 各状态数字。三类信息分别成区，不做切换才能看到的混合卡片。
2. **继续工作与置顶 Agent**：最近真实使用的 Agent 和记录来源；优先可操作的进行中、失败待处理、新完成任务。置顶最多展示六个入口，类型图标 32px。两侧面板整体等高、顶底边框对齐，不能仅对齐标题文本。
3. **个人活动**：年度活动热力图，灰与分级绿色表示强度；日期可查看实际活动，范围和计数口径一致。

桌面 1440×900、1280×800 以一屏容纳主要概览为目标，通过去重、控制条数、局部展开和合理留白实现。保留重要图表的面积、可读字体及入口。长列表、详细事件和完整历史进入详情；窄屏、浏览器缩放、字体放大允许自然纵向滚动，禁止用页面 `overflow: hidden` 裁掉内容来验收“一屏”。

### 响应式与内容边界

外壳沿用 768px 响应边界；团队工作区现有 1248px / 1540px **容器阈值**继续按其嵌入行为处理。这些是 CM 兼容约定，不是 Fluent 的统一断点。Dashboard 按可用容器重排，不压缩成细小图表。表格仅在自身容器横向滚动，页面外壳不横向溢出。

表格单元格始终单行：名称与 ID 同行，状态与短原因同行；长内容省略并提供键盘可达的完整信息入口。团队对话和用户正文正常换行，不将单行规则扩展到聊天内容。移动端触控目标以至少 44px 为 CM 目标，图标本身可更小，但点击区域不能相互覆盖。

## Elevation & Depth

用表面明度、边界和少量阴影表达层级。常规表格和静态分区保持清晰平稳；可交互卡片有轻微抬升，弹出层明显高于工作面。Fluent 2 使用环境光和方向光组合的六级阴影。[官方层级规范](https://fluent2.microsoft.design/elevation)

| Token | CM 用途 |
| --- | --- |
| `shadow2` | 自有轻浮表面的低层级 |
| `shadow4` | Fluent `Card appearance="filled"` 的默认阴影 |
| `shadow8` | 交互卡片悬停；Tooltip 使用同等级的双层 drop shadow |
| `shadow16` | Menu；默认 Popover 使用同等级的双层 drop shadow |
| `shadow28` | CM 自有的中等高度浮层 |
| `shadow64` | 模态 Dialog 和覆盖式 Drawer 的官方高度 |

自有表面使用 Fluent 阴影 Token。静态内容通过明度和分隔表达层次，悬浮和模态优先沿用组件阴影及 `colorBackgroundOverlay` 遮罩。PDF 第 18 页的阻断式 Dialog 使用 40% 默认遮罩，代码使用组件对应的主题 Token。工作面和浮层使用不透明中性色表面。

## Shapes

使用官方组件默认形状。常用控件为 `borderRadiusMedium`（4px），Card 按 `size` 应用 2 / 4 / 6px 圆角，Dialog 使用 `borderRadiusXLarge`（8px）。自有表面的圆角从同一组 Token 选取。[官方形状规范](https://fluent2.microsoft.design/shapes) · [React Card 默认样式](https://github.com/microsoft/fluentui/blob/master/packages/react-components/react-card/library/src/components/Card/useCardStyles.styles.ts) · [React Dialog 默认样式](https://github.com/microsoft/fluentui/blob/master/packages/react-components/react-dialog/library/src/components/DialogSurface/useDialogSurfaceStyles.styles.ts)

图形轮廓、分隔线和焦点边界保持细而明确。常规边线 1px，选中指示和焦点可以更强；高对比模式使用系统颜色，不依赖阴影和浅背景区分结构。

## Components

### 公共实现策略

新建或迁移的界面必须优先复用仓库已有的共享组件。共享组件选型以 **Fluent UI React v9** 为准，官方组件覆盖所需交互时，采用官方控件或对其封装。迁移既有共享控件时，保留调用方依赖的参数、事件、焦点、键盘行为和业务反馈。页面拥有权限、查询、路由和业务状态，组件负责呈现与输入。布局使用 React、Tailwind 或 CSS。官方组件缺少所需能力时，自有实现必须沿用同一主题、公开语义与交互状态，并以实际行为验收。[官方 React 组件目录](https://fluent2.microsoft.design/components/web/react)

接入时验证 React 和类型依赖兼容性，检查 Tailwind reset 与 Griffel 的样式顺序。将页面布局类放在 Provider 的子容器上。Portal 内的菜单、抽屉、弹窗、Tooltip 与 Toast 必须继承同一主题、字体及文字方向，并按正确层级展示。编辑器、终端和原生 Agent 嵌入面板通过各自支持的入口接收主题；外围样式迁移保留会话、文档隔离和工作区状态。

### 组件选型目录

下表覆盖 Fluent 2 Web UI Kit 的全部组件示例，并纳入 CM 常用的表格分页、文件选择、图表与工作台部件。各节以组件或变体为首列，列明适用任务和选型边界。设计稿与 PR 按实际场景标注组件、变体和状态。组件名也可能指 Figma 样式或 CM 组合模式，开发时以项目锁定版本的公开 API 为准。

| 组件类别 | 选型入口 | PDF 页码 |
| --- | --- | --- |
| 动作、操作组与链接 | [按钮与操作菜单](#按钮与操作菜单) | 10–13、26、52 |
| 字段、输入、数值、搜索与选择 | [搜索、表单与选择](#搜索表单与选择) | 16、21–25、35–37、39–40、43–44、48、50 |
| 列表、表格、数据网格与分页 | [表格、列表与数据网格](#表格列表与数据网格) | 17、27 |
| 导航、内容层级、展开与容器 | [导航、卡片与面板](#导航卡片与面板) | 4、9、14–15、19、31、45、54 |
| 图标、身份、状态标记与标签 | [图标、头像和品牌](#图标头像和品牌) | 5–8、32、42、46–47 |
| 浮层、通知、错误与加载 | [浮层、消息与加载](#浮层消息与加载) | 18、20、28–30、33–34、38、41、49、51、53 |
| 统计与数据可视化 | [图表、状态与数据真实性](#图表状态与数据真实性) | CM 扩展 |
| 对话、文件、编辑器、终端与原生 Agent | [工作台与嵌入内容](#工作台与嵌入内容) | CM 扩展 |

### 按钮与操作菜单

先按行为选择控件，再按动作优先级选择外观。同一命令组只放一个主动作。各操作等重要时使用中性外观。独立任务区域和打开的确认框分别确定主动作。[Fluent Button 选型](https://fluent2.microsoft.design/components/web/react/core/button/usage)

| 组件与变体 | 适用场景 | ClawManager 中的用法 |
| --- | --- | --- |
| `Button appearance="primary"` | 完成当前区域的主要任务 | 实例管理的「创建实例」、表单的「保存」、休眠工作台的「唤醒」、Runtime 概览的「实例调度」。用文字或图标加文字。 |
| `Button appearance="secondary"` | 主动作旁的次要任务 | 实例管理的「批量创建」与「创建实例」同在工具栏，保持独立入口。弹窗的「取消」、预览的「下载」也用此变体。 |
| `Button appearance="outline"` | 保留所在面板的底色，同时需要可见边框 | 独立浅色分区中的中性操作组。整组有一致边框和尺寸。 |
| `Button appearance="subtle"` | 局部、可重复的轻操作 | 工具栏展开搜索、清除输入、行内更多、侧栏折叠。局部重试可用带文字的 Subtle 按钮。 |
| `Button appearance="transparent"` | 与标题或控制栏融合的轻操作 | 模态关闭、编辑器内局部移除、已提供上下文的图标操作。悬停主要改变前景色。 |
| 共享危险确认按钮，基于 `Button appearance="primary"` | 用户在确认框中批准不可逆或高风险变更 | 行内「删除实例」放在更多菜单末尾。Dialog 内的「删除实例」才用实底危险色，初始焦点在「取消」。风险确认条件沿用对应业务流程。 |
| `ToggleButton appearance="subtle"` | 工具或视图状态持续开启或关闭 | 置顶 Agent、工作台工具栏中持续打开的工具。由 `checked` 绑定真实状态，图标与状态一致，可访问名称保持稳定。 |
| `MenuButton` 或 Subtle 图标 Button + `MenuTrigger` | 显示一组可执行命令 | 带文字的「运行管理」、账户菜单、行内「更多」。触发器只展开已授权的选项。 |
| `SplitButton` + `Menu` | 一个明确的主命令带多个相关执行方式 | 默认执行一次导出，箭头提供其他导出方式。主命令和菜单各有键盘目标。 |
| `CompoundButton appearance="secondary"` | 选择命令前需要阅读一行补充说明 | 在显式动作选择区域展示名称及简短影响，说明放 `secondaryContent`。 |
| `Link` 或路由链接 | 导向有实际地址的详情、页面或文档 | 实例名称到详情、表格事件到审计详情。保留打开新标签等原生链接行为。 |
| `Toolbar` + 分组的命令控件 | 在同一区域集中提供频繁使用的操作 | 实例管理的搜索、筛选和创建位于紧凑工具栏。搜索与筛选组成查询区，批量创建是独立命令。窄屏优先收起次要筛选。 |

#### 按钮颜色和动效

下表的状态依次是默认、悬停、按下。采用原生变体时由 Fluent UI React v9 输出这些状态。危险确认由共享组件按公开样式接口映射同一组状态。[Fluent React 按钮样式](https://github.com/microsoft/fluentui/blob/master/packages/react-components/react-button/library/src/components/Button/useButtonStyles.styles.ts) · [Web 浅色 Token](https://fluent2.microsoft.design/color-tokens/)

| 变体 | 背景 | 文字与图标 | 描边 |
| --- | --- | --- | --- |
| Primary | `colorBrandBackground` #0f6cbd → `colorBrandBackgroundHover` #115ea3 → `colorBrandBackgroundPressed` #0c3b5e | `colorNeutralForegroundOnBrand` #ffffff | 透明，保留组件焦点描边 |
| Secondary | `colorNeutralBackground1` #ffffff → `colorNeutralBackground1Hover` #f5f5f5 → `colorNeutralBackground1Pressed` #e0e0e0 | `colorNeutralForeground1` 及对应 Hover、Pressed alias | `colorNeutralStroke1` #d1d1d1，悬停与按下用对应 Hover、Pressed alias |
| Outline | `colorTransparentBackground` 及对应 Hover、Pressed alias，默认浅色主题均透明 | 同 Secondary | 同 Secondary |
| Subtle | `colorSubtleBackground` 透明 → `colorSubtleBackgroundHover` #f5f5f5 → `colorSubtleBackgroundPressed` #e0e0e0 | `colorNeutralForeground2` 及对应 Hover、Pressed alias，图标交互色沿用 Fluent | 透明 |
| Transparent | `colorTransparentBackground` 及对应 Hover、Pressed alias | `colorNeutralForeground2` → `colorNeutralForeground2BrandHover` → `colorNeutralForeground2BrandPressed` | 透明 |
| 危险确认 | `colorStatusDangerBackground3` #c50f1f → `colorStatusDangerBackground3Hover` #b10e1c → `colorStatusDangerBackground3Pressed` #960b18 | `colorNeutralForegroundOnBrand` #ffffff | 透明，沿用主按钮焦点对比 |
| Link | 继承所在表面 | `colorBrandForegroundLink` → `colorBrandForegroundLinkHover` → `colorBrandForegroundLinkPressed` | 通过下划线或可辨识的导航布局表达链接 |

- **持续选中**使用 ToggleButton 的 `checked`。Subtle 的已选背景是 `colorSubtleBackgroundSelected` #ebebeb，按住瞬间仍使用 Pressed 状态。Primary 的持续选中使用 `colorBrandBackgroundSelected` #0f548c，仅在整体已属于主操作时使用。
- **尺寸**默认 `size="medium"`，32px 高、14/20px、600 字重、圆角 4px。有文字的中号按钮保留 Fluent 的 96px 最小宽度，纯图标中号为 32×32px。24px 小号用于密集桌面工具栏或单行表格内，40px 大号用于需要更大操作面积的独立动作。移动端触控目标至少 44×44px，命中区独立于图标大小。
- **焦点**使用官方焦点样式。CM 自有按钮使用 `colorStrokeFocus1` 与 `colorStrokeFocus2` 的高对比组合，强制色使用系统前景。纯图标按钮需要可访问名称与 Tooltip。
- **禁用与提交中**保留动作语义。填充型按钮使用 `colorNeutralBackgroundDisabled`、`colorNeutralForegroundDisabled`、`colorNeutralStrokeDisabled`，边框是否显示随原变体。请求处理中保留原按钮占位，显示 Spinner 加「保存中」等文本，阻止重复执行，并在结果返回后恢复有效操作和焦点。
- **动效**标准 Button 的背景、文字和描边过渡使用 `durationFaster` 100ms / `curveEasyEase`。自有按钮沿用同样的属性和时长，尺寸和位置固定。焦点出现及按下语义即时生效，减少动态效果时直接呈现目标状态。

### 搜索、表单与选择

| 组件与变体 | 适用场景 | ClawManager 中的用法 |
| --- | --- | --- |
| `Field` | 输入控件需要长期可见的标签、约束或校验反馈 | 名称、资源配额、导入配置用同一容器关联标签、单位、帮助和错误。必填、范围、格式等任务所需信息始终可见。 |
| `Label` 或原生 `label` | 单个控件仅需名称，或给一个输入组命名 | 开关的可见标签，或用 `fieldset` / `legend` 为一组互斥选项提供标题。使用 Field 时复用其标签关系。 |
| `InfoLabel` | 标签旁需要可选的口径说明、术语或更多文档 | 费用的统计范围、配额与实测用量区别。完成任务所需的单位、限制和错误放在 Field 的可见帮助中。[Fluent InfoLabel](https://fluent2.microsoft.design/components/web/react/core/infolabel/usage/) |
| `Input appearance="outline"` | 输入单行文本或精确数值 | 名称、路径、端口、起始序号。数值用适当类型、步长和单位。占位符用于示例，标签始终显示。 |
| `Textarea`，默认 Outline | 输入可以换行的文本 | 描述、备注和多行环境变量。保持原始换行和键盘编辑。代码语法与行级诊断使用工作台编辑器。 |
| `SearchBox appearance="outline" size="medium"` | 查找或缩小当前结果集，输入本身是搜索条件 | 管理工具栏初始用 Subtle 图标触发。展开后焦点进入，Esc 收起并还原焦点。保留其他筛选和原有查询规则。 |
| `Dropdown appearance="outline" size="medium"` | 从固定集合选单值或多值，允许自定义选项呈现 | 状态、类型、Lite/Pro 筛选。显示当前值或已选数，筛选支持清除。选项名称与结果列表的状态词一致。 |
| 原生 `Select` | 交互要求使用平台原生选择器、列表只含简单文本 | 使用系统弹出列表时核对目标平台行为。需要一致的 Fluent 浮层、搜索或结构化选项时按对应 Dropdown 或 Combobox 规则。 |
| `Combobox appearance="outline"` | 先输入关键字，再从匹配项确定值 | 负责人或团队数量较多时，用搜索缩小候选范围。区分加载、无匹配与失败，仅在业务允许时接受自由值。 |
| `TagPicker`，默认 Outline | 搜索并加入多个对象，已选对象需一直可见且能分别移除 | 多个成员或技能的选择。候选项与选择结果使用同一份真实数据，使用 Tag 表达已选项，移除后合理移动焦点。[Fluent TagPicker](https://fluent2.microsoft.design/components/web/react/core/tagpicker/usage) |
| `RadioGroup` | 一组少量、相互排斥的选项需要同时比较 | 创建实例时同时呈现 Lite 和 Pro 及必要说明。选项多且无需同时比较时用 Dropdown。 |
| `Checkbox` | 独立勾选、多选、待提交的布尔值或批量行选择 | 技能勾选、表单中随提交生效的布尔值。表头只选中部分记录时显示 indeterminate，并说明选择范围。 |
| `Switch` | 即时反映开关值，或立即改变当前表单模式 | 「保持运行」即时提交，请求中防重，失败回到已确认值并就近说明。「批量创建」立即切换表单字段，创建仍由提交按钮执行。 |
| `SpinButton`，默认 Outline | 有明确上下界及固定步长，用户需精确递增或递减 | 批量创建数量等整数。保留可键入数值、最小值、最大值和错误反馈。需要直接输入大范围精确值时用 Input。 |
| `Slider` + 同步数值输入 | 范围位置有意义，用户需拖动快速粗调，且能输入精确值 | Pro 资源配置可以同时使用滑块和数字输入，两者共用后端规定的范围、步长和单位。金额、端口等精确值只用数值输入。 |
| `Rating` / `RatingDisplay` | 显式收集用户评分，或展示已收集的真实聚合评分 | 填写评分用可交互 Rating。详情展示分数与明确统计数，密集列表可用紧凑 RatingDisplay。健康、权限和运行级别沿用状态组件。[Fluent Rating](https://fluent2.microsoft.design/components/web/react/core/rating/usage) |
| `SwatchPicker` | 选择一个颜色是任务本身，并且可选颜色已经定义 | 适用于将来的外观色板等明确选色场景。色块有可读名称和选中标记，必要时放入 Popover。 |
| 原生日期、时间输入 + `Field` | 输入绝对日期、计划时间或选择查询区间 | 审计导出范围、API Key 到期、计划时间。显示时区，起止分别有标签，并在相邻位置报区间错误。 |
| 文件输入 + Secondary 按钮 | 从本机导入受支持的文件或目录 | 实例配置、工作区和技能导入。选择后显示文件名、替换和移除。文件类型、授权、大小和失败原因按真实规则反馈。 |

默认字段高度 32px，正文 14/20px，圆角 4px。Outline Input 的表面使用 `colorNeutralBackground1`，常态外边缘用 `colorNeutralStroke1`，底边用 `colorNeutralStrokeAccessible`。悬停外边缘和底边使用各自的 Hover alias。2px 焦点底边用 `colorCompoundBrandStroke`，失焦的错误边框沿官方 `colorPaletteRedBorder2`，校验文字用 `colorStatusDangerForeground1`。只读字段保留可读前景及文字选择能力。禁用态跟随组件的 `colorNeutralForegroundDisabled` 与 `colorNeutralStrokeDisabled`。复合搜索只显示一套焦点指示。选项列表沿用组件内建的悬停、焦点和选中标记，键盘路径覆盖方向键、Enter 和 Escape。[Input](https://fluent2.microsoft.design/components/web/react/core/input/usage) · [Dropdown](https://fluent2.microsoft.design/components/web/react/core/dropdown/usage)

操作名称使用明确动词并在需要时加对象，如“创建实例”“删除实例”。保留任务所需的标签、限制和操作后果。错误说明发生了什么，以及用户能做的下一步。可见标签、提示和无障碍名称统一进入现有国际化体系。

### 组件状态与交互

按组件实际支持的状态逐项验收。截图或设计稿明确状态，代码使用组件属性、浏览器语义或公共状态样式表达相同含义。

| 状态 | 验收标准 |
| --- | --- |
| 默认、悬停、按下 | 默认态表达操作优先级。悬停和按下的反馈各有对应样式。触屏可直接完成操作，图标或行操作在键盘焦点进入时可达。 |
| 键盘焦点 | 焦点清晰且可见，滚动容器、粘性区域和浮层保留可见的焦点目标。浮层关闭后返回触发入口，入口消失时移到最近仍可操作的位置。 |
| 当前、选中、展开 | 当前页面、选中项和展开状态分别使用适当的 `aria-current`、`aria-selected`、`aria-pressed`、`aria-expanded` 或原生语义。结合文字、勾选、字重或指示条辨识状态。 |
| 禁用、处理中 | 禁用按组件语义阻止操作，需要说明时提供键盘可读的原因。提交中防止重复执行，显示进度或处理中标签。 |
| 加载、空数据 | 初次加载维持内容的空间结构。刷新保留已有内容及选择。空列表说明当前确实无数据或无匹配，并在合适位置提供下一步。 |
| 错误、权限不足 | 字段错误就地关联输入，页面或请求错误就地保留并提供可用的恢复方式。按真实权限呈现操作。 |

对话框打开时焦点进入弹窗，`Tab` 循环在模态内，关闭后焦点返回。删除等危险操作写明对象与后果，初始焦点落在安全选项。选择列表和命令菜单各自保留正确的键盘模型。

### 表格、列表与数据网格

| 组件、密度与布局 | 适用场景 | ClawManager 中的用法 |
| --- | --- | --- |
| `Table` 标准型，44px | 浏览并操作多列资源 | 实例和 Runtime Pod 管理。名称为链接，勾选、状态和行内菜单独立。排序、筛选、分页沿用现有数据来源及选择范围。 |
| `Table` 密集型，桌面 36px | 长时间高密度扫读日志、审计或紧凑预览 | 时间、等级和摘要单行，主要记录通过链接或行操作打开详情。触屏回到标准 44px。 |
| `Table` 数值型，沿用标准型或已选密度 | 比较跨行的数值、用量或成对字段 | 费用汇总用标准型，创建预览的 Key / Value 可用桌面密集型。表头显示币种或单位，数字右对齐，空值用“—”，显示真实的 0。 |
| `DataGrid`，默认 `focusMode="cell"` | 需要方向键在多个单元格之间连续查看或操作 | 有真实行选择需求时启用 `selectionMode`。单元格内有多个独立操作且需要行级复合导航时选 `focusMode="composite"`，验收进入、操作、离开的键盘路径。 |
| `List` 或语义 `ul` / `ol` | 逐项浏览同类对象，一列即可判断下一步 | 最近使用、实例侧栏、成员单列列表。每项有稳定名称和入口。比较多字段用 Table，父子路径用 Tree。 |
| 说明列表 `dl` | 阅读单个对象的多组属性 | 实例概况、连接详情、只读配置摘要。有多行需要比较、排序或批量操作时用 Table。 |
| CM 分页控件组合 | 结果按服务端或客户端页组织 | 上一页／下一页使用有名称的按钮，显示当前页和真实总数。页大小可更改时用 Dropdown。筛选更改后的页码按原有查询规则更新。 |

#### 表格视觉与状态

标准表头和行高均为 44px，单元格水平内边距 12px。密集表头和行高均为 36px，水平内边距 8px。两种样式使用 14/20px 正文，表头 600 字重。尺寸为默认文字大小下的视觉基准，放大后按焦点和文字可见性调整控件区域。勾选和单个图标操作列各预留至少 44px。表格在自己的容器横向滚动，窄屏把次要操作收进菜单，保留资源名称、状态、主要操作和键盘入口。

| 部位或状态 | 背景、前景及分隔 | 放置及行为 |
| --- | --- | --- |
| 表头 | `colorNeutralBackground2` #fafafa、`colorNeutralForeground3` #616161 | 表头与对应列同向对齐。可排序列使用 `TableHeaderCell` 的排序按钮，当前排序列用图标和 `aria-sort` 表达方向。 |
| 正常行 | `colorNeutralBackground1` #ffffff、`colorNeutralForeground1` #242424，`colorNeutralStroke2` #e0e0e0 作 1px 水平分隔 | 同表维持一致底色和行高。名称用主要字阶，ID 和单位用次级字阶，状态词与短原因同行。 |
| 悬停与按下 | `colorSubtleBackgroundHover` #f5f5f5、`colorSubtleBackgroundPressed` #e0e0e0 | 行内可用操作随鼠标或焦点可达，移动端提供常显入口。独立链接、选择控件和菜单保留各自命中区。 |
| 选中行 | 标准选中 `TableRow appearance="brand"` 或 DataGrid 的 `selectionAppearance="brand"`，静止背景 `colorBrandBackground2` #ebf3fc，悬停和按下沿用 Fluent 行变体 | 复选框或单选标记在悬停及焦点时持续可见，与选中数同步。原生表格用选中控件表达，DataGrid 按自身语义表达。 |
| 键盘焦点 | 内建的 `colorStrokeFocus2` 焦点轮廓，强制色用系统色 | 普通表格使用 Tab 到表头命令、链接、选择和更多。DataGrid 使用网格键盘规则。截断容器只包住文字，焦点和浮层保留可见空间。 |
| 错误或警告行 | 行底沿用当前行状态，错误与警告色限于对应的 Badge、文本或图标 | 如 Runtime 失败，显示「失败」与可达的完整原因。状态更新保持行高和按钮位置。 |

名称与 ID、状态与短原因共用一行。优先保留 ID 和必要状态，长名称省略。通过现有链接或可点击、可键盘访问的「详情」入口查看全文。数值与表头对齐，支持等宽数字。批量操作前显示已选数量，表头勾选提示选择范围，确认框沿用实际授权和失败恢复规则。表格刷新沿用原位更新，排序、选中、刷新和分页即时呈现，避免逐行位移和入场。CM 自有行如果加悬停过渡，只允许背景色 `durationFaster` 100ms / `curveEasyEase`。[Fluent Table 行样式](https://github.com/microsoft/fluentui/blob/master/packages/react-components/react-table/library/src/components/TableRow/useTableRowStyles.styles.ts)

### 导航、卡片与面板

#### 导航与视图

| 组件与外观 | 适用场景 | ClawManager 中的用法 |
| --- | --- | --- |
| `NavDrawer` + `NavItem` | 在产品主区域或稳定的一级子区域间导航 | 个人区和 Console 外壳按 224px／64px，导航行 40px。NavItem 背景 `colorNeutralBackground4` #f0f0f0、悬停 `colorNeutralBackground4Hover` #fafafa。当前项用 600 字重、品牌填充图标和 `colorCompoundBrandForeground1` 的 4×20px 指示条。折叠后保留可访问名称与 Tooltip。 |
| `Breadcrumb` + 路由链接 | 表达既有页面层级与当前位置 | 顶栏显示父路径和当前项。非当前链接用 `colorBrandForegroundLink`，当前文字用 `colorNeutralForeground1` 并标记 `aria-current="page"`。 |
| `TabList` + `Tab` | 在同一任务上下文切换互斥的内容面板 | Skill Hub 内容分类、同一个对象的不同视图。默认中性外观及品牌指示，保持面板与选中项关联。改变产品区域时使用 Nav 或 Link。 |
| `Tree` + `TreeItem` | 浏览或操作真实父子结构 | 工作区目录、具有多层层级的资源。分支负责展开，叶项负责打开或操作。未展开、选中、焦点各自可辨识，支持方向键。平面列表用 List，同级内容分组折叠用 Accordion。[Fluent Tree](https://fluent2.microsoft.design/components/web/react/core/tree/usage) |

#### 内容与容器

| 组件与变体 | 适用场景 | ClawManager 中的用法 |
| --- | --- | --- |
| `Text` 配合语义标题与正文 | 页面需要明确内容层级、辅助说明或数值 | 采用 [Typography](#typography) 的字阶映射。卡片标题、状态说明、指标数值各用相应角色，视觉字号和文档标题层级分别按语义确定。 |
| CM 语义 `section` | 同一区域内组织静态读数、表格或表单 | Dashboard 指标、管理表格容器和表单区域用 `colorNeutralBackground1`、1px `colorNeutralStroke2`、8px 圆角、16px 内边距，同组对齐。 |
| `Card appearance="filled"` | 展示可打开或具有独立操作的对象 | 置顶 Agent。白底 `colorNeutralBackground1` + `shadow4`，交互悬停用 `colorNeutralBackground1Hover` + `shadow8`，按下用 `colorNeutralBackground1Pressed`。主要入口、置顶和附属菜单分别可达。 |
| `Card appearance="filled-alternative"` | 白色容器中需要较弱的对象表面，仍保留卡片层级 | 可独立查看的次级对象用 `colorNeutralBackground2` + `shadow4`，同组对象保持一致外观。 |
| `Card appearance="outline"` | 在当前背景中用边界区分同类对象，层级保持平面 | 同组模板或选项的独立预览可用透明背景及 `colorNeutralStroke1`。若任务只是从互斥值选一个，选择状态仍使用单选语义。 |
| `Card appearance="subtle"` | 容器内的轻量对象摘要需要稳定内边距，底色与所在区域融合 | 次要对象预览。沿用 `colorSubtleBackground`，交互反馈使用组件内建状态。 |
| `Accordion` | 同页有多组可独立阅读的次要内容，用户可按需展开 | 高级配置、非必要的补充说明。标题说明内容，箭头位置一致。必需字段和待处理错误在主路径可见，校验涉及折叠内容时展开并定位。 |
| `Divider` | 标明内容或操作组的边界 | 全宽分隔不同区域，内缩分隔同一列表的相关项，竖向分隔工具栏中的命令组。同样层级使用相同强度。 |
| `Image` 或语义 `figure` | 展示一张截图、图片产物或有说明的媒体 | 提供有意义的替代文本。需要图注时使用 figure，查看原图使用可达链接。多张相互独立且需并排比较的截图保持同时可见。 |
| `Carousel` | 用户按顺序浏览多张图片或预览，每一项可独立查看 | 如同一份产物的多张截图。默认手动前后切换，显示当前项和总数。Dashboard 并列指标、需要横向比较的数据和关键操作保持同时可见。 |

[Fluent Nav 样式](https://github.com/microsoft/fluentui/blob/master/packages/react-components/react-nav/library/src/components/sharedNavStyles.styles.ts) · [Fluent Card](https://fluent2.microsoft.design/components/web/react/core/card/usage) · [Fluent Accordion](https://fluent2.microsoft.design/components/web/react/core/accordion/usage) · [Fluent Divider](https://fluent2.microsoft.design/components/web/react/core/divider/usage) · [Fluent Carousel](https://fluent2.microsoft.design/components/web/react/core/carousel/usage)

### 图标、头像和品牌

新建和迁移页面的导航、工具栏和操作使用 Fluent System Icons。常规态用 Regular，选中或强调态按语义用 Filled；默认 20px，紧凑行可用 16px，重要操作可用 24px。现有 Lucide 随页面迁移替换。[官方图标规范](https://fluent2.microsoft.design/iconography)

**Agent 类型使用真实品牌图标**，不替换为通用显示器或系统图标：OpenClaw、Hermes、OpenCode、DeepSeek Harness 等复用现有资产与类型映射；继续工作与置顶区域均用 32px 容器，保留原始宽高比及颜色，不滤镜染蓝、不裁切 Logo。未知类型有清晰回退和可访问名称。品牌图与系统图标承担不同角色，二者可以共存。

| 组件与变体 | 适用场景 | ClawManager 中的用法 |
| --- | --- | --- |
| `Avatar` | 一个人或一个团队的身份 | 个人头像通常圆形，团队头像用组形状。账户入口和成员行使用本人图片或姓名缩写，并保留名称。Agent 类型沿用品牌图标。 |
| `AvatarGroup` | 同一区域代表多位成员，快速辨认比展示完整属性重要 | 团队参与者默认用 spread。空间不足时用 stack，极紧凑且不需辨认每个成员时可用 pie。溢出显示真实数量，完整成员和角色仍可查看。 |
| `Persona` | 头像旁需要同时展示姓名和一项身份信息 | 带角色的成员或带身份提示的负责人候选项。支持一行或多行的场景可用完整 Persona。单行管理表格将名字与次级信息同行。 |
| `PresenceBadge` | 人员确有在线、忙碌、离开等实时信号 | 附着于 Avatar 或 Persona。保留状态文字或可访问名称，未知有独立显示。Agent 生命周期使用 Status Indicator 或 Badge。 |
| `Badge appearance="tint"` | 表示系统提供的稳定状态、级别或分类 | 成功／运行、警告、失败分别用 `color="success"`、`"warning"`、`"danger"`。仅表达分类时用中性色。标签提供状态词，完整原因另有可达入口。 |
| `Badge appearance="outline"` 或 `"ghost"` | 次要的系统标签需要弱化视觉权重 | 需要轮廓区分时用 Outline，旁边已有足够界限且只需短分类文字时用 Ghost。同列同场景使用一致外观。 |
| `CounterBadge` | 需要在对象或范围入口旁显示真实计数 | 例如待处理或未读数量。可访问名称同时说明数量和范围，完整清单通过原入口打开。 |
| Status Indicator（图标加文本） | 状态变化频繁，用户需要快速扫读对象进展 | 实例／Pod 列表中的「运行」「启动」「离线」「未知」及原因。使用状态图标和语义色配文字，颜色只应用在状态内容。 |
| `Tag` | 表达用户已经选中的值，唯一附属操作是移除该值 | 当前筛选、已选技能或成员。可移除时提供带名称的移除入口。状态由系统产生时用 Badge。 |
| `InteractionTag` | 已选对象还有一个与自身直接相关的主要动作 | 点击已选成员打开身份摘要，移除仍是独立次要动作。搜索和加入多个对象使用 TagPicker。 |

[Fluent Avatar](https://fluent2.microsoft.design/components/web/react/core/avatar/usage/) · [Fluent AvatarGroup](https://fluent2.microsoft.design/components/web/react/core/avatargroup/usage) · [Fluent Persona](https://fluent2.microsoft.design/components/web/react/core/persona/usage) · [Fluent Tag](https://fluent2.microsoft.design/components/web/react/core/tag/usage)

### 图表、状态与数据真实性

| 组件 | 适用场景 | ClawManager 中的用法 |
| --- | --- | --- |
| 指标值 + 时间范围 | 需要立即读出一个真实总量或当前值 | Token 总量、费用小计、实例数。用 Metric 字阶，单位、期间和覆盖范围就近呈现。 |
| 折线图／浅面积图 | 时间趋势和缺测连续性有意义 | Token 与费用并列时间轴趋势。用独立轴、单位和实线／虚线区分系列。 |
| 条形图 | 比较离散对象的量级或排序 | 不同服务、团队或资源项的分布。对齐尺度，类别名称和关键值始终可读。 |
| 环图 + 图例 | 展示可解释的总体及其组成 | Agent 状态分布。环中心显示运行／总数，图例显示真实分项和数量。 |
| 表盘 + 文本 | 当前读数需要对照可靠的容量上限 | Dashboard 同时展示 CPU、内存和 Workspace 存储的已用、可用与比例，标明是配额还是实际使用。 |
| 年度热力图 | 对齐日期并查看活动强度 | 个人活动日历。按时区组织日期，提供可达日期详情和明确定义的强度图例。 |
| 数据表 | 任务是精确查询、比较或导出数据 | 对应统计范围提供 Tab 可达的数据表或明细入口，保留与主图一致的单位和过滤条件。 |

图表优先核对 Fluent React Charts 对轴、单位、焦点和交互的支持。已有专业图形可在保留真实数据、图例、可访问入口和 Fluent 外层表面的前提下继续使用。Token 和费用曲线有时间范围、轴/单位、悬停与键盘可达的数据详情；当前值与选定时间范围一致。费用区分币种，区分实际账单与估算。曲线缺测保留断点或“暂无数据”，不补零制造趋势。用量卡片独立切换时间范围，只请求用量接口；保留其他区域的 DOM、交互状态和数据，局部加载时保留原图并提供短暂过渡，失败在卡片内重试。双轴采用蓝色 Token 实线与橙色费用虚线，平滑曲线经过真实样本且不超出相邻值范围。主卡片移除采集状态、统计口径和底部解释段落，保留日期、单位、币种、明确的状态标签以及错误；不完整费用以“小计”字段名表达，估算值用 ≈ 标明。必要的来源和口径放在详情中。

资源表盘同时显示 CPU、内存、Workspace 存储的已用 / 可用、单位和百分比；标题明确是配额占用、分配容量还是实际使用率。不能将分配量当实测消耗，不能把实例配额汇总称为集群物理容量。获取失败或暂不支持明确显示未知，不显示 0%。

Agent 环图中心显示“运行数量 / 总数量”，下方标注“运行 / 总数”；运行数量合计忙碌、空闲与已确认运行三类，总数量使用完整状态聚合，不受列表展示条数限制。中心保持单行；空数据为 `0 / 0`，统计不可用为 `— / —`。配完整图例与各状态数量，可进入相应列表。运行状态与任务状态分开：运行中不等于正在执行任务，休眠不等于失败；任务进行中、新完成与失败依赖真实事件或可靠信号。继续工作按真实使用记录排序，不按创建时间伪造最近使用。

活动热力图按个人身份、日期和时区统计，提供强度图例、日期详情及键盘替代入口。图表同时提供简短文字概览或数据表，不能让 SVG/Canvas 成为唯一的数据入口。异步同步时保持布局、图例顺序和焦点稳定，不自动重排用户正在操作的卡片。

### 浮层、消息与加载

#### 浮层

| 组件与外观 | 适用场景 | ClawManager 中的用法 |
| --- | --- | --- |
| `Menu`，白底、4px 圆角、`shadow16` | 从触发器选择即时命令或导航入口 | 行内更多、账户菜单、运行管理。方向键移动，Enter/Space 执行，Esc 返回触发器。危险命令靠后并与常用命令分组，文字用 `colorStatusDangerForeground1`。 |
| Dropdown／Combobox 的 Listbox，白底、4px 圆角、`shadow16` | 展开可选值，用户选择后控件保留该值 | 从筛选触发器或候选输入框对齐展开，视口边缘可翻转和滚动。焦点与已选标记可以同时辨识。 |
| `Tooltip`，白底、`colorNeutralForeground1`、4px 圆角、等效 `shadow8` | 为现有触发器提供简短纯文本说明 | 图标名称、已省略词的简短解释。悬停、键盘焦点均可显示。必要信息另有可达入口。 |
| `Popover`，白底、4px 圆角、等效 `shadow16` | 在原位查看结构化信息或完成一个简短附属操作 | 如实例运行详情和保持运行开关。内容受视口约束，关闭后回到触发器。较长详情选择 Drawer。 |
| Teaching Popover，默认中性外观 | 用户主动查看与当前控件相邻的操作帮助 | 标明适用控件、给出简短可执行步骤，并允许关闭。提交条件和关键后果放在主流程可见位置。 |
| `InlineDrawer`，medium、逻辑末端 | 阅读或编辑附属内容，同时要对照主区且继续操作 | 宽屏主列表旁核对一条资源详情。主区保持可操作，焦点按布局顺序可达。 |
| `OverlayDrawer`，medium、逻辑末端、`shadow64` | 附属详情较长，或窄屏空间只够显示当前任务 | 覆盖主区并根据任务使用组件的模态行为。视口内正文可滚动，关闭按钮及主操作可达。 |
| `Dialog` + `DialogSurface`，白底、8px 圆角、`shadow64`、40% `colorBackgroundOverlay` | 阻断式确认或必须完成的短流程 | 删除实例、放弃未保存更改。短确认统一 440px，窄屏保留两侧边距，长内容最大 600px。正文滚动，动作区末端对齐、间隔 8px、取消在确认前。 |
| Material acrylic（材质变体） | PDF 中用于轻触关闭的短时浮层 | CM 默认 Fluent 2 Web 的菜单和 Popover 使用不透明中性表面。后续经确认的外观若采用该材质，适用范围限于这些临时浮层，仍须验证叠在不同内容上的对比度。 |

#### 状态反馈与加载

| 组件与变体 | 适用场景 | ClawManager 中的用法 |
| --- | --- | --- |
| `Field` 的校验反馈 | 错误关联到一个具体输入，且可在原位修复 | 无效范围、日期顺序、文件格式。说明问题和修复方式，并通过控件关系让辅助技术读到。 |
| `MessageBar`，按实际情况选 `intent` | 状态或错误影响整个表单、区域、Drawer 或页面，需要在原处持续可见 | 筛选失败在表格区域给重试，提交失败在表单给恢复动作。Info 用 `colorNeutralBackground3`、`colorNeutralStroke1`，成功、警告、错误用各自 `colorStatus…Background1`、`…Border1` 和语义图标。 |
| `Toast`，指定语义 `intent` | 低风险操作结束，用户可继续当前任务 | 已复制、保存完成。带动作的 Toast 只用于确有可执行后续操作的情况。持久错误或部分失败的结果留在相应区域或 Dialog。 |
| `Skeleton` | 首次加载一块已知结构的内容，预计等待超过一秒 | 实例表格初载保留表头、列宽和行高。字块、图形遵循内容形状，用 `colorNeutralStencil1` / `colorNeutralStencil2`。刷新时保留已有数据。 |
| `Spinner` + 进度文字 | 一个局部动作正在处理，暂无可量化进度 | 表单正在保存、实例正在唤醒。显示在原操作区，防止重复提交。 |
| `ProgressBar`，可确定进度 | 后端提供可靠的完成量和总量 | 上传、可统计的批处理、资源容量。明确单位和实际进度，不用预设时钟模拟百分比。 |
| `ProgressBar`，不确定进度 | 较长任务需要持续进度提示，只有真实阶段或进行中信号 | 导入或恢复任务可显示当前阶段，进度条维持不确定状态。已知失败在任务区域留恢复入口。 |
| CM 空态组合 | 真实请求完成后结果为空、筛选无匹配或用户暂无访问权限 | 无对象时提供有权限的首个创建入口，无匹配时提供清除筛选。权限不足、服务错误和加载各保留对应状态说明。 |

[Fluent Dialog](https://fluent2.microsoft.design/components/web/react/core/dialog/usage) · [Fluent Drawer](https://fluent2.microsoft.design/components/web/react/core/drawer/usage) · [Fluent MessageBar](https://fluent2.microsoft.design/components/web/react/core/messagebar/usage) · [Fluent Menu](https://fluent2.microsoft.design/components/web/react/core/menu/usage)

### 关键页面的组合规则

| 页面或流程 | 组件组合 |
| --- | --- |
| 实例管理 | 同行工具栏使用 Primary「创建实例」、Secondary「批量创建」、Subtle 搜索和筛选 Dropdown。用标准 Table，名称与 ID 同行，主要行命令和更多各自可达。删除从更多进入 Dialog；批量部分失败时 Dialog 或管理窗口列出失败对象和原因并保留失败项选择。 |
| Runtime 概览和 Pod 管理 | 概览右侧 Primary「实例调度」+ Subtle「更多」。Pod 管理用标准 Table，运行状态单元格放点或图标、状态词及短原因。Pod 维护进入更多，按既有预检结果启用相应命令。 |
| 实例创建 | 单页表单使用 Field 与 Input / RadioGroup / Switch，提交 Primary「创建实例」，退出或取消用 Secondary。右侧预览是静态面板，环境变量用 Key / Value 表格。提交不替换整页，错误保留输入并回到可修复字段。 |
| 管理审计与费用 | 审计用桌面密集 Table 和可达详情入口，严重状态由 Badge 与文字表达。费用用标准 Table，单位与币种随列头，数字右对齐，缺测显示“—”。 |
| 实例工作台恢复 | 休眠内容区用 Primary「唤醒」和原生类型状态图标，操作中在原位置显示进度。失败用区域内 MessageBar 与可用的重试。工作台正常态的技能入口和「运行管理」各自可达。 |

### 工作台与嵌入内容

团队对话、实例工作台、编辑器、终端及 Console 沿用各自信息结构，用共享外壳、工具栏和焦点规则连接；不能为统一卡片外观破坏长会话、代码阅读、流式输出或终端尺寸。个人数据与平台管理数据遵循现有权限范围，不能混合呈现为用户拥有的资源。

| 组件或组合 | 适用场景 | ClawManager 中的用法 |
| --- | --- | --- |
| CM 可调分栏 + 可聚焦分隔条 | 两个或多个工作区要并行可见，宽度需要用户调整 | 实例工作台与团队工作区。分隔条支持拖动和键盘，保留最小尺寸、焦点和已有会话。静态视觉分组使用 Divider。 |
| CM 消息流 + 输入区 | 按时间阅读及继续团队或 Agent 会话 | 消息流逐条表达作者、内容和发送状态，长文本可换行。输入区使用多行编辑和发送按钮，保留发送、换行与输入法组合行为。 |
| CM 文件列表／`Tree` | 通过名称、属性或目录层级查找文件 | 需要比较大小、更新时间等字段时用单行 Table。需要浏览真实的多级目录时用 Tree。文件操作从行内菜单进入，受当前工作区权限约束。 |
| 工作区代码编辑器 | 需要多行代码、语法、行定位或存储中的文件编辑 | 复用工作区文本编辑器。工具栏提供保存、打开或只读标识，保留编辑缓冲区和错误定位。普通表单描述使用 Textarea。 |
| Markdown／媒体预览 | 阅读文件内容且无需编辑 | Git 文档、文本产物或图片按类型使用已有预览。链接与代码块保持可读，失败或不支持时在原位置说明。 |
| 终端 | 用户需要交互式命令会话或终端输出 | 复用现有终端与会话连接，工具栏只提供有意义的终端动作。窗口尺寸和主题变更保持连接、缓冲区与键盘交互。 |
| 原生 Agent 嵌入容器 | 打开实例原生界面，需要保留其文档或会话边界 | CM 外壳负责名称、状态、业务命令及加载提示。嵌入页通过既有边界、权限和公开入口运行。 |

实例工作台顶部保留可重命名的实例名称和生命周期状态，去掉返回按钮及类型、模式、Runtime、Workspace 大小、更新时间的元信息行。名称内联编辑，Enter/保存提交，Escape/取消返回；失败保留输入，保存仅更新名称，不重启实例。Share Link 旁提供技能管理按钮，在弹窗内管理；启动/停止、重启、配置环境变量并重启、删除收进“运行管理”菜单，删除仍需确认。不在工作台底部显示技能折叠面板或 Session Token Usage。桌面采用“实例列表 / Agent / 工作区”三栏，默认比例 **1.2:6.8:2**；两个分隔条支持拖动与键盘调宽，调整宽度不重载 Agent 会话。实例列表沿用 Fluent 中性表面和选中蓝，使用真实类型图标、名称、状态、服务端搜索与分页，可直接切换当前用户有权访问的实例。实例状态由原生类型图标表达，行背景保持中性：运行中原图轻呼吸；停止灰化并在一次熄灭动效后静止；故障保留静止的原图，仅放大、缩小红色三角感叹号，不晃动图标、不加背后红圈；休眠采用睡姿、Zz 和缓慢呼吸。动效尊重减少动态效果并在离屏、页面隐藏时暂停。当前选中项保留柔和的蓝色渐变高亮与 1px 半透明渐变轮廓；状态保留可访问名称和悬停文字。实例列表标题栏仅保留列表隐藏按钮，不放重复的全屏或展开管理按钮；隐藏后在 Agent 标题栏的实例状态后提供恢复列表按钮，与名称、状态同行，不单独占用一行；按钮不依赖原生页面加载或实例运行状态，Agent 全屏时仍可达，恢复列表同时退出全屏并将焦点返回列表标题栏，保留列表位置。实例列表顶部第一项为“创建实例”，第二项为“实例管理”，随后显示搜索和真实实例；空列表时主窗口显示创建提示及按钮，右上角不放创建入口。切换时使用短时、账户与查询隔离的无凭据列表快照消除空列表，并保留后台同步；详情、原生页面内容及工作区使用短暂淡入，尊重减少动态效果设置。原生文档隔离保持有效。当前实例已删除时清理该账户的导航快照，转到剩余可访问且非删除中的实例；没有剩余实例则在主窗口显示创建提示。元数据 404 的恢复不等待状态请求，服务错误保留重试路径。批量删除部分失败时，在管理窗口列出实例名称与原因并保留失败项选择，不用原生 alert 仅展示汇总数量。隐藏工具区后保留实例列表并将剩余空间分给 Agent；恢复时保持各栏最小宽度。窄屏用弹窗选择实例，Agent 与工具上下排列，保持可用高度。实例或账户切换必须隔离旧请求、弹窗、分享凭据和编辑状态，保留浏览器预览的文档隔离机制：普通运行、休眠、停止和故障实例切换不因访问元数据触发整页重载，仅实际打开 credentialless 预览时绑定当前实例的文档分区；已使用预览的文档切换到其他实例时仍建立新分区。

休眠实例的 Agent 主内容区统一显示该类型的休眠图标、自动休眠状态和“唤醒”按钮，适用于 OpenClaw、Hermes、OpenCode、DeepSeek Harness 的 Lite/Pro 路径。休眠依据 `status=stopped` 与 `stopped_reason=idle_timeout` 判断；唤醒调用现有 CM 启动接口，处理中禁用重复操作，失败在内容区显示真实原因并允许重试，错误只属于发生请求时的 Runtime 代际。手动停止、故障和启动中沿用各自状态。

### 统一创建实例

创建采用单页表单和右侧实时预览，不再使用三步向导。Agent 类型按当前已启用镜像和平台分发能力平铺，默认提供 OpenClaw、Hermes、OpenCode、DeepSeek Harness；镜像自动选择，不展示镜像选择字段。运行模式下方左侧提供“批量创建”开关；批量仅支持 Lite，显示名称前缀、数量、起始序号。负责人和描述选填。Lite 隐藏资源与初始化配置；Pro 展示 CPU、内存、存储，保留配额、GPU 等既有专用能力。

高级配置与实例信息采用同级标题、相同左边界；导入配置是展开后的第一项，单按钮选择文件，选中后显示文件名并可更换、移除；导入能力遵循实际 Runtime 支持范围。环境变量用多行 KEY=value，提供多项示例与错误定位，右侧使用 Key/Value 两列表格展示并遮蔽敏感值。技能从当前账号可用列表勾选，支持检索和取消，已选项同步右侧；正式流程不显示示例技能。部分失败保留每项真实结果和已经创建的实例入口；后续导入失败只重试导入，不重新创建成功实例。

### 动效与可访问性

#### 场景、时长与缓动

官方组件保留其内建动效。共享自有实现按表映射，动效属性限于表中所列。表内长度、时长和缓动使用对应 Token。[Fluent 动效规范](https://fluent2.microsoft.design/motion) · [Duration](https://github.com/microsoft/fluentui/blob/master/packages/tokens/src/global/durations.ts) · [Curves](https://github.com/microsoft/fluentui/blob/master/packages/tokens/src/global/curves.ts)

| 场景 | 动画属性 | Duration / Curve |
| --- | --- | --- |
| Button、ToggleButton 的悬停和按下 | 背景、文字与描边 | `durationFaster` 100ms / `curveEasyEase`。位置、点击区域和文案占位保持稳定。 |
| NavItem 切换、悬停 | 背景、当前项指示条与图标显隐 | `durationFaster` 100ms / `curveLinear`，随真实当前路由更新。 |
| CM 自有字段的焦点底边 | 2px 底边水平显现与收起 | 展开 `durationNormal` 200ms / `curveDecelerateMid`，收起 `durationUltraFast` 50ms / `curveAccelerateMid`。键盘焦点即时生效并保持可辨识。 |
| CM 自有 Menu、Popover 等小浮层 | 开启只淡入，关闭即时 | 开启 `durationFaster` 100ms / `curveDecelerateMid`。位置确定、测量和焦点管理独立于动画。 |
| 官方 Dialog | 内建缩放，遮罩同步 | 开启和关闭均为 `durationGentle` 250ms，进入 `curveDecelerateMid`，退出 `curveAccelerateMin`。 |
| 官方 Drawer | 从所在边缘滑入滑出 | small 250ms、medium 300ms、large 400ms、full 500ms。进入 `curveDecelerateMid`，退出 `curveAccelerateMin`。 |
| CM 自有交互卡片、工作台局部内容替换 | 卡片背景和阴影，内容只淡入 | `durationFast` 150ms。卡片反馈用 `curveEasyEase`，内容淡入用 `curveDecelerateMid`。 |
| 图表的真实数据变化 | 指针旋转及环图段描边 | `durationNormal` 200ms / `curveDecelerateMid`。指标文字直接更新，状态变化来自真实数据。 |
| 表格排序、分页、后台刷新与选择 | 即时更新内容或状态，保持结构 | 0ms 布局动效。自有表格行悬停只过渡背景色，100ms / `curveEasyEase`。 |

[Fluent Dialog 默认动效](https://github.com/microsoft/fluentui/blob/master/packages/react-components/react-dialog/library/src/components/DialogSurfaceMotion.ts) · [Fluent Drawer 默认动效](https://github.com/microsoft/fluentui/blob/master/packages/react-components/react-drawer/library/src/shared/drawerMotions.ts)

实例工作台与休眠提示的原生类型图标使用 `AgentStateIcon` 专用动效。运行呼吸 2.8s 循环，停止灰化 700ms 一次，故障只缩放警示标记 2.2s 循环，休眠姿态和 Zz 为 3.4s 循环。运行、故障、休眠使用 ease-in-out，停止使用 `cubic-bezier(.16,1,.3,1)`。离屏、后台暂停持续动画，回到可见区域继续当前动效。

`prefers-reduced-motion: reduce` 下直接呈现控件、状态和图表最终值，加载保留文本或静态进度指示。焦点转移、关闭浮层、错误展示等交互即时响应。

所有交互提供正确可访问名称和键盘路径。支持文本放大 200% 和页面缩放后的重排，在等效 320px 宽度下内容可用。必要的二维数据使用局部滚动。`forced-colors` 下边界、焦点和状态仍可辨识。加载、空态、权限不足、错误和长内容均按正式状态设计并使用实际状态数据驱动。

### 迁移与验收

先迁移公共 Token 与外壳，再迁移按钮、表单、菜单与浮层，随后覆盖表格和管理页、Dashboard 和工作台、团队及管理员边界场景。首组端到端样例用实例管理页的搜索、筛选、单行表格、批量选择和删除确认。每批核对共享组件的正常入口和受影响的调用方，尤其是团队嵌入页、实例工作台和管理员页面。

#### UI 评审清单

| 核对项 | 合格条件 |
| --- | --- |
| Token 与组件 | 对照组件选型目录核对本次涉及的组件、变体和选型场景。组件有近似选项时，按可观察的任务区别核对，例如命令与表单值、选中对象与系统状态、阻断确认与附属操作。实际渲染核对该组件的适用状态、尺寸、圆角、对比度及浮层主题。 |
| 布局与内容 | 实际浏览器检查 1440、1280、768、390px，补充等效 320px 重排。Dashboard 核对 1440×900 与 1280×800。检查中英文及日、韩、德文长文本，确认表格仅在自身容器滚动。 |
| 状态与可访问性 | 覆盖适用的正常、加载、空、错误、禁用与长内容。用键盘跑通主流程、选择列表、菜单和 Dialog，核对焦点可见、关闭后返回、200% 文本放大及强制色。核对受影响控件的动效属性、时长和缓动，同时检查减少动态效果。 |
| 业务回归 | 验证涉及的路由、API、权限、键盘交互与自动同步。实例管理检查搜索、筛选分页、Lite 批量选择和危险确认。涉及工作台时验证编辑内容及连接状态的保留。 |
| 交付记录 | PR 写明涉及的组件和变体、对应任务与验收状态，附实际浏览器截图或步骤、受影响的公共调用方、类型检查及关键行为回归结果。规范、独立预览、页面实现和部署状态各按实际证据记录。 |

所有适用项通过后才可完成 UI 验收。存在官方组件能力缺口时，按「规则与参考来源」记录选型和公共实现，仍须通过对应检查。历史预览用于核对视觉方向，变更以实际应用的浏览器结果验收。

构建、打包与部署方式由实际开发、CI 和目标部署环境确定，环境专属约束不作为项目通用设计要求。

## Do's and Don'ts

- **Do** 按本文的组件选型与 UI 评审清单交付。使用公共主题、语义层级及完整交互状态。
- **Do** 保留彩色品牌、真实 Agent 类型图标和鲜明而可读的数据图表，让颜色服务于操作和信息。
- **Do** 保留顶部位置导航、紧凑工具栏、单行表格、侧栏边缘折叠按钮及已有自动同步。
- **Do** 让 Token、资源和 Agent 状态分别表达；让继续工作与置顶面板整体对齐。
- **Do** 明确数据范围、单位、币种与缺测状态，保留业务权限和真实状态语义。
- **Don't** 将旧 08、Ant Design Pro、页面自建主题或对组件库内部类名的覆盖带入新建及迁移页面。
- **Don't** 在默认外观使用紫色主题、全屏玻璃、大面积彩色铺底或装饰性渐变。
- **Don't** 将真实 Logo 换成通用图标，或为了密度把正文缩小、裁掉内容、把重要环图缩成装饰。
- **Don't** 在管理概览重复大标题、设计解释、刷新按钮或更新时间；不要把批量创建藏进筛选。
- **Don't** 使用仅靠颜色、仅鼠标可达、失去焦点或没有文字替代的数据图表。
- **Don't** 把已更新的规范、文档预览或待迁移组件写成已经上线的实现。
