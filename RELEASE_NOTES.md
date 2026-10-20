release 流水线标准化：单条链路 + 常规检查 + 镜像冒烟 + sha256 附件

```bash
docker pull mobufan/fan-video:latest
```
```bash
docker pull mobufan/fan-video:v1.4.5
```

```bash
docker pull ghcr.io/meimolihan/fan-video:latest
```
```bash
docker pull ghcr.io/meimolihan/fan-video:v1.4.5
```

## 二进制安装
```bash
bash -c "$(curl -sSL https://raw.githubusercontent.com/meimolihan/fan-video/main/scripts/install.sh)" -p 9060 -d /var/lib/fan-video
```

## 二进制卸载
```bash
bash -c "$(curl -sSL https://raw.githubusercontent.com/meimolihan/fan-video/main/scripts/uninstall.sh)" -y --purge
```
