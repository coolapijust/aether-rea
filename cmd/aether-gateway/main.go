package main

import (
	"context"
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"flag"
	"fmt"
	"io"
	"log"
	"math/big"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"aether-rea/internal/core"

	"github.com/quic-go/quic-go"
	"github.com/quic-go/quic-go/http3"
	"github.com/quic-go/webtransport-go"
)

var (
	listenAddr = flag.String("listen", ":8080", "Listen address")
	certFile   = flag.String("cert", "cert.pem", "TLS certificate file")
	keyFile    = flag.String("key", "key.pem", "TLS key file")
	psk        = flag.String("psk", "", "Pre-shared key")
	secretPath = flag.String("path", "/v1/api/sync", "Secret path for WebTransport")
)

func main() {
	flag.Parse()

	// Support $PORT or $LISTEN_ADDR environment variables
	if envPort := os.Getenv("PORT"); envPort != "" {
		*listenAddr = "0.0.0.0:" + envPort
		log.Printf("Config: Using PORT environment variable: %s", *listenAddr)
	} else if envAddr := os.Getenv("LISTEN_ADDR"); envAddr != "" {
		*listenAddr = envAddr
		log.Printf("Config: Using LISTEN_ADDR environment variable: %s", *listenAddr)
	} else {
		// Ensure we bind to all interfaces if just a port is provided or default
		if strings.HasPrefix(*listenAddr, ":") {
			*listenAddr = "0.0.0.0" + *listenAddr
		}
		log.Printf("Config: Using default/flag listen address: %s", *listenAddr)
	}

	// Support $PSK and $DOMAIN environment variables
	if envPSK := os.Getenv("PSK"); envPSK != "" && *psk == "" {
		*psk = envPSK
	}
	domainEnv := os.Getenv("DOMAIN")

	// Support $SSL_CERT_FILE and $SSL_KEY_FILE for platform managed certs
	if envCert := os.Getenv("SSL_CERT_FILE"); envCert != "" {
		*certFile = envCert
	}
	if envKey := os.Getenv("SSL_KEY_FILE"); envKey != "" {
		*keyFile = envKey
	}

	if *psk == "" {
		log.Println("ERROR: PSK is required. Please set -psk flag or PSK environment variable.")
		os.Exit(1)
	}

	// Try to load TLS certs, fallback to self-signed
	var certs tls.Certificate
	var certErr error

	if *certFile != "" && *keyFile != "" {
		certs, certErr = tls.LoadX509KeyPair(*certFile, *keyFile)
	}

	if certErr != nil || (*certFile == "" && *keyFile == "") {
		log.Printf("No TLS certificates provided or failed to load. Generating self-signed certificate for testing...")
		certs, certErr = generateSelfSignedCert(domainEnv)
		if certErr != nil {
			log.Fatalf("Failed to generate self-signed cert: %v", certErr)
		}
	}

	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{certs},
		NextProtos:   []string{http3.NextProtoH3},
	}

	quicConfig := &quic.Config{
		EnableDatagrams: true,
	}

	server := webtransport.Server{
		H3: http3.Server{
			Addr:       *listenAddr,
			TLSConfig:  tlsConfig,
			QUICConfig: quicConfig,
		},
		CheckOrigin: func(r *http.Request) bool { return true },
	}

	http.HandleFunc(*secretPath, func(w http.ResponseWriter, r *http.Request) {
		session, err := server.Upgrade(w, r)
		if err != nil {
			log.Printf("Upgrade failed: %v", err)
			w.WriteHeader(500)
			return
		}
		handleSession(session, *psk)
	})

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<!doctype html>
<html lang="en">
  <head>
    <meta charset="utf-8" />
    <title>Aether Edge Relay</title>
  </head>
  <body>
    <h1>Aether Edge Relay</h1>
    <p>Operational</p>
  </body>
</html>`))
	})

	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})

	log.Printf("Listening on %s", *listenAddr)
	if err := server.ListenAndServe(); err != nil {
		log.Fatal(err)
	}
}

func handleSession(session *webtransport.Session, psk string) {
	log.Println("New session established")
	
	// Create a stream counter
	var streamID uint64 = 0

	for {
		stream, err := session.AcceptStream(context.Background())
		if err != nil {
			log.Printf("AcceptStream failed: %v", err)
			break
		}
		streamID++
		go handleStream(stream, psk, streamID)
	}
}

func handleStream(stream webtransport.Stream, psk string, streamID uint64) {
	defer stream.Close()

	reader := core.NewRecordReader(stream)
	
	// Read Metadata
	record, err := reader.ReadNextRecord()
	if err != nil {
		log.Printf("[Stream %d] Failed to read metadata record: %v", streamID, err)
		writeError(stream, 0x0001, "metadata required")
		return
	}

	if record.Type != core.TypeMetadata {
		log.Printf("[Stream %d] Invalid record type: %d", streamID, record.Type)
		writeError(stream, 0x0001, "metadata required")
		return
	}

	meta, err := core.DecryptMetadata(record, psk, streamID)
	if err != nil {
		log.Printf("[Stream %d] Decrypt failed: %v", streamID, err)
		writeError(stream, 0x0002, "metadata decrypt failed")
		return
	}

	targetAddr := fmt.Sprintf("%s:%d", meta.Host, meta.Port)
	log.Printf("[Stream %d] Connecting to %s", streamID, targetAddr)

	conn, err := net.DialTimeout("tcp", targetAddr, 10*time.Second)
	if err != nil {
		log.Printf("[Stream %d] Connect failed: %v", streamID, err)
		writeError(stream, 0x0004, "connect failed")
		return
	}
	defer conn.Close()

	// Bidirectional pipe
	errCh := make(chan error, 2)

	// WebTransport -> TCP
	go func() {
		buf := make([]byte, 32*1024)
		for {
			n, err := reader.Read(buf)
			if n > 0 {
				if _, wErr := conn.Write(buf[:n]); wErr != nil {
					errCh <- wErr
					return
				}
			}
			if err != nil {
				if err != io.EOF {
					errCh <- err
				} else {
					errCh <- nil
				}
				return
			}
		}
	}()

	// TCP -> WebTransport
	go func() {
		buf := make([]byte, 32*1024)
		for {
			n, err := conn.Read(buf)
			if n > 0 {
				// Encrypt/Wrap in Data Record
				recordBytes, err := core.BuildDataRecord(buf[:n], meta.Options.MaxPadding)
				if err != nil {
					errCh <- err
					return
				}
				if _, wErr := stream.Write(recordBytes); wErr != nil {
					errCh <- wErr
					return
				}
			}
			if err != nil {
				if err != io.EOF {
					// Ignore "use of closed network connection" if caused by other side closing
					if !strings.Contains(err.Error(), "closed network connection") {
						errCh <- err
					} else {
						errCh <- nil
					}
				} else {
					errCh <- nil
				}
				return
			}
		}
	}()

	select {
	case err := <-errCh:
		if err != nil {
			log.Printf("[Stream %d] Stream error: %v", streamID, err)
		}
	}
	// Cleanup happens via defer stream.Close() and defer conn.Close()
}

func writeError(w io.Writer, code uint16, msg string) {
	record, _ := core.BuildErrorRecord(code, msg)
	w.Write(record)
}

func generateSelfSignedCert(domain string) (tls.Certificate, error) {
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return tls.Certificate{}, err
	}

	subject := pkix.Name{
		Organization: []string{"Aether Edge Relay Self-Signed"},
	}
	if domain != "" {
		subject.CommonName = domain
	}

	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      subject,
		NotBefore:    time.Now(),
		NotAfter:     time.Now().Add(time.Hour * 24 * 365),

		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		DNSNames:             []string{domain},
	}
	if domain == "" {
		template.DNSNames = []string{"localhost"}
	}

	derBytes, err := x509.CreateCertificate(rand.Reader, &template, &template, &priv.PublicKey, priv)
	if err != nil {
		return tls.Certificate{}, err
	}

	certBuf := &bytes.Buffer{}
	pem.Encode(certBuf, &pem.Block{Type: "CERTIFICATE", Bytes: derBytes})

	keyBuf := &bytes.Buffer{}
	pem.Encode(keyBuf, &pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(priv)})

	return tls.X509KeyPair(certBuf.Bytes(), keyBuf.Bytes())
}
