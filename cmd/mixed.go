package cmd

import (
	"bufio"
	"context"
	"log"
	"net"
	"net/http"
	"time"

	"github.com/Diniboy1123/usque/config"
	"github.com/Diniboy1123/usque/internal"
	"github.com/Diniboy1123/usque/internal/socks5"
	"github.com/spf13/cobra"
)

var mixedCmd = &cobra.Command{
	Use:   "mixed",
	Short: "Expose Warp as a mixed (SOCKS5 + HTTP) proxy",
	Long:  "Dual-stack mixed proxy supporting both SOCKS5 and HTTP protocols on the same port.",
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

		// Resolvers
		resolver := internal.GetProxyResolver(cfg.LocalDNS, tunNet, cfg.DNSAddrs, cfg.DNSTimeout)
		socksResolver := internal.GetSocksResolver(cfg.LocalDNS, tunNet, cfg.DNSAddrs, cfg.DNSTimeout)

		// SOCKS5 Server Setup
		socksServer := &socks5.Server{
			Username: cfg.Username,
			Password: cfg.Password,
			Dialer: func(ctx context.Context, network, addr string) (net.Conn, error) {
				return tunNet.DialContext(ctx, network, addr)
			},
			Resolver: socksResolver,
		}

		// HTTP Server Setup
		var authHeader string
		if cfg.Username != "" && cfg.Password != "" {
			authHeader = "Basic " + internal.LoginToBase64(cfg.Username, cfg.Password)
		}

		httpServer := &http.Server{
			Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if !authenticate(r, authHeader) {
					w.Header().Set("Proxy-Authenticate", `Basic realm="Proxy"`)
					http.Error(w, "Proxy authentication required", http.StatusProxyAuthRequired)
					return
				}

				if r.Method == http.MethodConnect {
					handleHTTPSConnect(w, r, tunNet, resolver)
				} else {
					handleHTTPProxy(w, r, tunNet, resolver)
				}
			}),
			ReadHeaderTimeout: 10 * time.Second,
			IdleTimeout:       60 * time.Second,
		}

		// Listen
		listener, err := net.Listen("tcp", net.JoinHostPort(cfg.BindAddress, cfg.Port))
		if err != nil {
			cmd.Printf("Failed to listen: %v\n", err)
			return
		}
		log.Printf("Mixed proxy listening on %s:%s", cfg.BindAddress, cfg.Port)

		// Virtual Listener for HTTP
		httpListener := internal.NewVirtualListener(listener.Addr())
		go func() {
			if err := httpServer.Serve(httpListener); err != nil && err != http.ErrServerClosed {
				log.Printf("HTTP server error: %v", err)
			}
		}()

		// Accept Loop
		for {
			conn, err := listener.Accept()
			if err != nil {
				log.Printf("Accept error: %v", err)
				continue
			}

			go func(c net.Conn) {
				// Set a deadline for protocol detection to prevent Slowloris attacks
				c.SetReadDeadline(time.Now().Add(5 * time.Second))

				reader := bufio.NewReader(c)
				head, err := reader.Peek(1)
				if err != nil {
					c.Close()
					return
				}

				// Reset deadline before handing off
				c.SetReadDeadline(time.Time{})

				peekedConn := &internal.PeekedConn{
					Conn:   c,
					Reader: reader,
				}

				// SOCKS4 (0x04) or SOCKS5 (0x05)
				if head[0] == 4 || head[0] == 5 {
					if err := socksServer.ServeConn(peekedConn); err != nil {
						// Log debug if needed
					}
				} else {
					// Assume HTTP
					select {
					case httpListener.Ch <- peekedConn:
					case <-httpListener.Closed:
						c.Close()
					}
				}
			}(conn)
		}
	},
}

func init() {
	AddProxyFlags(mixedCmd)
	rootCmd.AddCommand(mixedCmd)
}
