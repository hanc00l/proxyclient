package suo5

import (
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"github.com/chainreactors/proxyclient/suo5/netrans"
	utls "github.com/refraction-networking/utls"
	"github.com/zema1/rawhttp"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type Suo5Client struct {
	Proxy *url.URL
	Conf  *Suo5Conf
}

type Suo5Conf struct {
	normalClient    *http.Client
	noTimeoutClient *http.Client
	rawClient       *rawhttp.Client
	*Suo5Config
}

// NewConfFromURL 从URL中解析用户名密码生成配置
func NewConfFromURL(proxyURL *url.URL) (*Suo5Conf, error) {
	scheme := "http"
	switch strings.ToLower(proxyURL.Scheme) {
	case "suo5":
		scheme = "http"
	case "suo5s":
		scheme = "https"
	default:
		return nil, fmt.Errorf("unsupported scheme: %s", proxyURL.Scheme)
	}

	// 使用这些值构建配置
	config := DefaultSuo5Config()
	err := config.Parse()
	if err != nil {
		return nil, err
	}
	config.Target = fmt.Sprintf("%s://%s%s", scheme, proxyURL.Host, proxyURL.Path)

	if config.DisableGzip {
		config.Header.Set("Accept-Encoding", "identity")
	}

	tr := &http.Transport{
		TLSClientConfig: &tls.Config{
			MinVersion:         tls.VersionTLS10,
			Renegotiation:      tls.RenegotiateOnceAsClient,
			InsecureSkipVerify: true,
		},
		DialTLSContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			conn, err := net.DialTimeout(network, addr, 5*time.Second)
			if err != nil {
				return nil, err
			}
			colonPos := strings.LastIndex(addr, ":")
			if colonPos == -1 {
				colonPos = len(addr)
			}
			hostname := addr[:colonPos]
			tlsConfig := &utls.Config{
				ServerName:         hostname,
				InsecureSkipVerify: true,
				Renegotiation:      utls.RenegotiateOnceAsClient,
				MinVersion:         utls.VersionTLS10,
			}
			uTlsConn := utls.UClient(conn, tlsConfig, utls.HelloRandomizedNoALPN)
			if err = uTlsConn.HandshakeContext(ctx); err != nil {
				_ = conn.Close()
				return nil, err
			}
			return uTlsConn, nil
		},
	}
	if config.UpstreamProxy != "" {
		proxy := strings.TrimSpace(config.UpstreamProxy)
		if !strings.HasPrefix(proxy, "socks5") && !strings.HasPrefix(proxy, "http") {
			return nil, fmt.Errorf("invalid proxy, both socks5 and http(s) are supported, eg: socks5://127.0.0.1:1080")
		}
		config.UpstreamProxy = proxy
		u, err := url.Parse(config.UpstreamProxy)
		if err != nil {
			return nil, err
		}
		//logs.Log.Infof("using Upstream proxy %v", proxy)
		tr.Proxy = http.ProxyURL(u)
	}
	if config.Upstream != nil {
		tr.DialContext = config.Upstream
	}
	if config.RedirectURL != "" {
		_, err := url.Parse(config.RedirectURL)
		if err != nil {
			return nil, fmt.Errorf("failed to parse redirect url, %s", err)
		}
		//logs.Log.Infof("using redirect url %v", config.RedirectURL)
	}
	var jar http.CookieJar
	if config.EnableCookieJar {
		jar, _ = cookiejar.New(nil)
	} else {
		// 对 PHP的特殊处理一下, 如果是 PHP 的站点则自动启用 cookiejar, 其他站点保持不启用
		jar = NewSwitchableCookieJar([]string{"PHPSESSID"})
	}

	noTimeoutClient := &http.Client{
		Transport: tr.Clone(),
		Jar:       jar,
		Timeout:   0,
	}
	normalClient := &http.Client{
		Timeout:   time.Duration(config.Timeout) * time.Second,
		Jar:       jar,
		Transport: tr.Clone(),
	}
	rawClient := NewRawClient(config.UpstreamProxy, 0)
	// 构建 Suo5Conf，并初始化 HTTP 客户端
	suo5Conf := &Suo5Conf{
		Suo5Config:      config,
		normalClient:    normalClient,
		noTimeoutClient: noTimeoutClient,
		rawClient:       rawClient,
	}

	return suo5Conf, nil
}

// Dial 实现了Client接口
func (c *Suo5Client) Dial(network, address string) (net.Conn, error) {
	// 创建一个新的 suo5Conn 连接
	//conn, err := c.conf.Upstream(network, address)
	//if err != nil {
	//	return nil, err
	//}
	suo5Conn := &suo5Conn{
		Suo5Conf: c.Conf,
		ctx:      context.Background(),
	}

	// 发送连接请求
	if err := suo5Conn.connect(address); err != nil {
		return nil, err
	}

	return suo5Conn, nil
}

// suo5Conn 实现了net.Conn接口
type suo5Conn struct {
	io.ReadWriteCloser
	ctx    context.Context
	closed bool
	*Suo5Conf
}

func (conn *suo5Conn) connect(address string) error {
	id := RandString(8)
	var req *http.Request
	var resp *http.Response
	var err error
	host, port, _ := net.SplitHostPort(address)
	uport, _ := strconv.Atoi(port)
	dialData := BuildBody(NewActionCreate(id, host, uint16(uport), conn.RedirectURL))
	ch, chWR := netrans.NewChannelWriteCloser(conn.ctx)
	defer chWR.Close()

	baseHeader := conn.Header.Clone()

	if conn.Mode == FullDuplex {
		body := netrans.MultiReadCloser(
			io.NopCloser(bytes.NewReader(dialData)),
			io.NopCloser(netrans.NewChannelReader(ch)),
		)
		req, _ = http.NewRequestWithContext(conn.ctx, conn.Method, conn.Target, body)
		baseHeader.Set(HeaderKey, HeaderValueFull)
		req.Header = baseHeader
		resp, err = conn.rawClient.Do(req)
	} else {
		req, _ = http.NewRequestWithContext(conn.ctx, conn.Method, conn.Target, bytes.NewReader(dialData))
		baseHeader.Set(HeaderKey, HeaderValueHalf)
		req.Header = baseHeader
		resp, err = conn.noTimeoutClient.Do(req)
	}
	if err != nil {
		//logs.Log.Debugf("request error to target, %s", err)
		return err
	}

	if resp.Header.Get("Set-Cookie") != "" && conn.EnableCookieJar {
		//logs.Log.Infof("update cookie with %s", resp.Header.Get("Set-Cookie"))
	}

	// skip offset
	if conn.Offset > 0 {
		//logs.Log.Debugf("skipping offset %d", m.Offset)
		_, err = io.CopyN(io.Discard, resp.Body, int64(conn.Offset))
		if err != nil {
			//logs.Log.Errorf("failed to skip offset, %s", err)
			return err
		}
	}
	fr, err := netrans.ReadFrame(resp.Body)
	if err != nil {
		//logs.Log.Errorf("failed to read response frame, may be the target has load balancing?")
		return err
	}
	//logs.Log.Debugf("recv dial response from server: length: %d", fr.Length)

	serverData, err := Unmarshal(fr.Data)
	if err != nil {
		//logs.Log.Errorf("failed to process frame, %v", err)
		return err
	}
	status := serverData["s"]
	if len(status) != 1 || status[0] != 0x00 {
		return fmt.Errorf("failed to dial, status: %v", status)
	}

	var streamRW io.ReadWriteCloser
	if conn.Mode == FullDuplex {
		streamRW = NewFullChunkedReadWriter(id, chWR, resp.Body)
	} else {
		streamRW = NewHalfChunkedReadWriter(conn.ctx, id, conn.normalClient, conn.Method, conn.Target,
			resp.Body, baseHeader, conn.RedirectURL)
	}

	if !conn.DisableHeartbeat {
		streamRW = NewHeartbeatRW(streamRW.(RawReadWriteCloser), id, conn.RedirectURL)
	}

	conn.ReadWriteCloser = streamRW
	return nil
}

func (conn *suo5Conn) LocalAddr() net.Addr {
	return nil
}

func (conn *suo5Conn) RemoteAddr() net.Addr {
	return nil
}

func (conn *suo5Conn) SetDeadline(t time.Time) error {
	return nil
}

func (conn *suo5Conn) SetReadDeadline(t time.Time) error {
	return nil
}

func (conn *suo5Conn) SetWriteDeadline(t time.Time) error {
	return nil
}
