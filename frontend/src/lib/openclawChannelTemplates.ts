export type OpenClawChannelTemplateCategory = 'builtin' | 'plugin';

export interface OpenClawChannelTemplate {
  id: string;
  label: string;
  description: string;
  category: OpenClawChannelTemplateCategory;
  resourceKey: string;
  resourceName: string;
  resourceDescription: string;
  tags: string[];
  config: Record<string, unknown>;
}

const createChannelTemplate = (
  id: string,
  label: string,
  description: string,
  category: OpenClawChannelTemplateCategory,
  config: Record<string, unknown>,
): OpenClawChannelTemplate => ({
  id,
  label,
  description,
  category,
  resourceKey: id,
  resourceName: label,
  resourceDescription: `${label} starter template imported from the ClawPanel channel presets.`,
  tags: ['channel', category, id],
  config,
});

export const OPENCLAW_CHANNEL_TEMPLATES: OpenClawChannelTemplate[] = [
  createChannelTemplate('telegram', 'Telegram', 'Telegram Bot API channel with DM and group policy controls.', 'builtin', {
    enabled: true,
    botToken: '',
    dmPolicy: 'open',
    allowFrom: ['*'],
  }),
  createChannelTemplate('dingtalk-connector', 'DingTalk', 'DingTalk channel with client credentials and sender allowlist controls.', 'builtin', {
    enabled: true,
    clientId: '',
    clientSecret: '',
    allowFrom: ['*'],
  }),
  createChannelTemplate('slack', 'Slack', 'Slack workspace app powered by Bolt.', 'builtin', {
    enabled: true,
    appToken: '',
    botToken: '',
    groupPolicy: 'allowlist',
    channels: {
      '#general': {
        allow: true,
      },
    },
    capabilities: {
      interactiveReplies: true,
    },
  }),
  createChannelTemplate('feishu', 'Feishu / Lark', 'Feishu or Lark plugin channel with account-aware defaults.', 'plugin', {
    enabled: true,
    accounts: {
      main: {
        appId: '',
        appSecret: '',
      },
    },
  }),
];

export const OPENCLAW_CHANNEL_TEMPLATE_CATEGORY_LABELS: Record<OpenClawChannelTemplateCategory, string> = {
  builtin: 'Built-in Channels',
  plugin: 'Plugin Channels',
};

export const findOpenClawChannelTemplate = (id: string): OpenClawChannelTemplate | undefined =>
  OPENCLAW_CHANNEL_TEMPLATES.find((item) => item.id === id);
