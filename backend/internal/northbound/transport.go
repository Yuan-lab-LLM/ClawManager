package northbound

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"
	"strings"

	"clawreef/internal/config"
)

func GatewayTLSConfig(cfg config.NorthboundConfig) (*tls.Config, error) {
	cert, err := tls.LoadX509KeyPair(strings.TrimSpace(cfg.GatewayClientCertFile), strings.TrimSpace(cfg.GatewayClientKeyFile))
	if err != nil {
		return nil, fmt.Errorf("load northbound gateway client certificate: %w", err)
	}
	caPEM, err := os.ReadFile(strings.TrimSpace(cfg.CoreCAFile))
	if err != nil {
		return nil, fmt.Errorf("read northbound Core CA: %w", err)
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(caPEM) {
		return nil, fmt.Errorf("northbound Core CA contains no certificates")
	}
	return &tls.Config{
		MinVersion:   tls.VersionTLS13,
		Certificates: []tls.Certificate{cert},
		RootCAs:      roots,
	}, nil
}

func CoreTLSConfig(cfg config.NorthboundConfig) (*tls.Config, error) {
	serverCertificate, err := tls.LoadX509KeyPair(strings.TrimSpace(cfg.CoreTLSCertFile), strings.TrimSpace(cfg.CoreTLSKeyFile))
	if err != nil {
		return nil, fmt.Errorf("load northbound Core server certificate: %w", err)
	}
	caPEM, err := os.ReadFile(strings.TrimSpace(cfg.CoreClientCAFile))
	if err != nil {
		return nil, fmt.Errorf("read northbound Gateway client CA: %w", err)
	}
	clients := x509.NewCertPool()
	if !clients.AppendCertsFromPEM(caPEM) {
		return nil, fmt.Errorf("northbound Gateway client CA contains no certificates")
	}
	return &tls.Config{
		MinVersion:   tls.VersionTLS13,
		Certificates: []tls.Certificate{serverCertificate},
		ClientAuth:   tls.RequireAndVerifyClientCert,
		ClientCAs:    clients,
	}, nil
}
