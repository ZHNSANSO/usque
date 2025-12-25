package cmd

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"

	"github.com/Diniboy1123/usque/config"
	"github.com/Diniboy1123/usque/internal"
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

		resolver := internal.GetProxyResolver(cfg.LocalDNS, tunNet, cfg.DNSAddrs, cfg.DNSTimeout)

		var authHeader string
		if cfg.Username != "" && cfg.Password != "" {
			authHeader = "Basic " + internal.LoginToBase64(cfg.Username, cfg.Password)
		}

		server := &http.Server{
			Addr: net.JoinHostPort(cfg.BindAddress, cfg.Port),
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
		}

		log.Printf("HTTP proxy listening on %s:%s\n", cfg.BindAddress, cfg.Port)
		if err := server.ListenAndServe(); err != nil {
			cmd.Printf("Failed to start HTTP proxy: %v\n", err)
		}
	},
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
		internal.CopyBuffer(destConn, clientConn)
	}()
	internal.CopyBuffer(clientConn, destConn)
}

// handleHTTPProxy forwards HTTP proxy requests to the destination and relays responses back to the client using the provided resolver.
func handleHTTPProxy(w http.ResponseWriter, r *http.Request, tunNet *netstack.Net, resolver *net.Resolver) {
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
	internal.CopyBuffer(w, resp.Body)
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
	AddProxyFlags(httpProxyCmd)
	rootCmd.AddCommand(httpProxyCmd)
}
