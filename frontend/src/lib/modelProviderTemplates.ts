export interface ProviderTemplate {
  id: string;
  label: string;
  providerType: string;
  baseUrl: string;
  allowCustomBaseUrl?: boolean;
  requiresApiKey?: boolean;
  keywords?: string[];
}

export const BUILTIN_PROVIDER_TEMPLATES: ProviderTemplate[] = [
  {
    id: 'openai',
    label: 'OpenAI',
    providerType: 'openai',
    baseUrl: 'https://api.openai.com/v1',
    requiresApiKey: true,
    keywords: ['chatgpt', 'gpt'],
  },
  {
    id: 'openrouter',
    label: 'OpenRouter',
    providerType: 'openai-compatible',
    baseUrl: 'https://openrouter.ai/api/v1',
    requiresApiKey: true,
    keywords: ['router', 'aggregator'],
  },
  {
    id: 'deepseek',
    label: 'DeepSeek',
    providerType: 'openai-compatible',
    baseUrl: 'https://api.deepseek.com',
    requiresApiKey: true,
    keywords: ['deepseek', 'coder', 'chat', 'shen du qiu suo', '深度求索'],
  },
  {
    id: 'siliconflow',
    label: 'SiliconFlow',
    providerType: 'openai-compatible',
    baseUrl: 'https://api.siliconflow.cn/v1',
    requiresApiKey: true,
    keywords: ['siliconflow', 'silicon', 'liu dong', '硅基流动'],
  },
  {
    id: 'moonshot',
    label: 'Moonshot AI',
    providerType: 'openai-compatible',
    baseUrl: 'https://api.moonshot.cn/v1',
    requiresApiKey: true,
    keywords: ['moonshot', 'kimi', 'yue zhi an mian', '月之暗面'],
  },
  {
    id: 'zhipu',
    label: 'Zhipu AI',
    providerType: 'openai-compatible',
    baseUrl: 'https://open.bigmodel.cn/api/paas/v4',
    requiresApiKey: true,
    keywords: ['zhipu', 'glm', 'bigmodel', 'zhi pu', '智谱'],
  },
  {
    id: 'dashscope',
    label: 'Alibaba DashScope',
    providerType: 'openai-compatible',
    baseUrl: 'https://dashscope.aliyuncs.com/compatible-mode/v1',
    requiresApiKey: true,
    keywords: ['dashscope', 'qwen', 'tongyi', 'tong yi', '通义'],
  },
  {
    id: 'ark',
    label: 'Volcengine Ark',
    providerType: 'openai-compatible',
    baseUrl: 'https://ark.cn-beijing.volces.com/api/v3',
    requiresApiKey: true,
    keywords: ['ark', 'doubao', 'volcengine', 'huo shan', '火山'],
  },
  {
    id: 'groq',
    label: 'Groq',
    providerType: 'openai-compatible',
    baseUrl: 'https://api.groq.com/openai/v1',
    requiresApiKey: true,
    keywords: ['groq', 'llama'],
  },
  {
    id: 'together',
    label: 'Together AI',
    providerType: 'openai-compatible',
    baseUrl: 'https://api.together.xyz/v1',
    requiresApiKey: true,
    keywords: ['together', 'togetherai'],
  },
  {
    id: 'fireworks',
    label: 'Fireworks AI',
    providerType: 'openai-compatible',
    baseUrl: 'https://api.fireworks.ai/inference/v1',
    requiresApiKey: true,
    keywords: ['fireworks', 'fireworksai'],
  },
  {
    id: 'xai',
    label: 'xAI',
    providerType: 'openai-compatible',
    baseUrl: 'https://api.x.ai/v1',
    requiresApiKey: true,
    keywords: ['xai', 'grok'],
  },
  {
    id: 'perplexity',
    label: 'Perplexity',
    providerType: 'openai-compatible',
    baseUrl: 'https://api.perplexity.ai',
    requiresApiKey: true,
    keywords: ['perplexity', 'sonar'],
  },
  {
    id: 'lingyiwanwu',
    label: '01.AI',
    providerType: 'openai-compatible',
    baseUrl: 'https://api.lingyiwanwu.com/v1',
    requiresApiKey: true,
    keywords: ['01ai', 'lingyi', 'yi', 'ling yi wan wu', '零一万物'],
  },
  {
    id: 'minimax',
    label: 'MiniMax',
    providerType: 'openai-compatible',
    baseUrl: 'https://api.minimax.chat/v1',
    requiresApiKey: true,
    keywords: ['minimax', 'abab'],
  },
  {
    id: 'ollama',
    label: 'Local / Internal',
    providerType: 'local',
    baseUrl: 'http://localhost:11434/v1',
    allowCustomBaseUrl: true,
    requiresApiKey: false,
    keywords: ['ollama', 'localhost', 'local', 'internal'],
  },
];

export function normalizeBaseUrl(baseUrl: string) {
  return baseUrl.trim().replace(/\/+$/, '').toLowerCase();
}

export function findProviderTemplate(providerType: string, baseUrl: string) {
  const normalizedProviderType = providerType.trim().toLowerCase();
  const normalizedBaseUrl = normalizeBaseUrl(baseUrl);
  if (!normalizedProviderType || !normalizedBaseUrl) {
    return undefined;
  }

  const exactMatch = BUILTIN_PROVIDER_TEMPLATES.find((template) => (
    template.providerType === normalizedProviderType &&
    normalizeBaseUrl(template.baseUrl) === normalizedBaseUrl
  ));
  if (exactMatch) {
    return exactMatch;
  }

  return BUILTIN_PROVIDER_TEMPLATES.find((template) => (
    template.providerType === normalizedProviderType &&
    template.allowCustomBaseUrl
  ));
}
