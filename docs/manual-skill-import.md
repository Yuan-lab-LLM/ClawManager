# 手动 Skill 导入链路

这条链路的目标很窄：允许操作者在本地拿到一个 Skill `.zip` 包后，把它导入 GTManager，再挂载到指定 OpenClaw 实例。

这条链路不依赖下列能力：

- `skill-scanner` 默认开启
- FastSkill 自动发现
- `/api/v1/admin/skills` 非空

## Skill 包放在哪里

Skill `.zip` 包先放在操作者自己的本地机器任意目录即可，例如：

```text
/Users/<you>/Downloads/my-skill-pack.zip
```

不要把 Skill 包手工拷进 repo、Pod 文件系统或 Kubernetes manifest。正确入口是由平台上传接口接收，GTManager 会自己存到对象存储。

也不要把 `ClawManager-full-workspace-*.tar.gz` 这种完整工程包当成 Skill 包上传。那个文件应该先在本地解压，然后按 `00-START-HERE.md` 部署平台。

## 包格式要求

- 只支持 `.zip`
- 压缩包里必须有一个或多个一级目录
- 每个一级目录会被导入为一个 skill
- 根目录下不能是散文件

示例：

```text
my-skill-pack.zip
├── hello-skill/
│   ├── README.md
│   └── manifest.json
└── weather-skill/
    └── manifest.json
```

上面的压缩包会导入出两个 skill：`hello-skill` 和 `weather-skill`。

## 导入到平台

### UI 路径

1. 登录 GTManager 的 `User` 界面
2. 进入 `OpenClaw Resource Management`
3. 在 Skill 区域上传 `.zip`
4. 上传完成后，平台会生成 `uploaded skill`

补充：

- `Manager` 界面负责系统设置、模型配置和管理员能力
- Skill 上传与实例 attach 属于用户侧操作，默认从 `User` 界面进入更直接

### API 路径

带登录态或 Bearer token 调用：

```bash
curl -k \
  -X POST "https://localhost:30443/api/v1/skills/import" \
  -H "Authorization: Bearer <access-token>" \
  -F "file=@/absolute/path/to/my-skill-pack.zip"
```

## scanner 关闭时的行为

如果当前默认发布路径里没有启用 `skill-scanner`，手动导入现在仍然允许成功。

导入后的 skill 会保持：

- `source_type=uploaded`
- `scan_status=pending`
- `risk_level=unknown`

这表示平台接受这个包，但还没有完成外部扫描。后续如果单独启用 scanner，可以再补扫描链。

## 如何发给实例

导入只完成“进入平台”，不会自动进入某个实例。下一步要做 attach：

1. 进入 `Create Instance` 页面，或某个实例的详情页
2. 选择刚刚导入出来的 `uploaded skill`
3. 执行 attach

对应 API：

```bash
curl -k \
  -X POST "https://localhost:30443/api/v1/instances/<instance-id>/skills" \
  -H "Authorization: Bearer <access-token>" \
  -H "Content-Type: application/json" \
  -d '{"skill_id": <skill-id>}'
```

## 平台内部发生了什么

链路会按下面顺序推进：

1. GTManager 接收本地 `.zip`
2. 为每个一级目录生成一个 `uploaded skill`
3. 写入 `skill_blobs` 和 `skill_versions`
4. attach 到实例时写入 `instance_skills`
5. GTManager 下发 `install_skill` 命令
6. runtime agent 再从平台下载对应 skill version 并安装到实例

所以，Skill 包的唯一人工动作只有两步：

1. 本地上传到平台
2. 在目标实例上 attach

## 常见异常

如果实例侧安装命令报：

```text
skill md5 mismatch
```

先按 Skill 包完整性问题处理：

1. 重新导出 zip
2. 重新上传
3. 再次 attach

不要先把它归因为 scanner、FastSkill 或 A1 主链故障。

## 当前边界

这条手动链路成立，不等于下面这些能力已经进入默认发布范围：

- `skill-scanner` 默认启用
- FastSkill 自动发现
- frozen U3 验证
- `/api/v1/admin/skills` 展示自动发现目录
