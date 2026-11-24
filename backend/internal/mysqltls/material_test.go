package mysqltls

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/binary"
	"encoding/hex"
	"encoding/pem"
	"io"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	mysql "github.com/go-sql-driver/mysql"
)

func mysqlBusinessTLSFixture(t *testing.T) (Material, tls.Certificate) {
	return mysqlBusinessTLSFixtureCA(t, x509.KeyUsageCertSign|x509.KeyUsageDigitalSignature, nil)
}

func mysqlBusinessTLSFixtureCA(t *testing.T, usage x509.KeyUsage, trailer []byte) (Material, tls.Certificate) {
	t.Helper()
	pub, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	ca := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "owned protocol fixture CA"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), BasicConstraintsValid: true, IsCA: true, KeyUsage: usage}
	caDER, err := x509.CreateCertificate(rand.Reader, ca, ca, pub, key)
	if err != nil {
		t.Fatal(err)
	}
	serverPub, serverKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	leaf := &x509.Certificate{SerialNumber: big.NewInt(2), DNSNames: []string{"mysql.original.invalid"}, NotBefore: ca.NotBefore, NotAfter: ca.NotAfter, KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	der, err := x509.CreateCertificate(rand.Reader, leaf, ca, serverPub, key)
	if err != nil {
		t.Fatal(err)
	}
	raw := append(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDER}), trailer...)
	file := filepath.Join(t.TempDir(), "original-ca.pem")
	if os.WriteFile(file, raw, 0600) != nil {
		t.Fatal("CA fixture write")
	}
	sum := sha256.Sum256(raw)
	return Material{CAFile: file, CASHA256: hex.EncodeToString(sum[:]), ServerName: "mysql.original.invalid"}, tls.Certificate{Certificate: [][]byte{der}, PrivateKey: serverKey}
}

func TestMySQLTLSRejectsUnrelatedPEMTrailerAndExplicitWrongKeyUsage(t *testing.T) {
	for _, tc := range []struct {
		usage   x509.KeyUsage
		trailer []byte
	}{
		{usage: x509.KeyUsageDigitalSignature},
		{trailer: []byte{0, 0}},
		{trailer: []byte("unrelated contents")},
	} {
		material, _ := mysqlBusinessTLSFixtureCA(t, tc.usage, tc.trailer)
		if _, err := material.TLSConfig(); err == nil {
			t.Fatal("invalid CA material accepted")
		}
	}
}
func TestBusinessMySQLTLSSelectionPinsAndIsolation(t *testing.T) {
	if tlsConfig, err := (Material{}).TLSConfig(); err != nil || tlsConfig != nil {
		t.Fatal("legacy zero config changed", err)
	}
	cfg, _ := mysqlBusinessTLSFixture(t)
	first, err := businessMySQLDriverConfig(cfg, "fixture:PRIVATE_CANARY@tcp(127.0.0.1:3306)/test")
	if err != nil {
		t.Fatal(err)
	}
	second, err := businessMySQLDriverConfig(cfg, "fixture:PRIVATE_CANARY@tcp(127.0.0.1:3306)/test")
	if err != nil {
		t.Fatal(err)
	}
	if first.TLS == nil || first.TLS.MinVersion != tls.VersionTLS13 || first.TLS.InsecureSkipVerify || first.TLS.RootCAs == nil || first.TLS.ServerName != cfg.ServerName || first.AllowFallbackToPlaintext || first.TLSConfig != "" {
		t.Fatal("TLS identity weakened")
	}
	first.TLS.ServerName = "mutated.invalid"
	if second.TLS.ServerName != cfg.ServerName || first.TLS == second.TLS || first.TLS.RootCAs == second.TLS.RootCAs {
		t.Fatal("connectors share mutable TLS config")
	}
	for _, mode := range []string{"partial", "wrong-pin", "relative", "wildcard", "preferred", "skip-verify"} {
		t.Run(mode, func(t *testing.T) {
			bad := cfg
			dsn := "fixture:PRIVATE_CANARY@tcp(127.0.0.1:3306)/test"
			switch mode {
			case "partial":
				bad.CAFile = ""
			case "wrong-pin":
				bad.CASHA256 = strings.Repeat("a", 64)
			case "relative":
				bad.CAFile = "relative.pem"
			case "wildcard":
				bad.ServerName = "*.invalid"
			default:
				dsn += "?tls=" + mode
			}
			if _, err := businessMySQLDriverConfig(bad, dsn); err == nil || strings.Contains(err.Error(), "PRIVATE_CANARY") {
				t.Fatal("invalid or unsafe selection accepted/leaked", err)
			}
		})
	}
}

func mysqlFixturePacket(w io.Writer, seq byte, body []byte) error {
	header := []byte{byte(len(body)), byte(len(body) >> 8), byte(len(body) >> 16), seq}
	if _, err := w.Write(header); err != nil {
		return err
	}
	_, err := w.Write(body)
	return err
}
func mysqlFixtureRead(r io.Reader) ([]byte, error) {
	var header [4]byte
	if _, err := io.ReadFull(r, header[:]); err != nil {
		return nil, err
	}
	size := int(header[0]) | int(header[1])<<8 | int(header[2])<<16
	if size > 65536 {
		return nil, io.ErrUnexpectedEOF
	}
	body := make([]byte, size)
	_, err := io.ReadFull(r, body)
	return body, err
}

// Real mysql-driver connector -> protocol greeting/SSLRequest -> TLS handshake
// -> auth response -> OK. The peer is an explicit local protocol fixture,
// not a real MySQL installation or a field certificate/require_secure_transport proof.
func TestBusinessMySQLTLSActualDriverHandshake(t *testing.T) {
	for _, mode := range []string{"tls13", "mysql-generated-ca", "wrong-name", "wrong-ca", "tls12-only", "no-server-tls"} {
		t.Run(mode, func(t *testing.T) {
			cfg, certificate := mysqlBusinessTLSFixture(t)
			if mode == "mysql-generated-ca" {
				cfg, certificate = mysqlBusinessTLSFixtureCA(t, 0, []byte{0})
			}
			if mode == "wrong-name" {
				cfg.ServerName = "different.original.invalid"
			}
			if mode == "wrong-ca" {
				other, _ := mysqlBusinessTLSFixture(t)
				cfg.CAFile, cfg.CASHA256 = other.CAFile, other.CASHA256
			}
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			defer listener.Close()
			var tlsVersion atomic.Uint32
			var plaintextAuth atomic.Bool
			done := make(chan struct{})
			go func() {
				defer close(done)
				conn, e := listener.Accept()
				if e != nil {
					return
				}
				defer conn.Close()
				conn.SetDeadline(time.Now().Add(4 * time.Second))
				flags := uint32(1 | 4 | 8 | 512 | 8192 | 32768 | 0x80000)
				if mode != "no-server-tls" {
					flags |= 2048
				}
				greeting := append([]byte{10}, []byte("8.0.0-fixture\x00")...)
				greeting = append(greeting, 1, 0, 0, 0)
				greeting = append(greeting, []byte("12345678")...)
				greeting = append(greeting, 0, byte(flags), byte(flags>>8), 45, 2, 0, byte(flags>>16), byte(flags>>24), 21)
				greeting = append(greeting, make([]byte, 10)...)
				greeting = append(greeting, []byte("abcdefghijkl\x00mysql_native_password\x00")...)
				if mysqlFixturePacket(conn, 0, greeting) != nil {
					return
				}
				request, e := mysqlFixtureRead(conn)
				if e != nil {
					return
				}
				if mode == "no-server-tls" {
					plaintextAuth.Store(len(request) > 0)
					return
				}
				if len(request) != 32 || binary.LittleEndian.Uint32(request[:4])&2048 == 0 {
					plaintextAuth.Store(true)
					return
				}
				serverConfig := &tls.Config{Certificates: []tls.Certificate{certificate}, MinVersion: tls.VersionTLS13}
				if mode == "tls12-only" {
					serverConfig.MinVersion = tls.VersionTLS12
					serverConfig.MaxVersion = tls.VersionTLS12
				}
				secure := tls.Server(conn, serverConfig)
				if secure.Handshake() != nil {
					return
				}
				tlsVersion.Store(uint32(secure.ConnectionState().Version))
				if _, e = mysqlFixtureRead(secure); e != nil {
					return
				}
				mysqlFixturePacket(secure, 3, []byte{0, 0, 0, 2, 0, 0, 0})
			}()
			driverCfg, err := businessMySQLDriverConfig(cfg, "fixture:PRIVATE_CANARY@tcp("+listener.Addr().String()+")/test")
			if err != nil {
				t.Fatal(err)
			}
			driverCfg.Timeout = time.Second
			driverCfg.ReadTimeout = time.Second
			driverCfg.WriteTimeout = time.Second
			connector, err := mysql.NewConnector(driverCfg)
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			conn, err := connector.Connect(ctx)
			success := mode == "tls13" || mode == "mysql-generated-ca"
			if success {
				if err != nil {
					t.Fatal("actual mysql TLS13 connector rejected", err)
				}
				conn.Close()
			} else {
				if err == nil {
					conn.Close()
					t.Fatal("invalid peer/TLS negotiated")
				}
				if strings.Contains(err.Error(), "PRIVATE_CANARY") {
					t.Fatal("credential leaked")
				}
			}
			<-done
			if plaintextAuth.Load() {
				t.Fatal("connector sent plaintext authentication")
			}
			if success && tlsVersion.Load() != tls.VersionTLS13 {
				t.Fatal("did not negotiate TLS13")
			}
		})
	}
}

func businessMySQLDriverConfig(c Material, dsn string) (*mysql.Config, error) {
	driver, err := mysql.ParseDSN(dsn)
	if err != nil {
		return nil, ErrConfiguration
	}
	if err = c.Apply(driver); err != nil {
		return nil, err
	}
	return driver, nil
}
