package api

import (
	"context"
	"crypto/ecdsa"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"log"
	"net"
	"net/http"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	connectip "github.com/Diniboy1123/connect-ip-go"
	"github.com/Diniboy1123/usque/config"
	"github.com/Diniboy1123/usque/internal"
	"github.com/quic-go/quic-go"
	"github.com/quic-go/quic-go/http3"
	"github.com/yosida95/uritemplate/v3"
)

// PrepareTlsConfig creates a TLS configuration using the provided certificate and SNI.
func PrepareTlsConfig(privKey *ecdsa.PrivateKey, peerPubKey *ecdsa.PublicKey, cert [][]byte, sni string) (*tls.Config, error) {
	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{
			{
				Certificate: cert,
				PrivateKey:  privKey,
			},
		},
		ServerName: sni,
		NextProtos: []string{http3.NextProtoH3},
		// WARN: SNI is usually not for the endpoint, so we must skip verification
		InsecureSkipVerify: true,
		// we pin to the endpoint public key
		VerifyPeerCertificate: func(rawCerts [][]byte, verifiedChains [][]*x509.Certificate) error {
			if len(rawCerts) == 0 {
				return nil
			}

			cert, err := x509.ParseCertificate(rawCerts[0])
			if err != nil {
				return err
			}

			if _, ok := cert.PublicKey.(*ecdsa.PublicKey); !ok {
				// we only support ECDSA
				return x509.ErrUnsupportedAlgorithm
			}

			if !cert.PublicKey.(*ecdsa.PublicKey).Equal(peerPubKey) {
				return x509.CertificateInvalidError{Cert: cert, Reason: 10, Detail: "remote endpoint has a different public key than what we trust in config.json"}
			}

			return nil
		},
	}

	return tlsConfig, nil
}

// ConnectTunnel establishes a QUIC connection and sets up a Connect-IP tunnel.
// Implements a generalized Happy Eyeballs (RFC 8305) to race candidate endpoints.
func ConnectTunnel(ctx context.Context, tlsConfig *tls.Config, quicConfig *quic.Config, connectUri string, endpoints []*net.UDPAddr) (*net.UDPConn, *http3.Transport, *connectip.Conn, *http.Response, error) {
	type dialResult struct {
		udpConn *net.UDPConn
		tr      *http3.Transport
		ipConn  *connectip.Conn
		rsp     *http.Response
		err     error
	}

	resultCh := make(chan dialResult, len(endpoints))
	dialCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	var wg sync.WaitGroup

	dial := func(ep *net.UDPAddr) {
		defer wg.Done()

		var udpConn *net.UDPConn
		var err error

		if ep.IP.To4() == nil {
			udpConn, err = net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv6zero, Port: 0})
		} else {
			udpConn, err = net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4zero, Port: 0})
		}

		if err != nil {
			return
		}

		qConn, err := quic.Dial(dialCtx, udpConn, ep, tlsConfig, quicConfig)
		if err != nil {
			udpConn.Close()
			return
		}

		tr := &http3.Transport{
			EnableDatagrams:    true,
			AdditionalSettings: map[uint64]uint64{0x276: 1}, // SETTINGS_H3_DATAGRAM_00
			DisableCompression: true,
		}

		hconn := tr.NewClientConn(qConn)
		additionalHeaders := http.Header{"User-Agent": []string{""}}
		template := uritemplate.MustNew(connectUri)

		ipConn, rsp, err := connectip.Dial(dialCtx, hconn, template, "cf-connect-ip", additionalHeaders, true)
		if err != nil {
			qConn.CloseWithError(0, "dial failed")
			udpConn.Close()
			return
		}

		if rsp.StatusCode != 200 {
			ipConn.Close()
			qConn.CloseWithError(0, "bad status")
			udpConn.Close()
			return
		}

		select {
		case resultCh <- dialResult{udpConn: udpConn, tr: tr, ipConn: ipConn, rsp: rsp}:
			cancel() // Winner! Stop others.
		case <-dialCtx.Done():
			ipConn.Close()
			qConn.CloseWithError(0, "lost race")
			udpConn.Close()
		}
	}

	for i, ep := range endpoints {
		if ep == nil {
			continue
		}
		wg.Add(1)
		go dial(ep)

		// Happy Eyeballs Delay: wait 200ms before starting next attempt
		if i < len(endpoints)-1 {
			timer := time.NewTimer(200 * time.Millisecond)
			select {
			case <-timer.C:
			case <-dialCtx.Done():
				timer.Stop()
			}
		}
	}

	go func() {
		wg.Wait()
		close(resultCh)
	}()

	res, ok := <-resultCh
	if !ok {
		return nil, nil, nil, nil, errors.New("all connection attempts failed")
	}

	return res.udpConn, res.tr, res.ipConn, res.rsp, nil
}

// MaintainTunnel continuously connects to the MASQUE server and handles packet forwarding.
func MaintainTunnel(ctx context.Context, config *config.Config, keepalivePeriod time.Duration, initialPacketSize uint16, device TunnelDevice, mtu int, reconnectDelay time.Duration) {
	packetBufferPool := NewNetBuffer(mtu)
	var activeConn atomic.Pointer[connectip.Conn]

	// 1. Prepare TLS Config with SNI Spoofing
	sni := config.SNI
	if sni == "" {
		sni = internal.ConnectSNI
	}

	privKey, err := config.GetEcPrivateKey()
	if err != nil {
		log.Fatalf("Failed to get private key: %v", err)
	}
	peerPubKey, err := config.GetEcEndpointPublicKey()
	if err != nil {
		log.Fatalf("Failed to get endpoint public key: %v", err)
	}
	cert, err := internal.GenerateCert(privKey, &privKey.PublicKey)
	if err != nil {
		log.Fatalf("Failed to generate cert: %v", err)
	}

	tlsConfig, err := PrepareTlsConfig(privKey, peerPubKey, cert, sni)
	if err != nil {
		log.Fatalf("Failed to prepare TLS config: %v", err)
	}

	// Persistent TUN Reader Loop
	go func() {
		buf := packetBufferPool.Get()
		defer packetBufferPool.Put(buf)

		for {
			n, err := device.ReadPacket(buf)
			if err != nil {
				return
			}
			conn := activeConn.Load()
			if conn != nil {
				conn.WritePacket(buf[:n])
			}
		}
	}()

	// Connection Maintenance Loop
	for {
		if ctx.Err() != nil {
			return
		}

		// Collect all possible endpoints from config and prioritize IPv6
		var rawEndpoints []string
		if config.Endpoint != "" {
			rawEndpoints = append(rawEndpoints, config.Endpoint)
		}
		if config.EndpointV6 != "" {
			rawEndpoints = append(rawEndpoints, config.EndpointV6)
		}
		if config.EndpointV4 != "" {
			rawEndpoints = append(rawEndpoints, config.EndpointV4)
		}

		// Debug: log what we found in config
		log.Printf("Config points: endpoint=%q, v6=%s, v4=%s", config.Endpoint, config.EndpointV6, config.EndpointV4)

		var endpoints []*net.UDPAddr
		seen := make(map[string]bool)

		for _, raw := range rawEndpoints {
			for _, ep := range internal.DeriveEndpoints(raw) {
				s := ep.String()
				if !seen[s] {
					endpoints = append(endpoints, ep)
					seen[s] = true
				}
			}
		}

		// Re-sort to ensure IPv6 candidates come first
		sort.SliceStable(endpoints, func(i, j int) bool {
			return endpoints[i].IP.To4() == nil && endpoints[j].IP.To4() != nil
		})

		if len(endpoints) == 0 {
			log.Fatalf("Invalid or empty endpoint configuration")
		}

		log.Printf("Establishing MASQUE connection (SNI: %s, Candidates: %v)", sni, endpoints)
		udpConn, tr, ipConn, _, err := ConnectTunnel(
			ctx,
			tlsConfig,
			internal.DefaultQuicConfig(keepalivePeriod, initialPacketSize),
			internal.ConnectURI,
			endpoints,
		)

		if err != nil {
			log.Printf("Failed to connect tunnel: %v", err)
			time.Sleep(reconnectDelay)
			continue
		}

		log.Println("Connected to MASQUE server")
		activeConn.Store(ipConn)

		// IP -> TUN Reader Loop (Block until connection dies)
		readBuf := packetBufferPool.Get()
		for {
			n, err := ipConn.ReadPacket(readBuf, true)
			if err != nil {
				break
			}
			device.WritePacket(readBuf[:n])
		}
		packetBufferPool.Put(readBuf)

		// Teardown
		activeConn.Store(nil)
		ipConn.Close()
		if udpConn != nil {
			udpConn.Close()
		}
		if tr != nil {
			tr.Close()
		}
		time.Sleep(reconnectDelay)
	}
}
