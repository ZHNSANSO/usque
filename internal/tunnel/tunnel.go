package tunnel

import (
	"context"
	"crypto/ecdsa"
	"crypto/tls"
	"net"
	"net/netip"
	"sync"
	"time"

	"github.com/Diniboy1123/usque/api"
	"github.com/Diniboy1123/usque/internal"
	"golang.zx2c4.com/wireguard/tun"
	"golang.zx2c4.com/wireguard/tun/netstack"
)

// TunnelConfig holds all the configuration for creating a MASQUE tunnel.
type TunnelConfig struct {
	SNI               string
	Endpoint          *net.UDPAddr
	PrivateKey        *ecdsa.PrivateKey
	PeerPublicKey     *ecdsa.PublicKey
	Keepalive         time.Duration
	InitialPacketSize uint16
	MTU               int
	ReconnectDelay    time.Duration
	LocalAddresses    []netip.Addr
	DNS               []netip.Addr
}

// Tunnel represents an active MASQUE tunnel and its associated userspace network device.
type Tunnel struct {
	Config *TunnelConfig
	Device tun.Device
	Net    *netstack.Net
	tls    *tls.Config
}

// NewTunnel creates and configures a new tunnel but does not start it.
func NewTunnel(cfg *TunnelConfig) (*Tunnel, error) {
	cert, err := internal.GenerateCert(cfg.PrivateKey, &cfg.PrivateKey.PublicKey)
	if err != nil {
		return nil, err
	}

	tlsConfig, err := api.PrepareTlsConfig(cfg.PrivateKey, cfg.PeerPublicKey, cert, cfg.SNI)
	if err != nil {
		return nil, err
	}

	tunDev, tunNet, err := netstack.CreateNetTUN(cfg.LocalAddresses, cfg.DNS, cfg.MTU)
	if err != nil {
		return nil, err
	}

	return &Tunnel{
		Config: cfg,
		Device: tunDev,
		Net:    tunNet,
		tls:    tlsConfig,
	}, nil
}

// Start begins the tunnel maintenance loop in a new goroutine.
func (t *Tunnel) Start(ctx context.Context) {
	adapter := NewNetstackAdapter(t.Device)
	go api.MaintainTunnel(ctx, t.tls, t.Config.Keepalive, t.Config.InitialPacketSize, t.Config.Endpoint, adapter, t.Config.MTU, t.Config.ReconnectDelay)
}

// Close closes the underlying TUN device.
func (t *Tunnel) Close() error {
	return t.Device.Close()
}

// NetstackAdapter wraps a tun.Device (e.g. from netstack) to satisfy TunnelDevice.
type NetstackAdapter struct {
	dev             tun.Device
	tunnelBufPool   sync.Pool
	tunnelSizesPool sync.Pool
}

func (n *NetstackAdapter) ReadPacket(buf []byte) (int, error) {
	packetBufsPtr := n.tunnelBufPool.Get().(*[][]byte)
	sizesPtr := n.tunnelSizesPool.Get().(*[]int)

	defer func() {
		(*packetBufsPtr)[0] = nil
		n.tunnelBufPool.Put(packetBufsPtr)
		n.tunnelSizesPool.Put(sizesPtr)
	}()

	(*packetBufsPtr)[0] = buf
	(*sizesPtr)[0] = 0

	_, err := n.dev.Read(*packetBufsPtr, *sizesPtr, 0)
	if err != nil {
		return 0, err
	}

	return (*sizesPtr)[0], nil
}

func (n *NetstackAdapter) WritePacket(pkt []byte) error {
	// Write expects a slice of packet buffers.
	_, err := n.dev.Write([][]byte{pkt}, 0)
	return err
}

// NewNetstackAdapter creates a new NetstackAdapter.
func NewNetstackAdapter(dev tun.Device) api.TunnelDevice {
	return &NetstackAdapter{
		dev: dev,
		tunnelBufPool: sync.Pool{
			New: func() interface{} {
				buf := make([][]byte, 1)
				return &buf
			},
		},
		tunnelSizesPool: sync.Pool{
			New: func() interface{} {
				sizes := make([]int, 1)
				return &sizes
			},
		},
	}
}
