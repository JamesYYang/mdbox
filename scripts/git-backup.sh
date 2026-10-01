#!/usr/bin/env bash
# mdbox 数据目录的 git 自动备份脚本。
#
# 用法：
#   1) 首次把数据目录初始化成 git 仓库并关联远端私有仓库：
#        cd data && git init && git remote add origin git@github.com:<you>/<repo>.git
#   2) 加进 crontab，每 10 分钟备份一次：
#        */10 * * * * /path/to/mdbox/scripts/git-backup.sh /path/to/mdbox/data
#
# 说明：每篇文档自带 frontmatter 记录 created/updated，git 只负责版本与异地备份，
# 因此推送失败不影响服务继续运行。
set -euo pipefail

DATA_DIR="${1:-./data}"
cd "$DATA_DIR"

if [ ! -d .git ]; then
  echo "[git-backup] $DATA_DIR 不是 git 仓库，跳过。先执行：git init && git remote add origin <url>"
  exit 0
fi

git add -A

if git diff --cached --quiet; then
  exit 0  # 没有变更，不产生空提交
fi

git commit -q -m "chore: mdbox 自动备份 $(date '+%Y-%m-%d %H:%M:%S')"

if git remote get-url origin >/dev/null 2>&1; then
  git push -q origin HEAD 2>/dev/null && echo "[git-backup] 已推送 $(date '+%F %T')" \
    || echo "[git-backup] 推送失败，提交已保留在本地"
else
  echo "[git-backup] 未配置 remote，仅本地提交"
fi
