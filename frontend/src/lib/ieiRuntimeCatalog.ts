export interface IEIRuntimePresentation {
  id: string;
  name: string;
  category: string;
  tagline: string;
  positioning: string;
  selectionGuide: string;
  capabilities: string[];
  scenarios: string;
  badge?: string;
  notice?: string;
  theme: {
    accent: string;
    accentSoft: string;
    border: string;
    dot: string;
    selection: string;
  };
}

const runtimeCatalog: Record<string, IEIRuntimePresentation> = {
  openclaw: {
    id: "openclaw",
    name: "OpenClaw",
    category: "通用智能体工作台",
    tagline: "连接会话、工具与自动化的通用智能体",
    positioning:
      "OpenClaw 是覆盖面广、扩展性强的通用智能体运行时。它以持续会话为中心，将工具调用、定时执行、消息渠道、文件工作区和 Skill 扩展连接成完整工作链路，既能即时响应，也能持续承接需要长期跟进的自动化任务。",
    selectionGuide:
      "当任务类型多、使用周期长，并且需要不断接入新工具和自动化流程时，优先选择 OpenClaw。它强调通用性与持续协作，而不是只解决某一种专业任务。",
    capabilities: [
      "持续会话与上下文",
      "丰富的工具调用",
      "定时与自动化任务",
      "Channel 多渠道接入",
      "Skill 能力扩展",
      "文件与工作区协作",
    ],
    scenarios:
      "适合市场分析、资料整理、信息处理、业务问答、周期性任务、跨工具自动化和需要持续跟进的综合工作。尤其适用于任务边界会不断变化、后续还要扩展新能力的场景。",
    theme: {
      accent: "text-rose-600",
      accentSoft: "bg-rose-50",
      border: "border-rose-200",
      dot: "bg-rose-500",
      selection: "border-rose-300 bg-rose-50/70 shadow-[inset_4px_0_0_#f43f5e]",
    },
  },
  hermes: {
    id: "hermes",
    name: "Hermes",
    category: "深度任务智能体",
    tagline: "擅长推理、研究与长程任务推进",
    positioning:
      "Hermes 专注需要深入思考和连续推进的复杂任务。它能够围绕目标组织上下文、拆分步骤、调用工具并保留阶段成果，在多轮交互中逐步研究、判断和执行，适合对推理深度、过程完整性与知识沉淀要求较高的工作。",
    selectionGuide:
      "当问题不能一次回答，需要经过检索、分析、论证和多轮推进才能形成可靠结果时，优先选择 Hermes。它比通用自动化更强调深度推理和长程任务质量。",
    capabilities: [
      "多步骤规划与推理",
      "深度研究与知识综合",
      "持续上下文管理",
      "原生工具协同",
      "长程任务执行",
      "过程成果沉淀",
    ],
    scenarios:
      "适合专题研究、复杂问题分析、知识问答、长文材料梳理、方案论证和需要多轮持续推进的任务，也适合将零散资料逐步整理成结构化结论。",
    theme: {
      accent: "text-violet-600",
      accentSoft: "bg-violet-50",
      border: "border-violet-200",
      dot: "bg-violet-500",
      selection: "border-violet-300 bg-violet-50/70 shadow-[inset_4px_0_0_#8b5cf6]",
    },
  },
  opencode: {
    id: "opencode",
    name: "OpenCode",
    category: "智能编码工作台",
    tagline: "理解仓库、执行终端并交付代码修改",
    positioning:
      "OpenCode 是面向真实代码仓库的智能编码运行时。它能够理解项目结构、文件关联和工程上下文，直接完成代码修改、命令执行、错误分析与结果验证，让需求分析、编码、调试和测试形成连续的交付闭环。",
    selectionGuide:
      "当最终成果是可运行、可验证的代码变更时，优先选择 OpenCode。它围绕仓库和工程流程工作，与偏重文档和事务协作的 WorkBuddy 有明确分工。",
    capabilities: [
      "项目与仓库上下文理解",
      "代码生成、修改与重构",
      "终端命令执行",
      "问题定位与调试",
      "测试与结果验证",
      "研发流程自动化",
    ],
    scenarios:
      "适合功能开发、缺陷修复、代码维护、仓库分析、脚本编写、测试验证、依赖升级和工程流程自动化，也适合接手既有项目并围绕实际代码持续迭代。",
    theme: {
      accent: "text-blue-600",
      accentSoft: "bg-blue-50",
      border: "border-blue-200",
      dot: "bg-blue-600",
      selection: "border-blue-300 bg-blue-50/80 shadow-[inset_4px_0_0_#2563eb]",
    },
  },
  "deepseek-harness": {
    id: "deepseek-harness",
    name: "DeepSeek Harness",
    category: "插件化智能体框架",
    tagline: "一切皆插件，按需组合智能体能力",
    positioning:
      "DeepSeek Harness 以“一切皆插件”为核心理念，通过 Cordis 架构将模型、工具、上下文、任务流程和子代理抽象为可组合能力。它不是用途固定的助手，而是可以按任务需要装配、替换和扩展行为的开放式智能体框架。",
    selectionGuide:
      "当重点是试验新能力、组合插件或构建定制智能体流程时，优先选择 DeepSeek Harness。它的优势是开放和可组合，而不是提供固定的一站式工作方式。",
    capabilities: [
      "插件化能力组合",
      "模型与工具灵活装配",
      "任务流程扩展",
      "子代理协同",
      "工作上下文管理",
      "自定义能力实验",
    ],
    scenarios:
      "适合复杂任务拆解、插件能力验证、多代理协作、定制智能体流程，以及快速试验新模型、新工具和新交互方式的场景，也适合作为新型 Agent 工作流的探索环境。",
    badge: "NEW",
    notice: "新一代插件化 Agent Harness，能力与插件生态仍在快速演进。",
    theme: {
      accent: "text-teal-600",
      accentSoft: "bg-teal-50",
      border: "border-teal-200",
      dot: "bg-teal-500",
      selection: "border-teal-300 bg-teal-50/70 shadow-[inset_4px_0_0_#14b8a6]",
    },
  },
  workbuddy: {
    id: "workbuddy",
    name: "WorkBuddy",
    category: "智能办公搭档",
    tagline: "围绕信息、文档与事务持续协作",
    positioning:
      "WorkBuddy 是面向个人日常工作的智能办公伙伴。它围绕文档、资料、沟通内容和待办事项理解工作上下文，协助整理信息、生成内容、跟进事务并沉淀项目成果，让分散的办公任务形成连续、可回顾的工作过程。",
    selectionGuide:
      "当工作核心是信息、文档、汇报和事务推进时，优先选择 WorkBuddy。它关注个人办公效率与持续协作，不以代码仓库和软件交付为中心。",
    capabilities: [
      "文档理解与内容生成",
      "信息整理与归纳",
      "办公事务协同",
      "项目资料沉淀",
      "连续任务跟进",
      "个人工作上下文",
    ],
    scenarios:
      "适合文档撰写、会议纪要、工作汇报、资料分类、信息总结、项目跟踪、任务梳理和日常事务处理，也适合围绕同一主题长期积累资料与阶段成果。",
    theme: {
      accent: "text-amber-700",
      accentSoft: "bg-amber-50",
      border: "border-amber-200",
      dot: "bg-amber-500",
      selection: "border-amber-300 bg-amber-50/70 shadow-[inset_4px_0_0_#f59e0b]",
    },
  },
};

const fallbackRuntime: IEIRuntimePresentation = {
  id: "unknown",
  name: "自定义 Runtime",
  category: "受管工作空间",
  tagline: "按需接入的自定义能力空间",
  positioning: "该实例使用扩展 Runtime，具体工具、交互方式和任务能力由当前 Runtime 提供。",
  selectionGuide: "适合需要企业自定义能力、专用工具或特定工作流程的使用场景。",
  capabilities: ["独立工作区", "生命周期管理", "安全访问", "文件管理"],
  scenarios: "适用于企业自定义镜像和后续接入的新型 Runtime。",
  theme: {
    accent: "text-slate-600",
    accentSoft: "bg-slate-100",
    border: "border-slate-200",
    dot: "bg-slate-500",
    selection: "border-slate-300 bg-slate-50 shadow-[inset_4px_0_0_#64748b]",
  },
};

export function getIEIRuntimePresentation(type: string): IEIRuntimePresentation {
  const normalized = type.trim().toLowerCase();
  const configured = runtimeCatalog[normalized];
  if (configured) return configured;
  return {
    ...fallbackRuntime,
    id: normalized || fallbackRuntime.id,
    name: type.trim() || fallbackRuntime.name,
  };
}
