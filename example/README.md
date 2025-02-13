# ProxyClient Examples

这个目录包含了三个使用 ProxyClient 的示例程序：

## 1. Curl 示例

模拟 curl 命令，通过代理访问指定的 URL。

```bash
cd curl
go build
./curl <proxy-url> <target-url>

# 示例
./curl http://127.0.0.1:8080 https://example.com
```

## 2. NC 示例

模拟 nc 命令，通过代理连接指定的主机和端口。

```bash
cd nc
go build
./nc <proxy-url> <target-host> <target-port>

# 示例
./nc http://127.0.0.1:8080 example.com 80
```

## 3. SOCKS5 服务器示例

在本地启动一个 SOCKS5 服务器，将所有流量通过上游代理转发。

```bash
cd socks5
go build
./socks5 <proxy-url> <listen-addr>

# 示例
./socks5 http://127.0.0.1:8080 :1080
```

## 编译所有示例

```bash
cd example
go mod tidy
go build ./...
```

## 注意事项

1. 所有示例都支持各种类型的代理协议（HTTP、HTTPS、SOCKS5 等）
2. 代理 URL 格式：`<protocol>://<host>:<port>`
3. 如果代理需要认证，可以在 URL 中包含用户名和密码：`<protocol>://<username>:<password>@<host>:<port>`
