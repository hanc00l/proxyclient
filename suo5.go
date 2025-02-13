//go:build suo5
// +build suo5

package proxyclient

import (
	"context"
	"github.com/chainreactors/proxyclient/suo5"
	"net"
	"net/url"
)

func init() {
	RegisterScheme("SUO5", NewSuo5Client)
	RegisterScheme("SUO5S", NewSuo5Client)
}

// NewSuo5Client 创建一个新的 Suo5Client
func NewSuo5Client(proxy *url.URL, upstreamDial Dial) (dial Dial, err error) {
	conf, err := suo5.NewConfFromURL(proxy)
	if err != nil {
		return nil, err
	}
	if upstreamDial != nil {
		conf.Upstream = upstreamDial
	}
	c := &suo5.Suo5Client{
		Proxy: proxy,
		Conf:  conf,
	}

	return func(ctx context.Context, network, address string) (net.Conn, error) {
		return c.Dial(network, address)
	}, nil
}
