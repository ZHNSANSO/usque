package cmd

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/netip"
	"strconv"
	"time"

	"github.com/Diniboy1123/usque/api"
	"github.com/Diniboy1123/usque/config"
	"github.com/Diniboy1123/usque/internal"
	"github.com/spf13/cobra"
	"golang.zx2c4.com/wireguard/tun/netstack"
)

var portFwCmd = &cobra.Command{
	Use:   "portfw",
	Short: "Forward ports through a MASQUE tunnel",
	Long: "This tool is useful if you have Cloudflare Zero Trust Gateway enabled and want to forward ports to/from the tunnel." +
		" It creates a virtual TUN device and forward ports through it either from or to the client. It works a bit like SSH port forwarding. TCP only at the moment." +
		"Doesn't require elevated privileges.",
	Run: func(cmd *cobra.Command, args []string) {
		if !config.ConfigLoaded {
			cmd.Println("Config not loaded. Please register first.")
			return
		}

		sni, _ := cmd.Flags().GetString("sni-address")
		// If not provided in flags, fallback to config file
		if sni == internal.ConnectSNI && config.AppConfig.SNI != "" {
			sni = config.AppConfig.SNI
		}

		keepalivePeriod, _ := cmd.Flags().GetDuration("keepalive-period")
		initialPacketSize, _ := cmd.Flags().GetUint16("initial-packet-size")
		connectPort, _ := cmd.Flags().GetInt("connect-port")

		// Use unified endpoint logic
		endpoint := config.AppConfig.Endpoint
		if cmd.Flags().Changed("connect-port") {
			host, _, err := net.SplitHostPort(endpoint)
			if err != nil {
				host = endpoint
			}
			endpoint = net.JoinHostPort(host, strconv.Itoa(connectPort))
		}

		tunnelIPv4, _ := cmd.Flags().GetBool("no-tunnel-ipv4")
		tunnelIPv6, _ := cmd.Flags().GetBool("no-tunnel-ipv6")

		var localAddresses []netip.Addr
		if !tunnelIPv4 {
			v4, err := netip.ParseAddr(config.AppConfig.IPv4)
			if err == nil {
				localAddresses = append(localAddresses, v4)
			}
		}
		if !tunnelIPv6 {
			v6, err := netip.ParseAddr(config.AppConfig.IPv6)
			if err == nil {
				localAddresses = append(localAddresses, v6)
			}
		}

		dnsServers, _ := cmd.Flags().GetStringArray("dns")
		var dnsAddrs []netip.Addr
		for _, dns := range dnsServers {
			addr, err := netip.ParseAddr(dns)
			if err == nil {
				dnsAddrs = append(dnsAddrs, addr)
			}
		}

		mtu, _ := cmd.Flags().GetInt("mtu")
		if mtu != 1280 {
			log.Println("Warning: MTU is not the default 1280. This is not supported. Packet loss and other issues may occur.")
		}

		localPorts, _ := cmd.Flags().GetStringArray("local-ports")
		remotePorts, _ := cmd.Flags().GetStringArray("remote-ports")

		var localPortMappings []internal.PortMapping
		var remotePortMappings []internal.PortMapping

		for _, port := range localPorts {
			portMapping, err := internal.ParsePortMapping(port)
			if err != nil {
				cmd.Printf("Failed to parse local port mapping: %v\n", err)
				return
			}
			localPortMappings = append(localPortMappings, portMapping)
		}

		for _, port := range remotePorts {
			portMapping, err := internal.ParsePortMapping(port)
			if err != nil {
				cmd.Printf("Failed to parse remote port mapping: %v\n", err)
				return
			}
			remotePortMappings = append(remotePortMappings, portMapping)
		}

		reconnectDelay, _ := cmd.Flags().GetDuration("reconnect-delay")

		tunDev, tunNet, err := netstack.CreateNetTUN(localAddresses, dnsAddrs, mtu)
		if err != nil {
			cmd.Printf("Failed to create virtual TUN device: %v\n", err)
			return
		}
		defer tunDev.Close()

		// Update AppConfig with current runtime settings for MaintainTunnel
		runtimeConfig := config.AppConfig
		runtimeConfig.Endpoint = endpoint
		runtimeConfig.SNI = sni

		go api.MaintainTunnel(context.Background(), &runtimeConfig, keepalivePeriod, initialPacketSize, api.NewNetstackAdapter(tunDev), mtu, reconnectDelay)

		log.Printf("Virtual tunnel created, forwarding ports")

		// Start Local Port Forwarding (-L)
		for _, pm := range localPortMappings {
			go func(pm internal.PortMapping) {
				err := forwardPort(tunNet, pm, false) // false = local forwarding
				if err != nil {
					cmd.Printf("Error in local forwarding %d: %v\n", pm.LocalPort, err)
				}
			}(pm)
		}

		// Start Remote Port Forwarding (-R)
		for _, pm := range remotePortMappings {
			go func(pm internal.PortMapping) {
				err := forwardPort(tunNet, pm, true) // true = remote forwarding
				if err != nil {
					cmd.Printf("Error in remote forwarding %d: %v\n", pm.LocalPort, err)
				}
			}(pm)
		}

		// One packet must be sent in order to listen for incoming packets
		// a ping may suffice as well, but we will use a simple GET request
		client := &http.Client{
			Transport: &http.Transport{
				DialContext: tunNet.DialContext,
			},
		}
		resp, err := client.Get("https://cloudflareok.com/test")
		if err != nil {
			cmd.Printf("Failed to make request to cloudflare.com: %v\n", err)
			return
		}
		defer resp.Body.Close()
		if resp.StatusCode != 204 {
			cmd.Printf("Failed to make request to cloudflare.com: %s\n", resp.Status)
			return
		}
		log.Println("Successfully connected to Cloudflare")

		select {}
	},
}

// forwardPort sets up a local or remote port forwarding using either the MASQUE tunnel or the local network.
func forwardPort(netstackNet *netstack.Net, pm internal.PortMapping, isRemote bool) error {
	localAddrPort, err := netip.ParseAddrPort(fmt.Sprintf("%s:%d", pm.BindAddress, pm.LocalPort))
	if err != nil {
		return fmt.Errorf("invalid local address: %w", err)
	}

	if isRemote {
		// Remote forwarding: Listen inside the MASQUE tunnel
		listener, err := netstackNet.ListenTCPAddrPort(localAddrPort)
		if err != nil {
			return fmt.Errorf("failed to listen on %s: %w", localAddrPort, err)
		}
		defer listener.Close()

		log.Printf("Remote forwarding: Listening on MASQUE network %s, forwarding to local %s:%d", localAddrPort, pm.RemoteIP, pm.RemotePort)

		for {
			conn, err := listener.Accept()
			if err != nil {
				log.Printf("Accept error on %s: %v", localAddrPort, err)
				continue
			}

			go handleConnection(conn, pm, isRemote, netstackNet)
		}
	} else {
		// Local forwarding: Listen on local machine
		listener, err := net.Listen("tcp", fmt.Sprintf("%s:%d", pm.BindAddress, pm.LocalPort))
		if err != nil {
			return fmt.Errorf("failed to listen on %s:%d: %w", pm.BindAddress, pm.LocalPort, err)
		}
		defer listener.Close()

		log.Printf("Local forwarding: Listening on %s:%d, forwarding to remote %s:%d", pm.BindAddress, pm.LocalPort, pm.RemoteIP, pm.RemotePort)

		for {
			conn, err := listener.Accept()
			if err != nil {
				log.Printf("Accept error on %s:%d: %v", pm.BindAddress, pm.LocalPort, err)
				continue
			}

			go handleConnection(conn, pm, isRemote, netstackNet)
		}
	}
}

// handleConnection manages an individual forwarded connection between the local and remote endpoints.
func handleConnection(localConn net.Conn, pm internal.PortMapping, isRemote bool, tunNet *netstack.Net) {
	defer localConn.Close()

	remoteAddrPort, err := netip.ParseAddrPort(fmt.Sprintf("%s:%d", pm.RemoteIP, pm.RemotePort))
	if err != nil {
		log.Printf("Invalid remote address: %v", err)
		return
	}

	var remoteConn net.Conn
	if isRemote {
		// Remote forwarding: Connect to the external remote host
		remoteConn, err = net.Dial("tcp", remoteAddrPort.String())
	} else {
		// Local forwarding: Connect inside the tunnel network
		remoteConn, err = tunNet.DialContext(context.Background(), "tcp", remoteAddrPort.String())
	}

	if err != nil {
		log.Printf("Failed to connect to remote %s: %v", remoteAddrPort, err)
		return
	}
	defer remoteConn.Close()

	go func() { internal.CopyBuffer(remoteConn, localConn) }()
	internal.CopyBuffer(localConn, remoteConn)
}

func init() {
	portFwCmd.Flags().StringArrayP("local-ports", "L", []string{}, "List of port mappings to forward (SSH like e.g. localhost:8080:100.96.0.2:8080)")
	portFwCmd.Flags().StringArrayP("remote-ports", "R", []string{}, "List of port mappings to forward (SSH like e.g. 100.96.0.3:8080:localhost:8080)")
	portFwCmd.Flags().IntP("connect-port", "P", 443, "Used port for MASQUE connection")
	portFwCmd.Flags().StringArrayP("dns", "d", []string{"9.9.9.9", "149.112.112.112", "2620:fe::fe", "2620:fe::9"}, "DNS servers to use inside the MASQUE tunnel")
	portFwCmd.Flags().BoolP("ipv6", "6", false, "Use IPv6 for MASQUE connection")
	portFwCmd.Flags().BoolP("no-tunnel-ipv4", "F", false, "Disable IPv4 inside the MASQUE tunnel")
	portFwCmd.Flags().BoolP("no-tunnel-ipv6", "S", false, "Disable IPv6 inside the MASQUE tunnel")
	portFwCmd.Flags().StringP("sni-address", "s", internal.ConnectSNI, "SNI address to use for MASQUE connection")
	portFwCmd.Flags().DurationP("keepalive-period", "k", 30*time.Second, "Keepalive period for MASQUE connection")
	portFwCmd.Flags().IntP("mtu", "m", 1280, "MTU for MASQUE connection")
	portFwCmd.Flags().Uint16P("initial-packet-size", "i", 1242, "Initial packet size for MASQUE connection")
	portFwCmd.Flags().DurationP("reconnect-delay", "r", 1*time.Second, "Delay between reconnect attempts")
	rootCmd.AddCommand(portFwCmd)
}
