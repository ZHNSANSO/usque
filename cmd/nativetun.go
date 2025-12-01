package cmd

import (
	"context"
	"log"

	"github.com/Diniboy1123/usque/api"
	"github.com/Diniboy1123/usque/config"
	"github.com/Diniboy1123/usque/internal"
	"github.com/spf13/cobra"
)

type tunDevice struct {
	name     string
	mtu      int
	iproute2 bool
	ipv4     bool
	ipv6     bool
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

		// Create tunnel config from flags
		tunnelCfg, err := NewTunnelConfigFromFlags(cmd)
		if err != nil {
			log.Fatalf("Failed to create tunnel config: %v", err)
		}

		cert, err := internal.GenerateCert(tunnelCfg.PrivateKey, &tunnelCfg.PrivateKey.PublicKey)
		if err != nil {
			log.Fatalf("Failed to generate cert: %v", err)
		}

		tlsConfig, err := api.PrepareTlsConfig(tunnelCfg.PrivateKey, tunnelCfg.PeerPublicKey, cert, tunnelCfg.SNI)
		if err != nil {
			log.Fatalf("Failed to prepare TLS config: %v", err)
		}

		setIproute2, err := cmd.Flags().GetBool("no-iproute2")
		if err != nil {
			log.Fatalf("Failed to get no set address: %v", err)
		}

		interfaceName, err := cmd.Flags().GetString("interface-name")
		if err != nil {
			log.Fatalf("Failed to get interface name: %v", err)
		}

		if interfaceName != "" {
			err = internal.CheckIfname(interfaceName)
			if err != nil {
				log.Printf("Invalid interface name: %v", err)
				return
			}
		}

		t := &tunDevice{
			name:     interfaceName,
			mtu:      tunnelCfg.MTU,
			iproute2: !setIproute2,
			ipv4:     len(tunnelCfg.LocalAddresses) > 0 && tunnelCfg.LocalAddresses[0].Is4(),
			ipv6:     (len(tunnelCfg.LocalAddresses) > 0 && tunnelCfg.LocalAddresses[0].Is6()) || (len(tunnelCfg.LocalAddresses) > 1 && tunnelCfg.LocalAddresses[1].Is6()),
		}

		dev, err := t.create()
		if err != nil {
			log.Println("Are you root/administrator? TUN device creation usually requires elevated privileges.")
			log.Fatalf("Failed to create TUN device: %v", err)
		}

		log.Printf("Created TUN device: %s", t.name)

		adapter := api.NewWaterAdapter(dev)
		go api.MaintainTunnel(context.Background(), tlsConfig, tunnelCfg.Keepalive, tunnelCfg.InitialPacketSize, tunnelCfg.Endpoint, adapter, tunnelCfg.MTU, tunnelCfg.ReconnectDelay)

		log.Println("Tunnel established, you may now set up routing and DNS")

		select {}
	},
}

func init() {
	// Add tunnel-specific flags
	AddTunnelFlags(nativeTunCmd)

	// Add command-specific flags
	nativeTunCmd.Flags().BoolP("no-iproute2", "I", false, "Linux only: Do not set up IP addresses and do not set the link up")
	nativeTunCmd.Flags().StringP("interface-name", "n", "", "Custom inteface name for the TUN interface")

	rootCmd.AddCommand(nativeTunCmd)
}
