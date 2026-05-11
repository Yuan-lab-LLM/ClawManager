# Skill Content MD5 Calculation Spec

本文定义 ClawManager / OpenClaw / Hermes 之间统一使用的 `content_md5` 计算方式。Hermes agent 的 inventory 上报、`collect_skill_package` 上传、`install_skill` 安装后校验，都必须使用同一套算法。

## 结论

`content_md5` 不是 zip 文件本身的 MD5，也不包含 zip entry 顺序、压缩等级、mtime、权限等元数据。它是 skill 目录内容的规范化 MD5。

上传 zip 时，zip 必须包含且只包含一个顶层 skill 目录。ClawManager 会先剥掉这个顶层 skill 目录，再对目录内部内容计算 `content_md5`。

例如上传包结构为：

```text
weather/
  skill.json
  src/
    main.py
```

实际参与 MD5 的路径是：

```text
skill.json
src
src/main.py
```

注意：只剥掉 zip 的顶层 skill 目录 `weather/` 一次，不要再剥掉 skill 内部的 `src/`、`lib/`、`dist/` 等目录。

## 规范化规则

1. 以 skill 根目录作为基准，收集所有普通文件。
2. 路径统一使用 POSIX `/` 分隔符。
3. 去掉路径开头的 `./`，并做 clean 处理。
4. 跳过空路径、`.`、`..`、包含 `..` 越界语义的路径。
5. 跳过任意路径段以 `.` 开头的文件和目录，例如 `.git/config`、`.cache/a`、`.DS_Store`。
6. 目录项不直接从文件系统读取，而是由文件路径的父目录推导出来。
7. 将目录项和文件项放在同一个列表中，按规范化路径字典序升序排序。
8. 对每个目录项写入以下字节：

```text
{relative_path}\n
dir\n
```

9. 对每个文件项写入以下字节：

```text
{relative_path}\n
file\n
{raw_file_bytes}
\n
```

10. 对上述连续字节流计算 MD5，输出 32 位小写 hex 字符串。

不要改写文件内容。不要转换换行符，不要格式化 JSON，不要忽略空文件，不要把文件权限、mtime、owner、zip 压缩参数写入 digest。

## Hermes Agent Checklist

Hermes agent 需要检查下面几个点：

- inventory 上报的 `content_md5` 应该对 `/config/.hermes/skills/{skill_name}` 目录内部内容计算。
- 上传 `collect_skill_package` zip 时，zip 内应该有一个顶层目录 `{skill_name}/`。
- inventory 阶段和上传阶段必须使用同一份 skill 目录内容计算 MD5。
- 如果本地目录是 `/config/.hermes/skills/weather/src/main.py`，参与 MD5 的路径必须是 `src/main.py`，不是 `weather/src/main.py`，也不是 `main.py`。
- 如果 `content_md5` 和 ClawManager 返回的 expected 不一致，先检查是否多剥或少剥了顶层目录，其次检查是否把隐藏目录、文件元数据或 zip bytes 算进去了。

## Python Reference Implementation

下面实现可直接给 Hermes agent 端对齐算法：

```python
import hashlib
from pathlib import Path


def _is_hidden_relative_path(rel: str) -> bool:
    return any(part.startswith(".") for part in rel.split("/"))


def skill_content_md5(skill_dir: str | Path) -> str:
    root = Path(skill_dir).resolve()
    files: dict[str, bytes] = {}
    dirs: set[str] = set()

    for path in sorted(root.rglob("*")):
        if not path.is_file():
            continue

        rel = path.relative_to(root).as_posix()
        if rel.startswith("./"):
            rel = rel[2:]
        if not rel or rel == "." or rel.startswith("../") or _is_hidden_relative_path(rel):
            continue

        files[rel] = path.read_bytes()
        parts = rel.split("/")
        for i in range(1, len(parts)):
            parent = "/".join(parts[:i])
            if parent and not _is_hidden_relative_path(parent):
                dirs.add(parent)

    entries: dict[str, str] = {rel: "file" for rel in files}
    for rel in dirs:
        entries[rel] = "dir"

    digest = hashlib.md5()
    for rel in sorted(entries):
        digest.update(rel.encode("utf-8"))
        digest.update(b"\n")
        if entries[rel] == "dir":
            digest.update(b"dir\n")
        else:
            digest.update(b"file\n")
            digest.update(files[rel])
            digest.update(b"\n")

    return digest.hexdigest()
```

## Zip Upload Reference

上传给 ClawManager 的 zip 应保持一个顶层目录：

```text
weather/
  skill.json
  src/main.py
```

Hermes agent 在本地计算 MD5 时应对目录 `/config/.hermes/skills/weather` 调用 `skill_content_md5()`。不要对 zip 文件调用 MD5。

如果 agent 需要在上传前自检，可以先把 zip 解开，确认去掉 `weather/` 后得到的文件列表与本地计算使用的相对路径一致。
