package suo5

import (
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"github.com/chainreactors/proxyclient"
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

func init() {
	proxyclient.RegisterScheme("SUO5", NewSuo5Client)
	proxyclient.RegisterScheme("SUO5S", NewSuo5Client)
}

// Suo5Client 实现了Client接口，用于与目标通信而非直接暴露 SOCKS5 服务
type Suo5Client struct {
	proxy *url.URL
	conf  *Suo5Conf
}

// Suo5Conf 包含了多个类型的 HTTP 客户端，以及嵌入的 Suo5 配置
type Suo5Conf struct {
	normalClient    *http.Client
	noTimeoutClient *http.Client
	rawClient       *rawhttp.Client
	*Suo5Config
	upstream proxyclient.Dial
}

// NewSuo5Client 创建一个新的 Suo5Client
func NewSuo5Client(proxy *url.URL, upstreamDial proxyclient.Dial) (dial proxyclient.Dial, err error) {
	conf, err := NewConfFromURL(proxy)
	if err != nil {
		return nil, err
	}
	conf.upstream = upstreamDial
	c := &Suo5Client{
		proxy: proxy,
		conf:  conf,
	}

	return func(network, address string) (net.Conn, error) {
		return c.Dial(network, address)
	}, nil
}

// NewConfFromURL 从URL中解析用户名密码生成配置
func NewConfFromURL(proxyURL *url.URL) (*Suo5Conf, error) {
	scheme := "http"
	switch proxyURL.Scheme {
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
		//logs.Log.Infof("using upstream proxy %v", proxy)
		tr.Proxy = http.ProxyURL(u)
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
		upstream:        net.Dial,
		normalClient:    normalClient,
		noTimeoutClient: noTimeoutClient,
		rawClient:       rawClient,
	}

	return suo5Conf, nil
}

// Dial 实现了Client接口
func (c *Suo5Client) Dial(network, address string) (net.Conn, error) {
	// 创建一个新的 suo5Conn 连接
	//conn, err := c.conf.upstream(network, address)
	//if err != nil {
	//	return nil, err
	//}
	suo5Conn := &suo5Conn{
		Suo5Conf: c.conf,
		ctx:      context.Background(),
	}

	// 发送连接请求
	if err := suo5Conn.connect(address); err != nil {
		return nil, err
	}

	return WrapConn(suo5Conn.stream), nil
}

// suo5Conn 实现了net.Conn接口
type suo5Conn struct {
	stream io.ReadWriteCloser
	ctx    context.Context
	closed bool
	*Suo5Conf
}

func (m *suo5Conn) connect(address string) error {
	id := RandString(8)
	var req *http.Request
	var resp *http.Response
	var err error
	host, port, _ := net.SplitHostPort(address)
	uport, _ := strconv.Atoi(port)
	dialData := BuildBody(NewActionCreate(id, host, uint16(uport), m.RedirectURL))
	ch, chWR := netrans.NewChannelWriteCloser(m.ctx)
	defer chWR.Close()

	baseHeader := m.Header.Clone()

	if m.Mode == FullDuplex {
		body := netrans.MultiReadCloser(
			io.NopCloser(bytes.NewReader(dialData)),
			io.NopCloser(netrans.NewChannelReader(ch)),
		)
		req, _ = http.NewRequestWithContext(m.ctx, m.Method, m.Target, body)
		baseHeader.Set(HeaderKey, HeaderValueFull)
		req.Header = baseHeader
		resp, err = m.rawClient.Do(req)
	} else {
		req, _ = http.NewRequestWithContext(m.ctx, m.Method, m.Target, bytes.NewReader(dialData))
		baseHeader.Set(HeaderKey, HeaderValueHalf)
		req.Header = baseHeader
		resp, err = m.noTimeoutClient.Do(req)
	}
	if err != nil {
		//logs.Log.Debugf("request error to target, %s", err)
		return err
	}

	if resp.Header.Get("Set-Cookie") != "" && m.EnableCookieJar {
		//logs.Log.Infof("update cookie with %s", resp.Header.Get("Set-Cookie"))
	}

	defer resp.Body.Close()
	// skip offset
	if m.Offset > 0 {
		//logs.Log.Debugf("skipping offset %d", m.Offset)
		_, err = io.CopyN(io.Discard, resp.Body, int64(m.Offset))
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
	if m.Mode == FullDuplex {
		streamRW = NewFullChunkedReadWriter(id, chWR, resp.Body)
	} else {
		streamRW = NewHalfChunkedReadWriter(m.ctx, id, m.normalClient, m.Method, m.Target,
			resp.Body, baseHeader, m.RedirectURL)
	}

	if !m.DisableHeartbeat {
		streamRW = NewHeartbeatRW(streamRW.(RawReadWriteCloser), id, m.RedirectURL)
	}

	m.stream = streamRW
	return nil
}

func WrapConn(rwc io.ReadWriteCloser) net.Conn {
	return &WrappedConn{
		rwc: rwc,
	}
}

type WrappedConn struct {
	rwc io.ReadWriteCloser
}

func (conn *WrappedConn) LocalAddr() net.Addr {
	return nil
}

func (conn *WrappedConn) RemoteAddr() net.Addr {
	return nil
}

func (conn *WrappedConn) SetDeadline(t time.Time) error {
	return nil
}

func (conn *WrappedConn) SetReadDeadline(t time.Time) error {
	return nil
}

func (conn *WrappedConn) SetWriteDeadline(t time.Time) error {
	return nil
}

func (conn *WrappedConn) Read(p []byte) (n int, err error) {
	return conn.rwc.Read(p)
}

func (conn *WrappedConn) Write(p []byte) (n int, err error) {
	return conn.rwc.Write(p)
}

func (conn *WrappedConn) Close() error {
	return conn.rwc.Close()
}
