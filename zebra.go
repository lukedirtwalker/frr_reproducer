package main

import (
	"fmt"
	"net"
	"net/netip"
	"strings"
	"sync"
	"time"

	"github.com/osrg/gobgp/v3/pkg/log"
	"github.com/osrg/gobgp/v3/pkg/zebra"
)

const (
	reconnectInterval = time.Second
	zebraAPIVersion   = 6 // we only support FRR 8.2, which uses Zebra version 6
)

// Zebra manages route exchanges with the Zebra daemon in the FRR suite.
type Zebra struct {
	// ZebraServer the path to the zebra server unix socket.
	// It can be made to connect via TCP by using the following format: tcp://<host>:<port>
	ZebraServer string
	// ClientType is the type of the client. All published will be of this type.
	// Note that routes published by client of one type are never redistributed to a client
	// of the same type.
	ClientType zebra.RouteType

	mtx sync.Mutex
	// closeChan is used to shut down the FRR connection.
	closeChan chan struct{}
	// zebraSoftware is zebra software (quagga, frr, cumulus).
	zebraSoftware zebra.Software
}

func (z *Zebra) init() {
	z.mtx.Lock()
	defer z.mtx.Unlock()
	if z.closeChan == nil {
		z.zebraSoftware = zebra.NewSoftware(zebraAPIVersion, "frr10.1")
		z.closeChan = make(chan struct{})
	}
}

func (z *Zebra) Close() {
	z.init()
	close(z.closeChan)
}

func (z *Zebra) connect() *zebra.Client {
	for {
		network := "unix"
		address := z.ZebraServer
		if strings.HasPrefix(z.ZebraServer, "tcp://") {
			network = "tcp"
			address = address[6:]
		}
		client, err := zebra.NewClient(log.NewDefaultLogger(), network, address, z.ClientType,
			zebraAPIVersion, z.zebraSoftware, 0)
		if err == nil {
			fmt.Println("Connected to FRR", "zserv", z.ZebraServer)
			return client

		}
		fmt.Println("Could not connect to FRR", "zserv", z.ZebraServer, "err", err)
		wait := time.After(reconnectInterval)
		select {
		case <-wait:
			// Retry to connect to FRR.
			continue
		case <-z.closeChan:
			// Closed by the user.
			return nil
		}
	}
}

func (z *Zebra) addRoute(client *zebra.Client, prefix netip.Prefix, metric uint32, nextHopAddr net.IP, nextHopIfIndex uint32) error {
	routeBody := &zebra.IPRouteBody{
		Type: z.ClientType,
		Safi: zebra.SafiUnicast,
		Prefix: zebra.Prefix{
			Prefix:    prefix.Addr().AsSlice(),
			PrefixLen: uint8(prefix.Bits()),
		},
		Message: zebra.MessageNexthop | zebra.MessageMetric,
		Metric:  metric,
		Nexthops: []zebra.Nexthop{{
			Gate:    nextHopAddr,
			Ifindex: nextHopIfIndex,
		}},
	}
	return client.SendIPRoute(0, routeBody, false)
}

func (z *Zebra) delRoute(client *zebra.Client, prefix netip.Prefix) error {
	routeBody := &zebra.IPRouteBody{
		Type: z.ClientType,
		Safi: zebra.SafiUnicast,
		Prefix: zebra.Prefix{
			Prefix:    prefix.Addr().AsSlice(),
			PrefixLen: uint8(prefix.Bits()),
		},
	}
	return client.SendIPRoute(0, routeBody, true)
}
