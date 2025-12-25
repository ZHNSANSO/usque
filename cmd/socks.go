package cmd

import (
	"context"
	"log"
	"net"

	"github.com/Diniboy1123/usque/config"
	"github.com/Diniboy1123/usque/internal"
	"github.com/Diniboy1123/usque/internal/socks5"
	"github.com/spf13/cobra"
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

		cfg, err := LoadProxyConfig(cmd)
		if err != nil {
			cmd.Printf("Failed to load config: %v\n", err)
			return
		}

		tunNet, err := StartTunnel(context.Background(), cfg)
		if err != nil {
			cmd.Printf("Failed to start tunnel: %v\n", err)
			return
		}

		resolver := internal.GetSocksResolver(cfg.LocalDNS, tunNet, cfg.DNSAddrs, cfg.DNSTimeout)

		server := &socks5.Server{
			Username: cfg.Username,
			Password: cfg.Password,
			Dialer: func(ctx context.Context, network, addr string) (net.Conn, error) {
				return tunNet.DialContext(ctx, network, addr)
			},
			Resolver: resolver,
		}

		log.Printf("SOCKS proxy listening on %s:%s", cfg.BindAddress, cfg.Port)
		listener, err := net.Listen("tcp", net.JoinHostPort(cfg.BindAddress, cfg.Port))
		if err != nil {
			cmd.Printf("Failed to listen: %v\n", err)
			return
		}

		for {
			conn, err := listener.Accept()
			if err != nil {
				log.Printf("Accept error: %v", err)
				continue
			}
			go func(c net.Conn) {
				if err := server.ServeConn(c); err != nil {
					// SOCKS errors are common
				}
			}(conn)
		}
	},
}

func init() {
	AddProxyFlags(socksCmd)
	rootCmd.AddCommand(socksCmd)
}
