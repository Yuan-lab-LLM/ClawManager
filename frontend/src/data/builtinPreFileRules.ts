import type { PreFileRule } from '../types/hostHardening';

/**
 * KSec ac.yaml `preFileList.rules` 出厂模板（共 38 条），
 * 镜像 KSecMain/packaging/ac.yaml。
 *
 * 用作"可启停规则全集"的稳定数据源：
 *  - 前端表格按此列表渲染所有 38 行
 *  - 每行 Toggle 的开关状态 = path 是否在 effective FilePolicy.preFileList.rules 里
 *  - Toggle off → 保存时从 preFileList.rules 中剔除（ac.yaml 不再下发该条）
 *  - Toggle on  → 保存时把模板中对应条目追加回 preFileList.rules
 *
 * 当 KSec 更新模板时，同步更新此常量。
 */
export const BUILTIN_PREFILE_TEMPLATE: PreFileRule[] = [
  { path: '/usr/bin', mode: 'rx', desc: '禁止在系统目录中创建和修改可执行文件' },
  { path: '/usr/sbin', mode: 'rx', desc: '禁止在系统目录中创建和修改可执行文件' },
  { path: '/etc/audit', mode: 'rx', desc: '禁止修改日志和审计配置文件' },
  { path: '/boot', mode: 'rx', desc: '禁止修改grub配置和内核镜像' },
  { path: '/etc/inittab', mode: 'rx', desc: '禁止修改系统运行级别配置文件' },
  { path: '/lib/systemd/system/graphical.target', mode: 'rx', desc: '禁止修改系统运行级别配置文件' },
  { path: '/usr/lib/systemd/system/graphical.target', mode: 'rx', desc: '禁止修改系统运行级别配置文件' },
  { path: '/lib/systemd/system/multi-user.target', mode: 'rx', desc: '禁止修改系统运行级别配置文件' },
  { path: '/usr/lib/systemd/system/multi-user.target', mode: 'rx', desc: '禁止修改系统运行级别配置文件' },
  { path: '/etc/dnf/dnf.conf', mode: 'rx', desc: '禁止修改软件更新配置' },
  { path: '/etc/yum', mode: 'rx', desc: '禁止修改软件更新配置' },
  { path: '/etc/yum.repos.d', mode: 'rx', desc: '禁止修改软件更新配置' },
  { path: '/etc/apt', mode: 'rx', desc: '禁止修改软件更新配置' },
  { path: '/etc/cron.deny', mode: 'rx', desc: '禁止修改定时任务配置' },
  { path: '/etc/crontab', mode: 'rx', desc: '禁止修改定时任务配置' },
  { path: '/etc/cron.d', mode: 'rx', desc: '禁止修改定时任务配置' },
  { path: '/etc/cron.daily', mode: 'rx', desc: '禁止修改定时任务配置' },
  { path: '/etc/cron.hourly', mode: 'rx', desc: '禁止修改定时任务配置' },
  { path: '/etc/cron.monthly', mode: 'rx', desc: '禁止修改定时任务配置' },
  { path: '/etc/cron.weekly', mode: 'rx', desc: '禁止修改定时任务配置' },
  { path: '/var/spool/cron', mode: 'rx', desc: '禁止修改定时任务配置' },
  { path: '/etc/anacrontab', mode: 'rx', desc: '禁止修改定时任务配置' },
  { path: '/etc/pam.d', mode: 'rx', desc: '禁止修改登陆认证配置' },
  { path: '/etc/ld.so.preload', mode: 'rx', desc: '禁止修改动态库配置文件' },
  { path: '/etc/ssh', mode: 'rx', desc: '禁止修改ssh配置文件' },
  { path: '/etc/profile', mode: 'rx', desc: '禁止修改Bash配置文件' },
  { path: '/etc/profile.d', mode: 'rx', desc: '禁止修改Bash配置文件' },
  { path: '/etc/bash.bashrc', mode: 'rx', desc: '禁止修改Bash配置文件' },
  { path: '/etc/bashrc', mode: 'rx', desc: '禁止修改Bash配置文件' },
  { path: '/root/.bashrc', mode: 'rx', desc: '禁止修改Bash配置文件' },
  { path: '/root/.bash_profile', mode: 'rx', desc: '禁止修改Bash配置文件' },
  { path: '/etc/resolv.conf', mode: 'r', desc: '禁止修改域名解析文件' },
  { path: '/run/resolvconf/resolv.conf', mode: 'r', desc: '禁止修改域名解析文件' },
  { path: '/etc/host.conf', mode: 'r', desc: '禁止修改域名解析文件' },
  { path: '/etc/hosts.allow', mode: 'r', desc: '禁止修改允许/限制访问配置文件' },
  { path: '/etc/hosts.deny', mode: 'r', desc: '禁止修改允许/限制访问配置文件' },
  { path: '/proc/kallsyms', desc: '禁止查看内存镜像和内核导出符号' },
  { path: '/usr/bin/ksecgui', mode: 'all', desc: '允许修改ksecgui' },
];
