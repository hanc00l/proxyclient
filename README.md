# ProxyClient

the proxy client library

refactor from github.com/RouterScript/ProxyClient

supported SOCKS4, SOCKS4A, SOCKS5, HTTP, HTTPS etc proxy protocols

## Supported Schemes

- [x] Direct
- [x] Reject
- [x] Blackhole
- [x] HTTP (fixed)
- [x] HTTPS (fixed)
- [x] SOCKS5 (fixed)
- [x] ShadowSocks (fixed)
- [x] SSH Agent (fixed)
- [x] suo5
- [x] neoreg

# Documentation

# Example

```go
package main

import (
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"github.com/chainreactors/proxyclient"
)

func main() {
	proxy, _ := url.Parse("http://localhost:8080")
	dial, _ := proxyclient.NewClient(proxy)
	client := &http.Client{
		Transport: &http.Transport{
			DialContext: dial.Context,
		},
	}
	request, err := client.Get("http://www.example.com")
	if err != nil {
		panic(err)
	}
	content, err := ioutil.ReadAll(request.Body)
	if err != nil {
		panic(err)
	}
	fmt.Println(string(content))
}
```

# Reference

- https://github.com/GameXG/ProxyClient
- https://github.com/RouterScript/ProxyClient
