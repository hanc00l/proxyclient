//go:build neoreg
// +build neoreg

package proxyclient

import (
	"github.com/chainreactors/proxyclient/neoreg"
	"net"
	"net/url"
)

func init() {
	RegisterScheme("NEOREG", NewNeoregClient)
	RegisterScheme("NEOREGS", NewNeoregClient)
}

func NewNeoregClient(proxy *url.URL, upstreamDial Dial) (dial Dial, err error) {
	conf, err := neoreg.NewConfFromURL(proxy)
	if err != nil {
		return nil, err
	}
	if upstreamDial != nil {
		conf.Dial = upstreamDial
	}
	client := &neoreg.NeoregClient{
		Proxy: proxy,
		Conf:  conf,
	}

	return func(network, address string) (net.Conn, error) {
		return client.Dial(network, address)
	}, nil
}
