package main

import (
	"fmt"
	"math/rand/v2"
	"net"
	"net/http"
	"net/netip"
	"os"
	"slices"
	"time"

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

			var prefixes = []netip.Prefix{
				netip.MustParsePrefix("10.1.0.0/16"),
				netip.MustParsePrefix("10.2.0.0/16"),
				netip.MustParsePrefix("10.3.0.0/16"),
			}
			mux.HandleFunc("/reset", func(w http.ResponseWriter, r *http.Request) {
				nlRoute := netlink.Route{
					LinkIndex: link.Attrs().Index,
					Protocol:  149,
					Priority:  15,
				}
				for _, prefix := range prefixes {
					nlRoute.Dst = netipx.PrefixIPNet(prefix)
					netlink.RouteDel(&nlRoute)
				}
				w.WriteHeader(http.StatusOK)
			})
			mux.HandleFunc("/bug", func(w http.ResponseWriter, r *http.Request) {
				route1 := prefixes[0]
				route2 := prefixes[1]

				nlRoute := netlink.Route{
					LinkIndex: link.Attrs().Index,
					Dst:       netipx.PrefixIPNet(route1),
					Protocol:  149,
					Priority:  15,
				}
				// Add first route, should trigger BGP UPDATE adv
				if err := netlink.RouteAdd(&nlRoute); err != nil {
					fmt.Fprintf(os.Stderr, "Failed to add route %s: %v\n", route1, err)
					http.Error(w, err.Error(), http.StatusInternalServerError)
					return
				}
				time.Sleep(1 * time.Second)
				// Add second route, but no BGP UPDATE message yet, because MRAI timer.
				nlRoute.Dst = netipx.PrefixIPNet(route2)
				if err := netlink.RouteAdd(&nlRoute); err != nil {
					fmt.Fprintf(os.Stderr, "Failed to add route %s: %v\n", route2, err)
					http.Error(w, err.Error(), http.StatusInternalServerError)
					return
				}
				time.Sleep(250 * time.Millisecond)
				// Emulate route1 flap (del/add), where route1 will be eventually withdraw and the add
				// is suppress as a duplicate. This should happen within the MRAI timer.
				nlRoute.Dst = netipx.PrefixIPNet(route1)
				if err := netlink.RouteDel(&nlRoute); err != nil {
					fmt.Fprintf(os.Stderr, "Failed to del route %s: %v\n", route1, err)
					http.Error(w, err.Error(), http.StatusInternalServerError)
					return
				}
				time.Sleep(250 * time.Millisecond)
				if err := netlink.RouteAdd(&nlRoute); err != nil {
					fmt.Fprintf(os.Stderr, "Failed to re-add route %s: %v\n", route1, err)
					http.Error(w, err.Error(), http.StatusInternalServerError)
					return
				}
				w.WriteHeader(http.StatusOK)
			})
			mux.HandleFunc("/bug2", func(w http.ResponseWriter, r *http.Request) {
				route1 := prefixes[0]
				route2 := prefixes[1]

				nlRoute := netlink.Route{
					LinkIndex: link.Attrs().Index,
					Dst:       netipx.PrefixIPNet(route1),
					Protocol:  149,
					Priority:  15,
				}
				// Add first route, should trigger BGP UPDATE adv
				if err := netlink.RouteAdd(&nlRoute); err != nil {
					fmt.Fprintf(os.Stderr, "Failed to add route %s: %v\n", route1, err)
					http.Error(w, err.Error(), http.StatusInternalServerError)
					return
				}
				time.Sleep(1 * time.Second)
				// Del route1, the withdraw only BGP UPDATE is sent regardless of the MRAI timer.
				nlRoute.Dst = netipx.PrefixIPNet(route1)
				if err := netlink.RouteDel(&nlRoute); err != nil {
					fmt.Fprintf(os.Stderr, "Failed to del route %s: %v\n", route1, err)
					http.Error(w, err.Error(), http.StatusInternalServerError)
					return
				}
				nlRoute.Dst = netipx.PrefixIPNet(route2)
				time.Sleep(250 * time.Millisecond)
				if err := netlink.RouteAdd(&nlRoute); err != nil {
					fmt.Fprintf(os.Stderr, "Failed to add route %s: %v\n", route2, err)
					http.Error(w, err.Error(), http.StatusInternalServerError)
					return
				}
				w.WriteHeader(http.StatusOK)
			})
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
				w.WriteHeader(http.StatusOK)
			})
			mux.HandleFunc("/sequence", func(w http.ResponseWriter, r *http.Request) {
				if err := runSequence(link, routes); err != nil {
					fmt.Fprintf(os.Stderr, "Failed to run sequence: %v\n", err)
					http.Error(w, err.Error(), http.StatusInternalServerError)
					return
				}
				w.WriteHeader(http.StatusOK)
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
	nets := make([]netip.Prefix, 0, 1000)
	for i := 0; i < 1000; i++ {
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

func delRoute(link netlink.Link, route netip.Prefix) error {
	return netlink.RouteDel(&netlink.Route{
		LinkIndex: link.Attrs().Index,
		Dst:       netipx.PrefixIPNet(route),
		Protocol:  149,
		Priority:  15,
	})
}

func addRoute(link netlink.Link, route netip.Prefix) error {
	return netlink.RouteAdd(&netlink.Route{
		LinkIndex: link.Attrs().Index,
		Dst:       netipx.PrefixIPNet(route),
		Protocol:  149,
		Priority:  15,
	})
}

func selectRandom(routes []netip.Prefix, amount int) []netip.Prefix {
	if amount > len(routes) {
		amount = len(routes)
	}
	selected := make([]netip.Prefix, 0, amount)
	for i := 0; i < amount; i++ {
		idx := rand.Int32N(int32(amount))
		if slices.Contains(selected, routes[idx]) {
			continue
		}
		selected = append(selected, routes[idx])
	}
	return selected
}

func runSequence(link netlink.Link, routes []netip.Prefix) error {
	for i := 0; i < 10; i++ {
		fmt.Printf("Running sequence %d\n", i+1)
		selected := selectRandom(routes, 10)
		for _, r := range selected {
			if err := delRoute(link, r); err != nil {
				return fmt.Errorf("failed to del route %s: %w", r, err)
			}
			fmt.Printf("Deleted route: %s\n", r)
		}
		time.Sleep(time.Second / 4)
		for _, r := range selected {
			if err := addRoute(link, r); err != nil {
				return fmt.Errorf("failed to re-add route %s: %w", r, err)
			}
			fmt.Printf("Re-Added route: %s\n", r)
		}
		time.Sleep(time.Second)
	}
	return nil
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
