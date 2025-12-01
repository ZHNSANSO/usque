package cmd

import (
	"context"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"time"

	"github.com/Diniboy1123/usque/config"
	"github.com/Diniboy1123/usque/internal"
	"github.com/Diniboy1123/usque/internal/tunnel"
	"github.com/spf13/cobra"
	"golang.zx2c4.com/wireguard/tun/netstack"
)

var httpProxyCmd = &cobra.Command{
	Use:   "http-proxy",
	Short: "Expose Warp as an HTTP proxy with CONNECT support",
	Long:  "Dual-stack HTTP proxy with CONNECT support. Doesn't require elevated privileges.",
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

		var authHeader string
		if username != "" && password != "" {
			authHeader = "Basic " + internal.LoginToBase64(username, password)
		}

		dnsTimeout, err := cmd.Flags().GetDuration("dns-timeout")
		if err != nil {
			log.Fatalf("Failed to get DNS timeout: %v", err)
		}

		localDNS, err := cmd.Flags().GetBool("local-dns")
		if err != nil {
			log.Fatalf("Failed to get local-dns flag: %v", err)
		}

		resolver := internal.GetProxyResolver(localDNS, t.Net, tunnelCfg.DNS, dnsTimeout)

		server := &http.Server{
			Addr: net.JoinHostPort(bindAddress, port),
			Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if authHeader != "" && !authenticate(r, authHeader) {
					w.Header().Set("Proxy-Authenticate", `Basic realm="Proxy"`)
					http.Error(w, "Proxy authentication required", http.StatusProxyAuthRequired)
					return
				}

				if r.Method == http.MethodConnect {
					handleHTTPSConnect(w, r, t.Net, resolver)
				} else {
					handleHTTPProxy(w, r, t.Net, resolver)
				}
			}),
		}

		log.Printf("HTTP proxy listening on %s:%s\n", bindAddress, port)
		if err := server.ListenAndServe(); err != nil {
			log.Fatalf("Failed to start HTTP proxy: %v\n", err)
		}
	},
}

// authenticate verifies the Proxy-Authorization header in an HTTP request.
func authenticate(r *http.Request, expectedAuth string) bool {
	authHeader := r.Header.Get("Proxy-Authorization")
	return authHeader == expectedAuth
}

// handleHTTPSConnect establishes a tunnel to the destination using the provided resolver.
func handleHTTPSConnect(w http.ResponseWriter, r *http.Request, tunNet *netstack.Net, resolver *net.Resolver) {
	ctx := r.Context()

	host, port, err := net.SplitHostPort(r.Host)
	if err != nil {
		http.Error(w, "Invalid host", http.StatusBadRequest)
		return
	}

	var destAddr string
	if resolver != nil {
		ips, err := resolver.LookupIP(ctx, "ip", host)
		if err != nil || len(ips) == 0 {
			http.Error(w, "DNS resolution failed", http.StatusServiceUnavailable)
			return
		}
		destAddr = net.JoinHostPort(ips[0].String(), port)
	} else {
		destAddr = r.Host
	}

	destConn, err := tunNet.DialContext(ctx, "tcp", destAddr)
	if err != nil {
		http.Error(w, "Unable to connect to destination", http.StatusServiceUnavailable)
		return
	}

	hj, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "Hijacking not supported", http.StatusInternalServerError)
		destConn.Close()
		return
	}

	clientConn, _, err := hj.Hijack()
	if err != nil {
		http.Error(w, "Hijacking failed", http.StatusInternalServerError)
		destConn.Close()
		return
	}

	_, err = clientConn.Write([]byte("HTTP/1.1 200 Connection Established\r\n\r\n"))
	if err != nil {
		clientConn.Close()
		destConn.Close()
		return
	}

	go func() {
		defer destConn.Close()
		defer clientConn.Close()
		io.Copy(destConn, clientConn)
	}()
	io.Copy(clientConn, destConn)
}

// handleHTTPProxy forwards HTTP proxy requests to the destination and relays responses back to the client using the provided resolver.
func handleHTTPProxy(w http.ResponseWriter, r *http.Request, tunNet *netstack.Net, resolver *net.Resolver) {
	port := r.URL.Port()
	if port == "" {
		port = "80"
	}

	client := &http.Client{
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				host, port, err := net.SplitHostPort(addr)
				if err != nil {
					return nil, fmt.Errorf("invalid address: %w", err)
				}

				var dialAddr string
				if resolver != nil {
					ips, err := resolver.LookupIP(ctx, "ip", host)
					if err != nil || len(ips) == 0 {
						return nil, fmt.Errorf("DNS resolution failed for %s: %w", host, err)
					}
					dialAddr = net.JoinHostPort(ips[0].String(), port)
				} else {
					dialAddr = addr
				}

				return tunNet.DialContext(ctx, network, dialAddr)
			},
		},
	}

	req, err := http.NewRequestWithContext(r.Context(), r.Method, r.URL.String(), r.Body)
	if err != nil {
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}
	req.Header = r.Header.Clone()

	resp, err := client.Do(req)
	if err != nil {
		http.Error(w, "Failed to reach destination", http.StatusServiceUnavailable)
		return
	}
	defer resp.Body.Close()

	copyHeader(w.Header(), resp.Header)
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)
}

// copyHeader copies HTTP headers from one header map to another.
func copyHeader(dst, src http.Header) {
	for k, vv := range src {
		for _, v := range vv {
			dst.Add(k, v)
		}
	}
}

func init() {
	// Add tunnel-specific flags
	AddTunnelFlags(httpProxyCmd)

	// Add command-specific flags
	httpProxyCmd.Flags().StringP("bind", "b", "0.0.0.0", "Address to bind the HTTP proxy to")
	httpProxyCmd.Flags().StringP("port", "p", "8000", "Port to listen on for HTTP proxy")
	httpProxyCmd.Flags().StringP("username", "u", "", "Username for proxy authentication (specify both username and password to enable)")
	httpProxyCmd.Flags().StringP("password", "w", "", "Password for proxy authentication (specify both username and password to enable)")
	httpProxyCmd.Flags().DurationP("dns-timeout", "t", 2*time.Second, "Timeout for DNS queries")
	httpProxyCmd.Flags().BoolP("local-dns", "l", false, "Don't use the tunnel for DNS queries")

	rootCmd.AddCommand(httpProxyCmd)
}
