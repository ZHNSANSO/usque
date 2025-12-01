package cmd

import (
	"context"
	"log"
	"net"
	"os"
	"time"

	"github.com/Diniboy1123/usque/config"
	"github.com/Diniboy1123/usque/internal"
	"github.com/Diniboy1123/usque/internal/tunnel"
	"github.com/spf13/cobra"
	"github.com/things-go/go-socks5"
)

var socksCmd = &cobra.Command{
	Use:   "socks",
	Short: "Expose Warp as a SOCKS5 proxy",
	Long:  "Dual-stack SOCKS5 proxy with optional authentication. Doesn't require elevated privileges.",
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

		bindAddress, err := cmd.Flags().GetString("bind")
		if err != nil {
			log.Fatalf("Failed to get bind address: %v", err)
		}

		port, err := cmd.Flags().GetString("port")
		if err != nil {
			log.Fatalf("Failed to get port: %v", err)
		}

		var username string
		var password string
		if u, err := cmd.Flags().GetString("username"); err == nil && u != "" {
			username = u
		}
		if p, err := cmd.Flags().GetString("password"); err == nil && p != "" {
			password = p
		}

		dnsTimeout, err := cmd.Flags().GetDuration("dns-timeout")
		if err != nil {
			log.Fatalf("Failed to get DNS timeout: %v", err)
		}

		localDNS, err := cmd.Flags().GetBool("local-dns")
		if err != nil {
			log.Fatalf("Failed to get local-dns flag: %v", err)
		}

		var resolver socks5.NameResolver
		if localDNS {
			resolver = internal.TunnelDNSResolver{TunNet: nil, DNSAddrs: tunnelCfg.DNS, Timeout: dnsTimeout}
		} else {
			resolver = internal.TunnelDNSResolver{TunNet: t.Net, DNSAddrs: tunnelCfg.DNS, Timeout: dnsTimeout}
		}

		var server *socks5.Server
		if username == "" || password == "" {
			server = socks5.NewServer(
				socks5.WithLogger(socks5.NewLogger(log.New(os.Stdout, "socks5: ", log.LstdFlags))),
				socks5.WithDial(func(ctx context.Context, network, addr string) (net.Conn, error) {
					return t.Net.DialContext(ctx, network, addr)
				}),
				socks5.WithResolver(resolver),
			)
		} else {
			server = socks5.NewServer(
				socks5.WithLogger(socks5.NewLogger(log.New(os.Stdout, "socks5: ", log.LstdFlags))),
				socks5.WithDial(func(ctx context.Context, network, addr string) (net.Conn, error) {
					return t.Net.DialContext(ctx, network, addr)
				}),
				socks5.WithResolver(resolver),
				socks5.WithAuthMethods(
					[]socks5.Authenticator{
						socks5.UserPassAuthenticator{
							Credentials: socks5.StaticCredentials{
								username: password,
							},
						},
					},
				),
			)
		}

		log.Printf("SOCKS proxy listening on %s:%s", bindAddress, port)
		if err := server.ListenAndServe("tcp", net.JoinHostPort(bindAddress, port)); err != nil {
			log.Fatalf("Failed to start SOCKS proxy: %v", err)
		}
	},
}

func init() {
	// Add tunnel-specific flags
	AddTunnelFlags(socksCmd)

	// Add command-specific flags
	socksCmd.Flags().StringP("bind", "b", "0.0.0.0", "Address to bind the SOCKS proxy to")
	socksCmd.Flags().StringP("port", "p", "1080", "Port to listen on for SOCKS proxy")
	socksCmd.Flags().StringP("username", "u", "", "Username for proxy authentication (specify both username and password to enable)")
	socksCmd.Flags().StringP("password", "w", "", "Password for proxy authentication (specify both username and password to enable)")
	socksCmd.Flags().DurationP("dns-timeout", "t", 2*time.Second, "Timeout for DNS queries")
	socksCmd.Flags().BoolP("local-dns", "l", false, "Don't use the tunnel for DNS queries")

	rootCmd.AddCommand(socksCmd)
}
