package neoreg

import (
	"bytes"
	"errors"
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"net/url"
)

// 常量定义
const (
	cmdData          = 1
	cmdCommand       = 2
	cmdMark          = 3
	cmdStatus        = 4
	cmdError         = 5
	cmdIP            = 6
	cmdPort          = 7
	cmdRedirectURL   = 8
	cmdForceRedirect = 9

	// 密钥生成相关常量
	minKeyLen = 28
	// BLV编码相关常量
	blvHeadLen = 9 // 与Python中的BLVHEAD_LEN对应
)

var (
	saltPrefix  = []byte("11f271c6lm0e9ypkptad1uv6e1ut1fu0pt4xillz1w9bbs2gegbv89z9gca9d6tbk025uvgjfr331o0szln")
	BASE64CHARS = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/"
)

// NeoregClient 实现了Client接口
type NeoregClient struct {
	Proxy *url.URL
	Conf  *NeoregConf
}

// NeoregConf 配置结构
type NeoregConf struct {
	Dial func(network, address string) (net.Conn, error)

	// 目标配置
	Protocol string // http/https

	// 编码配置
	EncodeMap map[byte]byte
	DecodeMap map[byte]byte

	Key  string
	Rand *NeoregRand

	// BLV编码配置
	blvOffset int32 // 对应Python中的BLV_L_OFFSET
}

// NewConfFromURL 从URL中解析用户名密码生成配置
func NewConfFromURL(proxyURL *url.URL) (*NeoregConf, error) {
	if proxyURL.User == nil {
		return nil, errors.New("username and password required in URL")
	}

	scheme := "http"
	switch proxyURL.Scheme {
	case "neoreg":
		scheme = "http"
	case "neoregs":
		scheme = "https"
	default:
		return nil, fmt.Errorf("unsupported scheme: %s", proxyURL.Scheme)
	}

	// 生成密钥
	key := proxyURL.User.Username()

	mt := NewNeoregRand(key)

	encodeMap, decodeMap, blvOffset := generateMaps(mt)

	// 初始化随机生成器

	return &NeoregConf{
		Dial:      net.Dial,
		Protocol:  scheme,
		EncodeMap: encodeMap,
		DecodeMap: decodeMap,
		Key:       key,
		Rand:      mt,
		blvOffset: blvOffset,
	}, nil
}

// Dial 实现了Client接口
func (c *NeoregClient) Dial(network, address string) (net.Conn, error) {
	url := fmt.Sprintf("%s://%s%s", c.Conf.Protocol, c.Proxy.Host, c.Proxy.Path)

	nconn := &neoregConn{
		url:    url,
		mask:   randMask(),
		config: c.Conf,
	}

	// 建立目标连接
	if err := nconn.connect(address); err != nil {
		return nil, err
	}

	return nconn, nil
}

// neoregConn 实现了net.Conn接口
type neoregConn struct {
	net.Conn
	url    string
	mask   []byte
	closed bool
	config *NeoregConf
}

func (c *neoregConn) connect(address string) error {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return err
	}

	info := map[int][]byte{
		cmdCommand: []byte("CONNECT"),
		cmdMark:    c.mask,
		cmdIP:      []byte(host),
		cmdPort:    []byte(port),
	}

	resp, err := c.request(info)
	if err != nil {
		return err
	}

	if !bytes.Equal(resp[cmdStatus], []byte("OK")) {
		return errors.New("connect failed")
	}

	return nil
}

func (c *neoregConn) Read(b []byte) (n int, err error) {
	info := map[int][]byte{
		cmdCommand: []byte("READ"),
		cmdMark:    c.mask,
	}

	for !c.closed {
		resp, err := c.request(info)
		if err != nil {
			continue
		}

		if bytes.Equal(resp[cmdStatus], []byte("OK")) {
			if len(resp[cmdData]) > 0 {
				n = copy(b, resp[cmdData])
				return n, nil
			}
		} else {
			break
		}
	}
	return 0, errors.New("read failed")
}

func (c *neoregConn) Write(b []byte) (n int, err error) {
	info := map[int][]byte{
		cmdCommand: []byte("FORWARD"),
		cmdMark:    c.mask,
		cmdData:    b,
	}

	resp, err := c.request(info)
	if err != nil {
		return 0, err
	}

	if bytes.Equal(resp[cmdStatus], []byte("OK")) {
		return len(b), nil
	}
	return 0, errors.New("write failed")
}

func (c *neoregConn) Close() error {
	c.closed = true
	return c.Conn.Close()
}

// 内部辅助方法
func (c *neoregConn) request(info map[int][]byte) (map[int][]byte, error) {
	data := encodeBody(info, c.config)

	resp, err := http.Post(c.url, "application/octet-stream", bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	return decodeBody(body, c.config), nil
}
