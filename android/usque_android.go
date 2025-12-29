// Package usqueandroid provides Android-callable functions for the usque VPN library.
// This package is designed to be compiled with gomobile bind to produce an .aar file.
package usqueandroid

import (
	"context"
	"encoding/base64"
	"fmt"
	"log"
	"os"
	"sync"
	"time"

	"github.com/Diniboy1123/usque/api"
	"github.com/Diniboy1123/usque/config"
	"github.com/Diniboy1123/usque/internal"
)

// PacketFlow is the interface that Android must implement to exchange packets with the VPN.
type PacketFlow interface {
	// WritePacket writes an IP packet to the Android TUN device.
	WritePacket(data []byte)
}

// VpnStateCallback is the interface for VPN state notifications.
type VpnStateCallback interface {
	OnConnected()
	OnDisconnected(reason string)
	OnError(message string)
}

type tunnelState struct {
	mu        sync.Mutex
	running   bool
	cancel    context.CancelFunc
	inputChan chan []byte
	callback  VpnStateCallback
}

var state = &tunnelState{}

// Custom connection options
var (
	customSNI      = internal.ConnectSNI
	customEndpoint = ""
)

// Register creates a new Cloudflare WARP account and saves the configuration.
func Register(configPath string, deviceName string) string {
	if err := config.LoadConfig(configPath); err == nil {
		return "" // Config already exists and is valid
	}

	accountData, err := api.Register(internal.DefaultModel, internal.DefaultLocale, "", true)
	if err != nil {
		return fmt.Sprintf("Registration failed: %v", err)
	}

	privKey, pubKey, err := internal.GenerateEcKeyPair()
	if err != nil {
		return fmt.Sprintf("Failed to generate key pair: %v", err)
	}

	updatedAccountData, apiErr, err := api.EnrollKey(accountData, pubKey, deviceName)
	if err != nil {
		if apiErr != nil {
			return fmt.Sprintf("Failed to enroll key: %v (API: %s)", err, apiErr.ErrorsAsString("; "))
		}
		return fmt.Sprintf("Failed to enroll key: %v", err)
	}

	config.AppConfig = config.Config{
		PrivateKey:     base64.StdEncoding.EncodeToString(privKey),
		Endpoint:       updatedAccountData.Config.Peers[0].Endpoint.Host,
		EndpointPubKey: updatedAccountData.Config.Peers[0].PublicKey,
		License:        updatedAccountData.Account.License,
		ID:             updatedAccountData.ID,
		AccessToken:    accountData.Token,
		IPv4:           updatedAccountData.Config.Interface.Addresses.V4,
		IPv6:           updatedAccountData.Config.Interface.Addresses.V6,
	}

	if err := config.AppConfig.SaveConfig(configPath); err != nil {
		return fmt.Sprintf("Failed to save config: %v", err)
	}

	return ""
}

// IsRegistered checks if a valid configuration exists.
func IsRegistered(configPath string) bool {
	return config.LoadConfig(configPath) == nil
}

// GetAssignedIPv4 returns the assigned IPv4 address.
func GetAssignedIPv4(configPath string) string {
	if config.LoadConfig(configPath) == nil {
		return config.AppConfig.IPv4
	}
	return ""
}

// GetAssignedIPv6 returns the assigned IPv6 address.
func GetAssignedIPv6(configPath string) string {
	if config.LoadConfig(configPath) == nil {
		return config.AppConfig.IPv6
	}
	return ""
}

type androidTunDevice struct {
	fd       int
	file     *os.File
	mtu      int
	outputFn PacketFlow
}

func (d *androidTunDevice) ReadPacket(buf []byte) (int, error) {
	return d.file.Read(buf)
}

func (d *androidTunDevice) WritePacket(pkt []byte) error {
	if d.outputFn != nil {
		d.outputFn.WritePacket(pkt)
		return nil
	}
	_, err := d.file.Write(pkt)
	return err
}

// StartTunnel starts the VPN tunnel using the provided file descriptor.
func StartTunnel(configPath string, tunFd int, mtu int, packetFlow PacketFlow, callback VpnStateCallback) string {
	state.mu.Lock()
	defer state.mu.Unlock()

	if state.running {
		return "Tunnel is already running"
	}

	if err := config.LoadConfig(configPath); err != nil {
		return fmt.Sprintf("Failed to load config: %v", err)
	}

	// Prepare Tun device wrapper
	file := os.NewFile(uintptr(tunFd), "tun")
	if file == nil {
		return fmt.Sprintf("Failed to create file from fd %d", tunFd)
	}

	tunDev := &androidTunDevice{
		fd:       tunFd,
		file:     file,
		mtu:      mtu,
		outputFn: packetFlow,
	}

	// Update Runtime Config
	runtimeConfig := config.AppConfig
	if customSNI != "" {
		runtimeConfig.SNI = customSNI
	}
	if customEndpoint != "" {
		runtimeConfig.Endpoint = customEndpoint
	}

	ctx, cancel := context.WithCancel(context.Background())
	state.cancel = cancel
	state.running = true
	state.callback = callback

	go func() {
		log.Println("Starting MASQUE tunnel on Android...")

		// Notify connected after a brief delay (heuristic)
		go func() {
			time.Sleep(3 * time.Second)
			state.mu.Lock()
			running := state.running
			cb := state.callback
			state.mu.Unlock()
			if running && cb != nil {
				cb.OnConnected()
			}
		}()

		api.MaintainTunnel(ctx, &runtimeConfig, 30*time.Second, 1242, tunDev, mtu, time.Second)

		// Cleanup
		tunDev.file.Close()
		state.mu.Lock()
		state.running = false
		cb := state.callback
		state.mu.Unlock()
		if cb != nil {
			cb.OnDisconnected("Tunnel closed")
		}
	}()

	return ""
}

// InputPacket is called by Android whenever a packet is read from the TUN device.
// This is used for Alternative: File Descriptor based approach.
func InputPacket(data []byte) {
	// Not needed if using the direct fd reading in MaintainTunnel,
	// but kept for API completeness if Android wants to push packets.
}

// StopTunnel stops the running tunnel.
func StopTunnel() {
	state.mu.Lock()
	defer state.mu.Unlock()
	if !state.running {
		return
	}
	if state.cancel != nil {
		state.cancel()
	}
	state.running = false
}

// IsRunning returns true if the tunnel is active.
func IsRunning() bool {
	state.mu.Lock()
	defer state.mu.Unlock()
	return state.running
}

// SetSNI sets a custom SNI for the TLS connection.
func SetSNI(sni string) {
	customSNI = sni
}

// SetEndpoint sets a custom endpoint (host:port).
func SetEndpoint(endpoint string) {
	customEndpoint = endpoint
}

// ResetConnectionOptions resets SNI and Endpoint to defaults.
func ResetConnectionOptions() {
	customSNI = internal.ConnectSNI
	customEndpoint = ""
}

// GetVersion returns the library version.
func GetVersion() string {
	return "1.0.3-android-core"
}
