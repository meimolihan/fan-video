修复海报显示分辨率

```bash
docker pull mobufan/fan-video:latest
```
```bash
docker pull mobufan/fan-video:v1.3.8
```

```bash
docker pull ghcr.io/meimolihan/fan-video:latest
```
```bash
docker pull ghcr.io/meimolihan/fan-video:v1.3.8
```

## 二进制安装
```bash
bash -c "$(curl -sSL https://raw.githubusercontent.com/meimolihan/fan-video/main/scripts/install.sh)" -p 9060 -d /var/lib/fan-video
```

## 二进制卸载
```bash
bash -c "$(curl -sSL https://raw.githubusercontent.com/meimolihan/fan-video/main/scripts/uninstall.sh)" -y --purge
```
