package ugc

import (
	"io"

	"messeances/api/internal/syncproxy"
)

type Proxy = syncproxy.Proxy

func ParseProxies(reader io.Reader) ([]Proxy, error) { return syncproxy.Parse(reader) }
