/**
 * 入侵检测日志按规则名映射成中文「类别 + 描述」。
 * 镜像 KSecGUI/components/InvasionLog.vue 的 logRuleMap。
 *
 * 用法：
 *   const spec = INVASION_LOG_RULE_MAP[entry.rule];
 *   if (!spec) skip;            // 未知规则一律不展示
 *   const param = entry.output_fields[spec.paramKey];
 *   const desc  = spec.desc(param);
 */

export interface LogRuleSpec {
  /** 中文类别（"反弹shell" / "本地提权" 等），用作 UI 表格「类别」列 */
  ruleType: string;
  /** 主参数键：从 output_fields 里取值塞进 desc */
  paramKey?: string;
  /** 备选参数键（仅 Sudoers 用：fd.name 缺失时退到 proc.cmdline） */
  paramKey1?: string;
  /** 主描述生成 */
  desc: (param: string) => string;
  /** 备选描述（仅 Sudoers 用） */
  desc1?: (param: string) => string;
  /**
   * Linux Kernel Module Injection 专用：对 proc.cmdline 做参数过滤
   * （丢掉 `-` 开头的 flag，保留 module name + args 用顿号拼接）。
   */
  getParam?: (param: string) => string;
}

/** desc 的容器前缀：`检测到${containerDesc?容器XX中:''}<具体行为>`。 */
export const startDesc = (containerDesc = ''): string => `检测到${containerDesc}`;

export const INVASION_LOG_RULE_MAP: Record<string, LogRuleSpec> = {
  'System procs network activity': {
    ruleType: '反弹shell',
    paramKey: 'fd.name',
    desc: (p) => `反弹shell连接${p}`,
  },
  'Fileless execution via memfd_create': {
    ruleType: '无文件执行',
    desc: () => `使用memfd_create创建匿名内存文件并在其中执行代码`,
  },
  'Non sudo setuid': {
    ruleType: '本地提权',
    desc: () => `使用setuid改变当前用户权限`,
  },
  'Set Setuid or Setgid bit': {
    ruleType: '本地提权',
    paramKey: 'evt.arg.filename',
    desc: (p) => `通过设置${p}的setuid或setgid位提升权限`,
  },
  'Sudoers file modification detected': {
    ruleType: '本地提权',
    paramKey: 'fd.name',
    paramKey1: 'proc.cmdline',
    desc: (p) => `sudoers文件${p}被修改`,
    desc1: (p) => `sudoers文件被修改的提权行为${p}`,
  },
  'PTRACE attached to process': {
    ruleType: '进程注入',
    paramKey: 'proc.aexepath[0]',
    desc: (p) => `使用ptrace注入的可疑程序${p}`,
  },
  'Proc Memory attached to process': {
    ruleType: '进程注入',
    paramKey: 'proc.aexepath[0]',
    desc: (p) => `可能向另一个进程注入代码的可疑程序${p}`,
  },
  'Create Hardlink Over Sensitive Files': {
    ruleType: '敏感文件泄漏',
    paramKey: 'evt.arg.oldpath',
    desc: (p) => `系统文件${p}被创建硬链接`,
  },
  'Create Symlink Over Sensitive Files': {
    ruleType: '敏感文件泄漏',
    paramKey: 'evt.arg.target',
    desc: (p) => `系统文件${p}被创建软链接`,
  },
  'Create files below dev': {
    ruleType: 'rootkit攻击',
    paramKey: 'fd.name',
    desc: (p) => `设备文件${p}被创建`,
  },
  'Write below binary dir': {
    ruleType: 'rootkit攻击',
    paramKey: 'fd.name',
    desc: (p) => `可执行程序${p}被创建`,
  },
  'Modify Ld preload file': {
    ruleType: 'rootkit攻击',
    desc: () => `预加载配置文件/etc/ld.so.preload被修改`,
  },
  'Linux Kernel Module Injection Detected': {
    ruleType: '内核模块加载',
    paramKey: 'proc.cmdline',
    desc: (p) => `加载内核模块的行为${p}`,
    getParam: (cmdline) => {
      // 与 KSecGUI 一致：split() + 过滤 - 开头的 flag + 顿号拼接
      // (注：KSecGUI 用的是 `.split()`，未传分隔符在 JS 中按整串切——这里照搬同行为，
      //  实际只能拿到一整段；用空格切才有意义，这里取空格切。)
      const args = cmdline.split(/\s+/).filter((v) => v && !v.startsWith('-'));
      return args.join('，');
    },
  },
  'Clear Log Activities': {
    ruleType: '痕迹擦除',
    paramKey: 'fd.name',
    desc: (p) => `关键访问日志文件${p}被清除`,
  },
  'Schedule Cron Jobs': {
    ruleType: '计划任务篡改',
    paramKey: 'fd.name',
    desc: (p) => (p ? `计划任务配置文件${p}被修改` : '计划任务被修改'),
  },
  'Execution from /dev/shm': {
    ruleType: 'shm目录非法执行',
    paramKey: 'proc.aexepath[0]',
    desc: (p) => `${p}被执行`,
  },
  'Launch Suspicious Network Tool on Host': {
    ruleType: '可疑工具执行',
    paramKey: 'proc.name',
    desc: (p) => `可疑网络工具${p}被执行`,
  },
};

/** 从 /dev/shm 命令行里抓 /dev/shm/xxx 路径（与 KSecGUI shmMatch 一致）。 */
export function shmMatch(input: string): string[] {
  const pattern = /\/dev\/shm\/\S+/g;
  return input.match(pattern) ?? [];
}

// ============== 富化函数 ==============

/** UI 表格里一行的形态。 */
export interface InvasionLogRow {
  key: string;
  /** 已格式化（"2026-06-02 11:23:06"） */
  time: string;
  /** 中文类别 */
  ruleType: string;
  /** 进程（aexepath[0]） */
  source: string;
  /** 进程用户 */
  user: string;
  /** 中文描述（含可能的容器前缀） */
  desc: string;
  /** 进程调用链（aexepath[3] -> [2] -> [1] -> [0]） */
  processChain: string;
}

interface RawIdsLog {
  time?: string;
  rule?: string;
  output_fields?: Record<string, unknown>;
}

/**
 * 把 Falco 原始 IDS JSON 日志转成 UI 可直接渲染的 row。
 * 未知规则返回 null（与 KSecGUI `if(!logRule) return` 一致）。
 */
export function enrichInvasionLog(raw: RawIdsLog, idx: number): InvasionLogRow | null {
  if (!raw.rule || !raw.output_fields) return null;
  const spec = INVASION_LOG_RULE_MAP[raw.rule];
  if (!spec) return null;
  const of = raw.output_fields;
  const str = (k: string): string => {
    const v = of[k];
    return typeof v === 'string' ? v : '';
  };

  // 1) time: "2026-06-02T11:23:06.539444220Z" -> "2026-06-02 11:23:06"
  const time = (raw.time ?? '').replace('T', ' ').split('.')[0];

  // 2) source（进程）
  let source = str('proc.aexepath[0]') || '-';
  if (raw.rule === 'Fileless execution via memfd_create') source = '-';

  // 3) processChain
  const chainParts: string[] = [];
  const ae3 = str('proc.aexepath[3]');
  const ae2 = str('proc.aexepath[2]');
  const ae1 = str('proc.aexepath[1]');
  const ae0 = str('proc.aexepath[0]');
  if (ae3) chainParts.push(ae3);
  if (ae2) chainParts.push(ae2);
  if (ae1) chainParts.push(ae1);
  if (ae0) chainParts.push(ae0);
  const processChain = `进程调用关系：${chainParts.join(' -> ')}`;

  // 4) user
  const user = str('user.name') || '-';

  // 5) desc（含容器前缀 + 规则特殊分支）
  const containerId = str('container.id');
  const containerName = str('container.name');
  const containerDesc = containerId && containerId !== 'host' ? `容器${containerName || containerId}中` : '';
  const prefix = startDesc(containerDesc);

  let desc: string;
  if (raw.rule === 'Execution from /dev/shm') {
    // 单独处理 /dev/shm: 优先 aexepath[0]，否则 cmdline 抓 shm 路径，再否则兜底
    if (ae0.includes('/dev/shm')) desc = prefix + spec.desc(ae0);
    else {
      const cmdline = str('proc.cmdline');
      if (cmdline.includes('/dev/shm')) {
        const m = shmMatch(cmdline);
        desc = prefix + (m.length > 0 ? spec.desc(m[0]) : '/dev/shm目录下非法执行');
      } else {
        desc = prefix + '/dev/shm目录下非法执行';
      }
    }
  } else if (raw.rule === 'Sudoers file modification detected') {
    const p = str(spec.paramKey ?? '');
    if (p) desc = prefix + spec.desc(p);
    else desc = prefix + (spec.desc1?.(str(spec.paramKey1 ?? '')) ?? '');
  } else if (raw.rule === 'Linux Kernel Module Injection Detected') {
    const cmdline = str(spec.paramKey ?? '');
    const filtered = spec.getParam ? spec.getParam(cmdline) : cmdline;
    desc = prefix + spec.desc(filtered);
  } else {
    const p = spec.paramKey ? str(spec.paramKey) : '';
    desc = prefix + spec.desc(p);
  }

  return {
    key: `${time}#${idx}#${raw.rule}`,
    time,
    ruleType: spec.ruleType,
    source,
    user,
    desc,
    processChain,
  };
}
