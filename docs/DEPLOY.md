# 部署指南：Azure Container Apps

mdbox 是**有状态、单实例**服务（内存索引 + 内存会话 + 磁盘文件），不能像无状态服务那样随口扩缩。本文给出在 Azure 上的落地方式：

```
GitHub push(main) ──► GitHub Actions ──► Azure ACR（镜像）
                                              │ pull
                                              ▼
                                   Azure Container Apps（单副本，端口 8080）
                                              │ 挂载
                                              ▼
                                   Azure Files（持久卷 /data、/config）
```

- **镜像**：`.github/workflows/docker-acr.yml` 在 push 到 `main` 或打 `v*` tag 时构建并推送到 ACR。
- **运行**：Container Apps 单副本运行，镜像里 `ENTRYPOINT` 固定监听 **8080**。
- **持久化**：普通文件系统 I/O，必须挂 Azure Files；**不要**放在 Blob/对象存储上。

> 若只是本机跑，看 README 的「快速开始」即可，不需要本文。

---

## 前置条件

- 一个 Azure 订阅，可用 [Azure Portal](https://portal.azure.com) 或 `az` CLI（本文两者都给）。
- 能推代码的 GitHub 仓库（CI 已包含在仓库里）。
- 知道容器内运行的是**非 root 用户 `mdbox`**，这会影响第 6 步的挂载权限。

下文把这两个占位符换成你自己的：

| 占位符 | 含义 | 示例 |
|---|---|---|
| `<RG>` | 资源组 | `mdbox-rg` |
| `<ACR>` | 容器注册表名（全局唯一，仅小写字母数字） | `mdboxacr` |

---

## 第 1 步：创建 ACR，并配置 GitHub Secrets

**Portal**：创建 **Container Registry**（SKU 选 Basic 即可）→ 进入该注册表 → **Settings → Access keys** → 打开 **Admin user**，记下：

- **Login server**：形如 `<ACR>.azurecr.io`
- **Username** / **password**：Admin user 的用户名与密码

**CLI 等价**：

```bash
az group create -n <RG> -l eastasia
az acr create -n <ACR> -g <RG> --sku Basic --admin-enabled true
az acr credential show -n <ACR> --query "{server:username,user:username,pass:passwords[0].value}"
# login server：az acr show -n <ACR> --query loginServer -o tsv
```

**在 GitHub 仓库 → Settings → Secrets and variables → Actions** 添加三个 Secret（名字必须一致，CI 里写死了）：

| Secret | 值 |
|---|---|
| `ACR_LOGIN_SERVER` | `<ACR>.azurecr.io` |
| `ACR_USERNAME` | Admin user 用户名 |
| `ACR_PASSWORD` | Admin user 密码 |

之后每次 push 到 `main`，CI 会跑 `go vet ./...` + `go test ./...`，通过后构建镜像并推送（tag：`latest`、`sha-<短哈希>`，打 `v*` tag 时另有版本号）。也可以在 Actions 页手动 `workflow_dispatch` 触发。

> Container Apps 拉取私有镜像需要凭据。最省事的是用上面的 **ACR Admin 凭据**；更规范的做法是给环境/应用配 **Managed Identity** 并授予 `AcrPull`，即可免密拉取。

---

## 第 2 步：创建存储账户 + 文件共享

**Portal** → 创建 **Storage account**（Standard 即可）→ 进入该账户 → **Data storage → File shares → + File share**，建**两个**：

| 共享名 | 挂载点 | 用途 |
|---|---|---|
| `mdbox-data` | `/data` | 文档（`users/<name>/docs`、`archive`） |
| `mdbox-config` | `/config` | `config.yaml` + `users.yaml`（含密码哈希/token） |

> ⚠️ **必须是「经典」文件共享**（`Microsoft.Storage/storageAccounts/fileServices/shares`）。Container Apps 不支持新的顶层 `Microsoft.FileShares` 资源，挂了会直接失败。Portal 里用「File shares」建出来的就是经典共享。

拆成两个共享是为了让 `users.yaml` / `config.yaml` 落在 `/config`，不混进 `/data` 的 git 备份仓库。

**CLI 等价**：

```bash
SA=mdboxsa$RANDOM
az storage account create -g <RG> -n $SA --kind StorageV2 --sku Standard_LRS --enable-large-file-share
KEY=$(az storage account keys list -n $SA --query "[0].value" -o tsv)
az storage share-rm create -g <RG> --storage-account $SA -n mdbox-data   --quota 32 --enabled-protocols SMB
az storage share-rm create -g <RG> --storage-account $SA -n mdbox-config --quota 1  --enabled-protocols SMB
```

---

## 第 3 步：创建 Container Apps 环境

Portal → 创建 **Container Apps Environment**（消费计划即可）。这一步只需建一次，之后所有存储链接都挂在环境上。

**CLI**：`az containerapp env create -n mdbox-env -g <RG> -l eastasia`

---

## 第 4 步：把文件共享链接到环境

**Portal** → 打开 **Container Apps Environment** → 左侧 **Settings** 下选 **Volume mounts**（旧版界面可能叫 **Storage** / **Azure Files**）→ **Add**：

- 协议：**Server Message Block (SMB)**
- **Name**：环境内引用名，填 `data-mount`
- **Storage account name** / **Storage account key** / **File share**：填存储账户与 `mdbox-data`
- **Access mode**：**Read/Write**

保存。对 `mdbox-config` 重复一次，Name 用 `config-mount`。

> 部分 portal 版本可直接在 **Container App → Settings → Storage mounts** 里加，效果相同，二选一。

**CLI 等价**：

```bash
az containerapp env storage set -n mdbox-env -g <RG> \
  --storage-name data-mount   --access-mode ReadWrite \
  --azure-file-account-name $SA --azure-file-account-key "$KEY" --azure-file-share-name mdbox-data
az containerapp env storage set -n mdbox-env -g <RG> \
  --storage-name config-mount --access-mode ReadWrite \
  --azure-file-account-name $SA --azure-file-account-key "$KEY" --azure-file-share-name mdbox-config
```

---

## 第 5 步：创建 Container App 并挂载

镜像建议先用第 1 步 CI 推上去的 `<ACR>.azurecr.io/mdbox:latest`。

**Portal** → 创建 **Container App**，基本配置：

- **Ingress**：Enabled，外部，**Target port = 8080**（镜像里写死的监听端口）
- **Replicas**：**min = 1，max = 1**（见「日常运维」一节，别开自动扩缩）
- **Registry**：选 ACR 并填凭据，或配 Managed Identity

然后在 **Application → Revisions and replicas → Create new revision**：

1. **Volumes** 标签页 → **Add**，加两个卷（类型选 **Azure file volume**）：
   - Name `data-vol` → File share `mdbox-data`
   - Name `config-vol` → File share `mdbox-config`
   - **Mount options**：见第 6 步（**必填**，否则非 root 用户写不进去）
2. **Container** 标签页 → 选中容器 → **Volume mounts** 标签页 → 加两条：
   - `data-vol` → Mount path `/data`
   - `config-vol` → Mount path `/config`
3. **Create** 创建新修订版。

> **改挂载 / 环境变量 / 镜像后都必须发新修订版**，仅在旧修订版里改配置不会生效。

---

## 第 6 步：权限（非 root 用户的坑，必看）

Dockerfile 用 `USER mdbox`（`adduser -S` 创建的非 root 用户）运行。而 Azure Files SMB 默认挂载出来是 **root 属主、0755 权限**，`mdbox` 无法在 `/config` 写 `config.yaml`、也无法在 `/data` 下建 `users/<name>/`，应用会启动失败或注册报错。

**解法**：在卷的 **Mount options** 里填（两个卷都填）：

```
uid=<mdbox的uid>,gid=<mdbox的gid>,file_mode=0777,dir_mode=0777,nobrl,mfsymlinks,cache=strict
```

- `uid` / `gid`：让文件归属容器内的 `mdbox` 用户；
- `file_mode` / `dir_mode`：给足读写权限（也可收紧为 `0755` + 匹配的 uid/gid）；
- `nobrl` / `mfsymlinks` / `cache=strict`：官方对 SMB 的推荐项，规避 POSIX 锁与符号链接问题。

**`mdbox` 的 uid/gid 是多少**：Dockerfile 没写死，由 `adduser -S` 自动分配（常见为 `100:100`，但**以实测为准**）。任选一种方式查：

```bash
# 本地（需 Docker Desktop 已启动）
docker build -t mdbox . && docker run --rm --entrypoint id mdbox mdbox

# 或对已部署的应用
az containerapp exec -n mdbox-app -g <RG> --command "id mdbox"
```

把输出的 uid/gid 填进上面的 `uid=` / `gid=`。

> 备选（不推荐）：去掉 Dockerfile 里的 `USER mdbox` 改回 root 运行，则默认属主正好匹配，mount options 只需 `nobrl,mfsymlinks,cache=strict`。安全性会差一些。

---

## 第 7 步：验证

1. 记下当前时间，打开应用地址，**注册一个用户**。
2. 到 **Revisions and replicas** **重启**当前修订版（或再发一次新修订版）。
3. 用刚才的账号能登进去、文档还在 → 说明 `/config`（用户表）与 `/data`（文档）都持久化成功。
4. 进容器核对挂载与属主：

```bash
az containerapp exec -n mdbox-app -g <RG> --command "sh -c 'ls -la /data /config && id mdbox'"
```

---

## 日常运维与注意事项

- **保持单副本**：内存索引与登录会话都在进程内，横向扩容会让不同副本看到不同状态；也别用缩容到 0 的伸缩规则（会丢会话、冷启动重建索引）。`minReplicas = maxReplicas = 1`。
- **改配置要发新修订版**：挂载、环境变量、镜像标签的变更都只对新修订版生效。
- **换镜像**：CI 推 `latest` 后，在 **Revisions and replicas** 里新建修订版并用新镜像，或在 Container App 里改镜像后保存（会自动建新修订版）。
- **分享链接与 `secret`**：分享链接 = `HMAC(secret, user/id)`。**更换 `/config/config.yaml` 里的 `secret` 会让所有已分享链接立即失效**；反之不换就永久稳定。建议把 `config.yaml`、`users.yaml` 纳入备份。
- **容器内没有 git**：基础镜像是 `alpine`，**不含 git**，`scripts/git-backup.sh` 在容器里跑不起来。想要 git 版本备份需在 Dockerfile 里 `apk add git` 并自建定时任务；更省心的做法是直接用 **Azure Files 共享快照 / Azure 备份**做快照级备份。
- **`/config` 必须可写**：首次启动会在 `/config` 生成 `config.yaml`、`users.yaml`；只读挂载会导致启动失败。

---

## 附：用 YAML 一次更新（CLI 党）

Container Apps 的卷/挂载写在应用的 `template` 里，可直接导出改后回填：

```bash
az containerapp show -n mdbox-app -g <RG> -o yaml > app.yaml
```

在 `template` 段落里补上（`volumes` 引用第 4 步在环境里定义的 storage name）：

```yaml
template:
  containers:
    - name: mdbox
      image: <ACR>.azurecr.io/mdbox:latest
      volumeMounts:
        - volumeName: data-vol
          mountPath: /data
        - volumeName: config-vol
          mountPath: /config
  scale:
    minReplicas: 1
    maxReplicas: 1
  volumes:
    - name: data-vol
      storageType: AzureFile
      storageName: data-mount
    - name: config-vol
      storageType: AzureFile
      storageName: config-mount
      # mountOptions 也可写在这里（若 portal 填了可省略）
```

```bash
az containerapp update -n mdbox-app -g <RG> --yaml app.yaml
```

> 导出 YAML 时 **secrets 的值不会包含在内**：如果你要改某个 secret，务必在文件里同时给出它的 `name` 与 `value`；漏掉的 secret 会被删除。
