package loadbalance

import (
	"github.com/chainreactors/proxyclient"
	"net"
)

func NewRoundRobin(proxies []proxyclient.Dial) proxyclient.Dial {
	index := 0
	return func(network, address string) (net.Conn, error) {
		if int(index) > len(proxies) {
			index = 0
		}
		dial := proxies[index]
		index += 1
		return dial(network, address)
	}
}
