/**
 * 入侵检测策略数据来源（与 KSecGUI 解耦）：
 *
 *   - INVASION_PRISTINE_BODY: 出厂 ids.yaml 的所有 macro / list / rule 块
 *     （已剔除 3 个 whitelist 块，由用户态接管）。
 *     底层 YAML 取自 KSec 仓库 `KSecMain/packaging/falco/ids.yaml`，
 *     通过 vite `?raw` 在前端 build 时内联。**这是 Falco 唯一信任的真相源**——
 *     KSecGUI 的 invasionPolicy.js 已经跟 KSec 出厂 ids.yaml 漂了（多出
 *     %fd.cip/%fd.sip/%fd.lip/%fd.rip/%container.info 等 Falco 编译器拒收的 token）。
 *
 *   - INVASION_RULES_META: 17 条规则的展示元数据（中文 name / 类型 / icon / 描述 / ruleName）
 *     来源 KSecGUI invasionPolicy.js——纯前端字段，与 Falco YAML 无关。
 *
 * 保存时的合成顺序（与 KSecGUI Invasion.vue setPolicy() 对齐）：
 *     [3 个用户 whitelist 块] + [INVASION_PRISTINE_BODY 里的 macro / 其他 list]
 *     + [INVASION_PRISTINE_BODY 里的 rule 块, 按 enabledRuleNames 过滤]
 */

import yaml from 'js-yaml';
// 前端 build 时把整份 YAML 文本以字符串形式内联进 bundle
// eslint-disable-next-line @typescript-eslint/ban-ts-comment
// @ts-ignore — vite `?raw` import 的运行时由 Vite 处理
import idsTemplateRaw from './ids-template.yaml?raw';

// =============== 类型 ===============

export interface MacroBlock { macro: string; condition?: string }
export interface ListBlock { list: string; items: Array<string | number> }
export interface RuleBlock {
  rule: string;
  desc?: string;
  condition?: string;
  output?: string;
  priority?: string;
  tags?: string[];
  enabled?: boolean;
}
export type YamlBlock = MacroBlock | ListBlock | RuleBlock | Record<string, unknown>;

/** UI 展示元数据，每条规则一份 */
export interface RuleMeta {
  /** Falco rule 名（必须与 ids.yaml 中 `- rule: <name>` 完全一致，用作启用态识别 key） */
  ruleName: string;
  /** 中文展示名 */
  name: string;
  /** 攻击类型分组（反弹shell / 本地提权 / 进程注入 …） */
  type: string;
  /** 一句话描述 */
  desc: string;
  /** KSecGUI 字体图标名（本工程目前不渲染图标，保留供未来扩展） */
  icon: string;
}

// =============== 17 条规则元数据（来自 KSecGUI invasionPolicy.js）===============

export const INVASION_RULES_META: RuleMeta[] = [
  { ruleName: 'System procs network activity', name: '反弹shell网络链接', type: '反弹shell', desc: '检测进程的非法网络连接行为', icon: '#icon-fantanshell' },
  { ruleName: 'Fileless execution via memfd_create', name: '二进制文件内存执行', type: '无文件执行', desc: '检测使用 memfd_create 创建匿名内存文件并在其中执行代码的行为', icon: '#icon-wuwenjianzhixing' },
  { ruleName: 'Non sudo setuid', name: '通过setuid更改用户的提权', type: '本地提权', desc: '检测使用 setuid 改变用户权限的行为', icon: '#icon-benditiquan' },
  { ruleName: 'Set Setuid or Setgid bit', name: '通过chmod设置setgid或setuid位的提权', type: '本地提权', desc: '检测使用 chmod 设置 setuid 或 setgid 位权限的行为', icon: '#icon-benditiquan' },
  { ruleName: 'Sudoers file modification detected', name: '通过修改sudoers file获取权限的尝试', type: '本地提权', desc: '检测修改 sudoers 文件提升权限的行为', icon: '#icon-benditiquan' },
  { ruleName: 'PTRACE attached to process', name: '利用ptrace进程注入', type: '进程注入', desc: '检测使用 ptrace 向进程注入代码的行为', icon: '#icon-jinchengzhuru' },
  { ruleName: 'Proc Memory attached to process', name: 'Proc Memory类型进程注入', type: '进程注入', desc: '检测篡改进程内存数据注入代码的行为', icon: '#icon-jinchengzhuru' },
  { ruleName: 'Create Hardlink Over Sensitive Files', name: '敏感文件创建硬链接', type: '敏感文件泄漏', desc: '检测创建 /etc 或根目录下敏感文件硬链接的行为', icon: '#icon-minganwenjian' },
  { ruleName: 'Create Symlink Over Sensitive Files', name: '敏感文件创建软连接', type: '敏感文件泄漏', desc: '检测创建 /etc 或根目录下敏感文件/目录软链接的行为', icon: '#icon-minganwenjian' },
  { ruleName: 'Create files below dev', name: '/dev目录下文件创建', type: 'rootkit攻击', desc: '检测 /dev 目录下创建文件的行为', icon: '#icon-rootkit' },
  { ruleName: 'Write below binary dir', name: '二进制目录下创建文件', type: 'rootkit攻击', desc: '检测 /bin 等目录下创建文件的行为', icon: '#icon-rootkit' },
  { ruleName: 'Modify Ld preload file', name: '/etc/ld.so.preload文件写入行为', type: 'rootkit攻击', desc: '检测篡改 /etc/ld.so.preload 文件的行为', icon: '#icon-rootkit' },
  { ruleName: 'Linux Kernel Module Injection Detected', name: '加载内核模块的行为', type: '内核模块加载', desc: '检测使用 insmod 或 modprobe 加载内核模块的行为', icon: '#icon-neihemokuaijiazai' },
  { ruleName: 'Clear Log Activities', name: '清理日志', type: '痕迹擦除', desc: '检测清除系统审计日志的行为', icon: '#icon-henjicachu' },
  { ruleName: 'Schedule Cron Jobs', name: '创建或修改计划任务', type: '计划任务篡改', desc: '检测创建或修改计划任务的行为', icon: '#icon-jihuarenwu' },
  { ruleName: 'Execution from /dev/shm', name: '从/dev/shm目录执行', type: 'shm目录非法执行', desc: '检测 /dev/shm 目录下执行文件的行为', icon: '#icon-feifazhixing' },
  { ruleName: 'Launch Suspicious Network Tool on Host', name: '在主机上启动可疑的网络工具', type: '可疑工具执行', desc: '检测主机/容器启动可疑网络工具的行为', icon: '#icon-keyigongju' },
];

// =============== 出厂 ids.yaml 解析 ===============

const PRISTINE_ALL: YamlBlock[] = (() => {
  const parsed = yaml.load(idsTemplateRaw as unknown as string);
  return Array.isArray(parsed) ? (parsed as YamlBlock[]) : [];
})();

const USER_WHITELIST_LISTS = new Set(['whitelist_program_path', 'whitelist_file_path', 'whitelist_ip_address']);

/**
 * 出厂 ids.yaml 中除 3 个 whitelist 外的所有 macro / list / rule 块。
 * 保存时直接拼回——保留 KSec 出厂的 Falco 兼容内容（含 output/condition 中所有真实字段）。
 */
export const INVASION_PRISTINE_BODY: YamlBlock[] = PRISTINE_ALL.filter((b) => {
  if (b && typeof b === 'object' && 'list' in b && typeof (b as ListBlock).list === 'string') {
    return !USER_WHITELIST_LISTS.has((b as ListBlock).list);
  }
  return true;
});

// =============== 兼容旧导入（仅类型层 helper，运行时 buildInvasionYmlBody 已迁移）===============
// 保留一个最小化的 `invasionPolicy` 兼容导出，避免页面其他位置如有引用还能编译。
// 不再承载 17 条规则的完整 Falco 块——这些块全部由 INVASION_PRISTINE_BODY 提供。
export const invasionPolicy = {
  whiteList: [
    { list: 'whitelist_program_path', items: [] },
    { list: 'whitelist_file_path', items: [] },
    { list: 'whitelist_ip_address', items: [] },
  ] as ListBlock[],
  rules: INVASION_RULES_META.map((m) => ({ ruleName: m.ruleName, name: m.name, type: m.type, desc: m.desc, icon: m.icon })),
};
export const macroDefine: YamlBlock[] = []; // legacy 占位，已并入 INVASION_PRISTINE_BODY
export const miningPolicy: YamlBlock[] = []; // legacy 占位
