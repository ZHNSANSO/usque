package cmd

import (
	"context"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/netip"

	"github.com/Diniboy1123/usque/config"
	"github.com/Diniboy1123/usque/internal"
	"github.com/Diniboy1123/usque/internal/tunnel"
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

		// Create tunnel config from flags
		tunnelCfg, err := NewTunnelConfigFromFlags(cmd)
		if err != nil {
			log.Fatalf("Failed to create tunnel config: %v", err)
		}

		// Create the tunnel
		t, err := tunnel.NewTunnel(tunnelCfg)
		if err != nil {
			log.Fatalf("Failed to create tunnel: %v", err)
		}
		defer t.Close()

		// Start the tunnel
		t.Start(context.Background())

		localPorts, err := cmd.Flags().GetStringArray("local-ports")
		if err != nil {
			log.Fatalf("Failed to get local ports: %v", err)
		}

		remotePorts, err := cmd.Flags().GetStringArray("remote-ports")
		if err != nil {
			log.Fatalf("Failed to get remote ports: %v", err)
		}

		var localPortMappings []internal.PortMapping
		var remotePortMappings []internal.PortMapping

		for _, port := range localPorts {
			portMapping, err := internal.ParsePortMapping(port)
			if err != nil {
				log.Fatalf("Failed to parse local port mapping: %v", err)
			}
			localPortMappings = append(localPortMappings, portMapping)
		}

		for _, port := range remotePorts {
			portMapping, err := internal.ParsePortMapping(port)
			if err != nil {
				log.Fatalf("Failed to parse remote port mapping: %v", err)
			}
			remotePortMappings = append(remotePortMappings, portMapping)
		}

		log.Printf("Virtual tunnel created, forwarding ports")

		// Start Local Port Forwarding (-L)
		for _, pm := range localPortMappings {
			go func(pm internal.PortMapping) {
				err := forwardPort(t.Net, pm, false) // false = local forwarding
				if err != nil {
					log.Printf("Error in local forwarding %d: %v\n", pm.LocalPort, err)
				}
			}(pm)
		}

		// Start Remote Port Forwarding (-R)
		for _, pm := range remotePortMappings {
			go func(pm internal.PortMapping) {
				err := forwardPort(t.Net, pm, true) // true = remote forwarding
				if err != nil {
					log.Printf("Error in remote forwarding %d: %v\n", pm.LocalPort, err)
				}
			}(pm)
		}

		// One packet must be sent in order to listen for incoming packets
		// a ping may suffice as well, but we will use a simple GET request
		client := &http.Client{
			Transport: &http.Transport{
				DialContext: t.Net.DialContext,
			},
		}
		resp, err := client.Get("https://cloudflareok.com/test")
		if err != nil {
			log.Printf("Failed to make request to cloudflare.com: %v\n", err)
			return
		}
		defer resp.Body.Close()
		if resp.StatusCode != 204 {
			log.Printf("Failed to make request to cloudflare.com: %s\n", resp.Status)
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

	go func() { io.Copy(remoteConn, localConn) }()
	io.Copy(localConn, remoteConn)
}

func init() {
	// Add tunnel-specific flags
	AddTunnelFlags(portFwCmd)

	// Add command-specific flags
	portFwCmd.Flags().StringArrayP("local-ports", "L", []string{}, "List of port mappings to forward (SSH like e.g. localhost:8080:100.96.0.2:8080)")
	portFwCmd.Flags().StringArrayP("remote-ports", "R", []string{}, "List of port mappings to forward (SSH like e.g. 100.96.0.3:8080:localhost:8080)")

	rootCmd.AddCommand(portFwCmd)
}
