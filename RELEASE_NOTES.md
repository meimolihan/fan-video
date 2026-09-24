单个视频上使用更多→刷新元数据，解决转码问题

```bash
docker pull mobufan/fan-video:latest
```
```bash
docker pull mobufan/fan-video:v1.4.2
```

```bash
docker pull ghcr.io/meimolihan/fan-video:latest
```
```bash
docker pull ghcr.io/meimolihan/fan-video:v1.4.2
```

## 二进制安装
```bash
bash -c "$(curl -sSL https://raw.githubusercontent.com/meimolihan/fan-video/main/scripts/install.sh)" -p 9060 -d /var/lib/fan-video
```

## 二进制卸载
```bash
bash -c "$(curl -sSL https://raw.githubusercontent.com/meimolihan/fan-video/main/scripts/uninstall.sh)" -y --purge
```
