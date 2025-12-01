package cmd

import (
	"crypto/ecdsa"
	"net"
	"net/netip"
	"time"

	"github.com/Diniboy1123/usque/config"
	"github.com/Diniboy1123/usque/internal"
	"github.com/Diniboy1123/usque/internal/tunnel"
	"github.com/spf13/cobra"
)

// AddTunnelFlags adds all the common tunnel-related flags to a cobra command.
func AddTunnelFlags(cmd *cobra.Command) {
	cmd.Flags().IntP("connect-port", "P", 443, "Used port for MASQUE connection")
	cmd.Flags().BoolP("ipv6", "6", false, "Use IPv6 for MASQUE connection")
	cmd.Flags().BoolP("no-tunnel-ipv4", "F", false, "Disable IPv4 inside the MASQUE tunnel")
	cmd.Flags().BoolP("no-tunnel-ipv6", "S", false, "Disable IPv6 inside the MASQUE tunnel")
	cmd.Flags().StringP("sni-address", "s", internal.ConnectSNI, "SNI address to use for MASQUE connection")
	cmd.Flags().DurationP("keepalive-period", "k", 30*time.Second, "Keepalive period for MASQUE connection")
	cmd.Flags().IntP("mtu", "m", 1280, "MTU for MASQUE connection")
	cmd.Flags().Uint16P("initial-packet-size", "i", 1242, "Initial packet size for MASQUE connection")
	cmd.Flags().DurationP("reconnect-delay", "r", 1*time.Second, "Delay between reconnect attempts")
	cmd.Flags().StringArrayP("dns", "d", []string{"9.9.9.9", "149.112.112.112", "2620:fe::fe", "2620:fe::9"}, "DNS servers to use")
}

// NewTunnelConfigFromFlags creates a TunnelConfig from the command's flags.
func NewTunnelConfigFromFlags(cmd *cobra.Command) (*tunnel.TunnelConfig, error) {
	sni, err := cmd.Flags().GetString("sni-address")
	if err != nil {
		return nil, err
	}

	privKey, err := config.AppConfig.GetEcPrivateKey()
	if err != nil {
		return nil, err
	}

	peerPubKey, err := config.AppConfig.GetEcEndpointPublicKey()
	if err != nil {
		return nil, err
	}

	keepalivePeriod, err := cmd.Flags().GetDuration("keepalive-period")
	if err != nil {
		return nil, err
	}

	initialPacketSize, err := cmd.Flags().GetUint16("initial-packet-size")
	if err != nil {
		return nil, err
	}

	connectPort, err := cmd.Flags().GetInt("connect-port")
	if err != nil {
		return nil, err
	}

	var endpoint *net.UDPAddr
	if ipv6, err := cmd.Flags().GetBool("ipv6"); err == nil && !ipv6 {
		endpoint = &net.UDPAddr{
			IP:   net.ParseIP(config.AppConfig.EndpointV4),
			Port: connectPort,
		}
	} else {
		endpoint = &net.UDPAddr{
			IP:   net.ParseIP(config.AppConfig.EndpointV6),
			Port: connectPort,
		}
	}

	tunnelIPv4, err := cmd.Flags().GetBool("no-tunnel-ipv4")
	if err != nil {
		return nil, err
	}

	tunnelIPv6, err := cmd.Flags().GetBool("no-tunnel-ipv6")
	if err != nil {
		return nil, err
	}

	var localAddresses []netip.Addr
	if !tunnelIPv4 {
		v4, err := netip.ParseAddr(config.AppConfig.IPv4)
		if err != nil {
			return nil, err
		}
		localAddresses = append(localAddresses, v4)
	}
	if !tunnelIPv6 {
		v6, err := netip.ParseAddr(config.AppConfig.IPv6)
		if err != nil {
			return nil, err
		}
		localAddresses = append(localAddresses, v6)
	}

	dnsServers, err := cmd.Flags().GetStringArray("dns")
	if err != nil {
		return nil, err
	}

	var dnsAddrs []netip.Addr
	for _, dns := range dnsServers {
		addr, err := netip.ParseAddr(dns)
		if err != nil {
			return nil, err
		}
		dnsAddrs = append(dnsAddrs, addr)
	}

	mtu, err := cmd.Flags().GetInt("mtu")
	if err != nil {
		return nil, err
	}

	reconnectDelay, err := cmd.Flags().GetDuration("reconnect-delay")
	if err != nil {
		return nil, err
	}

	return &tunnel.TunnelConfig{
		SNI:               sni,
		Endpoint:          endpoint,
		PrivateKey:        privKey,
		PeerPublicKey:     peerPubKey.(*ecdsa.PublicKey),
		Keepalive:         keepalivePeriod,
		InitialPacketSize: initialPacketSize,
		MTU:               mtu,
		ReconnectDelay:    reconnectDelay,
		LocalAddresses:    localAddresses,
		DNS:               dnsAddrs,
	}, nil
}
