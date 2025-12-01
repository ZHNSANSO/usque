package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/netip"
	"os"
	"time"

	"github.com/Diniboy1123/usque/cmd"
	"github.com/Diniboy1123/usque/config"
	"github.com/Diniboy1123/usque/internal/tunnel"
	"github.com/Diniboy1123/usque/web"
	"github.com/kardianos/service"
)

var reloadChan = make(chan struct{})

func main() {
	svcConfig := &service.Config{
		Name:        "usque",
		DisplayName: "Usque Service",
		Description: "Usque Warp CLI as a service.",
	}

	prg := &program{}
	s, err := service.New(prg, svcConfig)
	if err != nil {
		log.Fatal(err)
	}

	if service.Interactive() || len(os.Args) > 1 {
		if err := cmd.Execute(); err != nil {
			fmt.Println("Error:", err)
			os.Exit(1)
		}
		return
	}

	err = s.Run()
	if err != nil {
		log.Fatalf("Failed to run service: %v", err)
	}
}

type program struct {
	cancel context.CancelFunc
}

func (p *program) Start(s service.Service) error {
	ctx, cancel := context.WithCancel(context.Background())
	p.cancel = cancel
	go p.run(ctx)
	return nil
}

func (p *program) run(ctx context.Context) {
	// Try to load config
	err := config.LoadConfig("config.json")
	if err != nil {
		log.Printf("Config file not found: %v. Starting in registration mode.", err)
		// Start web server in registration-only mode
		web.StartRegistrationServer(reloadChan)
		// Wait for registration to complete
		<-reloadChan
		log.Println("Registration complete. Reloading to start main service...")
	}

	// This loop runs the main service and handles reloads
	for {
		log.Println("Starting tunnel...")
		tunnelCtx, tunnelCancel := context.WithCancel(ctx)

		// Load config again in case it was just created
		if err := config.LoadConfig("config.json"); err != nil {
			log.Printf("Failed to load config after registration/reload: %v", err)
			time.Sleep(5 * time.Second)
			continue
		}

		// Start the web server in parallel if it's not already running
		// (This part needs a bit more robust logic to avoid starting it twice)
		// For now, we assume the registration server has stopped.
		go web.StartServer(reloadChan)

		t, err := startTunnel(tunnelCtx)
		if err != nil {
			log.Printf("Failed to start tunnel: %v. Retrying...", err)
			tunnelCancel()
			time.Sleep(5 * time.Second)
			continue
		}

		select {
		case <-reloadChan:
			log.Println("Reloading configuration...")
			t.Close()
			tunnelCancel()
			// The loop will restart and pick up the new config
		case <-ctx.Done():
			log.Println("Service stopping...")
			t.Close()
			tunnelCancel()
			return
		}
	}
}

func (p *program) Stop(s service.Service) error {
	log.Println("Usque service is stopping...")
	if p.cancel != nil {
		p.cancel()
	}
	return nil
}

// startTunnel creates and starts a tunnel based on the global AppConfig.
func startTunnel(ctx context.Context) (*tunnel.Tunnel, error) {
	if !config.ConfigLoaded {
		return nil, fmt.Errorf("configuration not loaded")
	}

	privKey, err := config.AppConfig.GetEcPrivateKey()
	if err != nil {
		return nil, err
	}
	peerPubKey, err := config.AppConfig.GetEcEndpointPublicKey()
	if err != nil {
		return nil, err
	}

	tunnelCfg := &tunnel.TunnelConfig{
		SNI: "engage.cloudflareclient.com",
		Endpoint: &net.UDPAddr{
			IP:   net.ParseIP(config.AppConfig.EndpointV4),
			Port: 2408, // Default warp port
		},
		PrivateKey:        privKey,
		PeerPublicKey:     peerPubKey,
		Keepalive:         30 * time.Second,
		InitialPacketSize: 1242,
		MTU:               1280,
		ReconnectDelay:    1 * time.Second,
		LocalAddresses: []netip.Addr{
			netip.MustParseAddr(config.AppConfig.IPv4),
			netip.MustParseAddr(config.AppConfig.IPv6),
		},
		DNS: []netip.Addr{
			netip.MustParseAddr("9.9.9.9"),
		},
	}

	t, err := tunnel.NewTunnel(tunnelCfg)
	if err != nil {
		return nil, err
	}

	t.Start(ctx)
	return t, nil
}
