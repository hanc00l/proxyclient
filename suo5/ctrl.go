package suo5

import (
	"fmt"
	"github.com/chainreactors/logs"
	"io"
	"math/rand"
	"net"
	"net/http"
	"net/http/httputil"
	"strings"
	"time"

	"github.com/chainreactors/proxyclient/suo5/netrans"
	utls "github.com/refraction-networking/utls"
	"github.com/zema1/rawhttp"
)

var rander = rand.New(rand.NewSource(time.Now().UnixNano()))

func checkConnectMode(config *Suo5Config) (ConnectionType, int, error) {
	// 这里的 client 需要定义 timeout，不要用外面没有 timeout 的 rawCient
	rawClient := NewRawClient(config.UpstreamProxy, time.Second*5)
	randLen := rander.Intn(1024)
	if randLen <= 32 {
		randLen += 32
	}
	data := RandString(randLen)
	ch := make(chan []byte, 1)
	ch <- []byte(data)
	req, err := http.NewRequest(config.Method, config.Target, netrans.NewChannelReader(ch))
	if err != nil {
		return Undefined, 0, err
	}
	req.Header = config.Header.Clone()
	req.Header.Set(HeaderKey, HeaderValueChecking)

	now := time.Now()
	go func() {
		// timeout
		time.Sleep(time.Second * 3)
		close(ch)
	}()
	resp, err := rawClient.Do(req)
	if err != nil {
		return Undefined, 0, err
	}
	defer resp.Body.Close()

	// 如果独到响应的时间在3s内，说明请求没有被缓存, 那么就可以变成全双工的
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		// 这里不要直接返回，有时虽然 eof 了但是数据是对的，可以使用
		logs.Log.Warnf("got error %s", err)
	}
	duration := time.Since(now).Milliseconds()

	offset := strings.Index(string(body), data[:32])
	if offset == -1 {
		header, _ := httputil.DumpResponse(resp, false)
		logs.Log.Errorf("response are as follows:\n%s", string(header)+string(body))
		return Undefined, 0, fmt.Errorf("got unexpected body, remote server test failed")
	}
	logs.Log.Infof("got data offset, %d", offset)

	if duration < 3000 {
		return FullDuplex, offset, nil
	} else {
		return HalfDuplex, offset, nil
	}
}

// 检查代理是否真正有效, 只要能按有响应即可，尝试连一下 server 的 LocalPort, 这里写 0，在 jsp 里有判断
//func testTunnel(socks5, username, password string, timeout time.Duration) bool {
//	addr, _ := gosocks5.NewAddr("127.0.0.1:0")
//	options := []client.DialOption{client.TimeoutDialOption(timeout)}
//	if username != "" && password != "" {
//		options = append(options, client.SelectorDialOption(client.NewClientSelector(url.UserPassword(username, password))))
//	}
//
//	conn, err := client.Dial(socks5, options...)
//	if err != nil {
//		logs.Log.Error(err)
//		return false
//	}
//	defer conn.Close()
//	if err := gosocks5.NewRequest(gosocks5.CmdConnect, addr).Write(conn); err != nil {
//		logs.Log.Error(err)
//		return false
//	}
//	_ = conn.SetReadDeadline(time.Now().Add(timeout))
//
//	reply, err := gosocks5.ReadReply(conn)
//	if err != nil {
//		logs.Log.Error(err)
//		return false
//	}
//	logs.Log.Debugf("recv socks5 reply: %d", reply.Rep)
//	return reply.Rep == gosocks5.Succeeded || reply.Rep == gosocks5.ConnRefused
//}

//func testAndExit(socks5 string, remote string, timeout time.Duration) error {
//	logs.Log.Infof("checking connection to %s using %s", remote, socks5)
//	u, err := url.Parse(socks5)
//	if err != nil {
//		return err
//	}
//	httpClient := http.Client{
//		Timeout: timeout,
//		Transport: &http.Transport{
//			Proxy: http.ProxyURL(u),
//		},
//	}
//	req, err := http.NewRequest(http.MethodGet, remote, nil)
//	if err != nil {
//		return err
//	}
//	req.Close = true
//	resp, err := httpClient.Do(req)
//	if err != nil {
//		if os.IsTimeout(err) {
//			return err
//		}
//		logs.Log.Infof("test connection got error, but it's ok, %s", err)
//		return nil
//	}
//	defer resp.Body.Close()
//	data, err := httputil.DumpResponse(resp, false)
//	if err != nil {
//		logs.Log.Debugf("test connection got error when read response,  %s, but it's ok", err)
//		return nil
//	}
//	logs.Log.Debugf("test connection got response for %s (without body)\n%s", remote, string(data))
//	return nil
//}

const letterBytes = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"

func RandString(n int) string {
	b := make([]byte, n)
	for i := range b {
		b[i] = letterBytes[rander.Intn(len(letterBytes))]
	}
	return string(b)
}

func NewRawClient(upstream string, timeout time.Duration) *rawhttp.Client {
	return rawhttp.NewClient(&rawhttp.Options{
		Proxy:                  upstream,
		Timeout:                timeout,
		FollowRedirects:        false,
		MaxRedirects:           0,
		AutomaticHostHeader:    true,
		AutomaticContentLength: true,
		ForceReadAllBody:       false,
		TLSHandshake: func(conn net.Conn, addr string, options *rawhttp.Options) (net.Conn, error) {
			colonPos := strings.LastIndex(addr, ":")
			if colonPos == -1 {
				colonPos = len(addr)
			}
			hostname := addr[:colonPos]
			uTlsConn := utls.UClient(conn, &utls.Config{
				InsecureSkipVerify: true,
				ServerName:         hostname,
				MinVersion:         utls.VersionTLS10,
				Renegotiation:      utls.RenegotiateOnceAsClient,
			}, utls.HelloRandomizedNoALPN)
			if err := uTlsConn.Handshake(); err != nil {
				_ = conn.Close()
				return nil, err
			}
			return uTlsConn, nil
		},
	})

}
