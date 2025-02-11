package main

import (
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"os"

	"github.com/spf13/cobra"
	"github.com/vishvananda/netlink"
	"go4.org/netipx"
)

var baseRange = netip.MustParsePrefix("10.42.0.0/16")

func main() {
	var ifaceName string

	rootCmd := &cobra.Command{
		Use:   "frr_test",
		Short: "A CLI to insert prefixes into netlink",
		RunE: func(cmd *cobra.Command, args []string) error {
			link, err := netlink.LinkByName(ifaceName)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Failed to get interface: %v\n", err)
				os.Exit(1)
			}

			listener, err := net.ListenTCP(
				"tcp",
				net.TCPAddrFromAddrPort(netip.MustParseAddrPort("10.42.0.2:80")),
			)
			if err != nil {
				return err
			}
			defer listener.Close()
			mux := http.NewServeMux()
			routes := genInitialRoutes()
			var lastDeleted []netip.Prefix
			mux.HandleFunc("/add", func(w http.ResponseWriter, r *http.Request) {
				toAdd := routes
				if len(lastDeleted) > 0 {
					toAdd = lastDeleted
				}
				if err := addRoutes(link, toAdd); err != nil {
					fmt.Fprintf(os.Stderr, "Failed to add routes: %v\n", err)
					http.Error(w, err.Error(), http.StatusInternalServerError)
					return
				}
				w.WriteHeader(http.StatusOK)
			})
			mux.HandleFunc("/del", func(w http.ResponseWriter, r *http.Request) {
				lastDeleted, err = delRandom(link, routes)
				if err != nil {
					fmt.Fprintf(os.Stderr, "Failed to del routes: %v\n", err)
					http.Error(w, err.Error(), http.StatusInternalServerError)
				}
			})
			return http.Serve(listener, mux)
		},
	}

	rootCmd.Flags().StringVarP(&ifaceName, "interface", "i", "eth0", "Interface name")

	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}

func genInitialRoutes() []netip.Prefix {
	subnetGen := Subnet{base: baseRange}
	nets := make([]netip.Prefix, 0, 2500)
	for i := 0; i < 2500; i++ {
		nets = append(nets, subnetGen.Next())
	}
	return nets
}

func delRandom(link netlink.Link, routes []netip.Prefix) ([]netip.Prefix, error) {
	var deleted []netip.Prefix
	for i := 0; i < 1000; i++ {
		idx := i
		err := netlink.RouteDel(&netlink.Route{
			LinkIndex: link.Attrs().Index,
			Dst:       netipx.PrefixIPNet(routes[idx]),
			Protocol:  149,
			Priority:  15,
		})
		if err != nil {
			return routes, err
		}
		deleted = append(deleted, routes[idx])
	}
	return deleted, nil
}

func addRoutes(link netlink.Link, routes []netip.Prefix) error {
	for _, route := range routes {
		err := netlink.RouteAdd(&netlink.Route{
			LinkIndex: link.Attrs().Index,
			Dst:       netipx.PrefixIPNet(route),
			Protocol:  149,
			Priority:  15,
		})
		if err != nil {
			return err
		}
	}
	return nil
}

type Subnet struct {
	base netip.Prefix
	last netip.Prefix
}

// Next returns the next /27 subnet in the base /16 subnet.
func (s *Subnet) Next() netip.Prefix {
	if !s.last.IsValid() {
		s.last = netip.PrefixFrom(s.base.Addr(), 27)
		return s.last
	}
	s.last = netip.PrefixFrom(netipx.PrefixLastIP(s.last).Next(), 27)
	return s.last
}
