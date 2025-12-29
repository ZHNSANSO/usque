package cmd

import (
	"context"
	"log"
	"net"
	"strconv"
	"time"

	"github.com/Diniboy1123/usque/api"
	"github.com/Diniboy1123/usque/config"
	"github.com/Diniboy1123/usque/internal"
	"github.com/spf13/cobra"
)

type tunDevice struct {
	name     string
	mtu      int
	ipv4     bool
	ipv6     bool
	iproute2 bool
}

var nativeTunCmd = &cobra.Command{
	Use:   "nativetun",
	Short: "Expose Warp as a native TUN device",
	Long:  longDescription,
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
		mtu, _ := cmd.Flags().GetInt("mtu")
		if mtu != 1280 {
			log.Println("Warning: MTU is not the default 1280. This is not supported. Packet loss and other issues may occur.")
		}

		noIproute2, _ := cmd.Flags().GetBool("no-iproute2")
		ifaceName, _ := cmd.Flags().GetString("interface-name")
		reconnectDelay, _ := cmd.Flags().GetDuration("reconnect-delay")

		t := &tunDevice{
			name:     ifaceName,
			mtu:      mtu,
			ipv4:     !tunnelIPv4,
			ipv6:     !tunnelIPv6,
			iproute2: !noIproute2,
		}

		dev, err := t.create()
		if err != nil {
			cmd.Printf("Failed to create TUN device: %v\n", err)
			return
		}

		log.Printf("Created TUN device: %s", t.name)

		// Update AppConfig with current runtime settings for MaintainTunnel
		runtimeConfig := config.AppConfig
		runtimeConfig.Endpoint = endpoint
		runtimeConfig.SNI = sni

		go api.MaintainTunnel(context.Background(), &runtimeConfig, keepalivePeriod, initialPacketSize, dev, mtu, reconnectDelay)

		log.Println("Tunnel established, you may now set up routing and DNS")

		select {}
	},
}

func init() {
	nativeTunCmd.Flags().BoolP("no-tunnel-ipv4", "F", false, "Disable IPv4 inside the MASQUE tunnel")
	nativeTunCmd.Flags().BoolP("no-tunnel-ipv6", "S", false, "Disable IPv6 inside the MASQUE tunnel")
	nativeTunCmd.Flags().StringP("sni-address", "s", internal.ConnectSNI, "SNI address to use for MASQUE connection")
	nativeTunCmd.Flags().DurationP("keepalive-period", "k", 10*time.Second, "Keepalive period for MASQUE connection")
	nativeTunCmd.Flags().IntP("mtu", "m", 1280, "MTU for MASQUE connection")
	nativeTunCmd.Flags().Uint16P("initial-packet-size", "i", 1242, "Initial packet size for MASQUE connection")
	nativeTunCmd.Flags().BoolP("no-iproute2", "I", false, "Linux only: Do not set up IP addresses and do not set the link up")
	nativeTunCmd.Flags().DurationP("reconnect-delay", "r", 200*time.Millisecond, "Delay between reconnect attempts")
	nativeTunCmd.Flags().StringP("interface-name", "n", "", "Custom interface name for the TUN interface")
	nativeTunCmd.Flags().BoolP("ipv6", "6", false, "Use IPv6 for MASQUE connection")
	nativeTunCmd.Flags().IntP("connect-port", "P", 443, "Used port for MASQUE connection")
	rootCmd.AddCommand(nativeTunCmd)
}
