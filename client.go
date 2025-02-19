package main

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"log"
	"net"
	"os"
	"time"

	"github.com/TwinProduction/go-color"
)

// These test keys were generated with the following program, available in the
// crypto/tls directory:
//
//	go run generate_cert.go -ecdsa-curve P256 -host 127.0.0.1 -allowDC
//
var delegatorCertPEMP256 = `-----BEGIN CERTIFICATE-----
MIIBgDCCASWgAwIBAgIRAKHVtdPqHtn9cjVHW94hM/gwCgYIKoZIzj0EAwIwEjEQ
MA4GA1UEChMHQWNtZSBDbzAeFw0yMTAzMTYyMTEzNThaFw0yMjAzMTYyMTEzNTha
MBIxEDAOBgNVBAoTB0FjbWUgQ28wWTATBgcqhkjOPQIBBggqhkjOPQMBBwNCAATo
iWgTin1LZO5Ncqz7lV+G6rmpFEJznHcLgFuQUdLKEO2sBh5gUd9s+4S9SpOUziZp
p1CK+A1yziNpRAXh0LZho1wwWjAOBgNVHQ8BAf8EBAMCB4AwEwYDVR0lBAwwCgYI
KwYBBQUHAwEwDAYDVR0TAQH/BAIwADAPBgkrBgEEAYLaSywEAgUAMBQGA1UdEQQN
MAuCCWxvY2FsaG9zdDAKBggqhkjOPQQDAgNJADBGAiEA3g74ed4oORh4NRXCESrd
EjqWLR3aSV/hn6ozgpLSbOsCIQD7/DFIiPu+mmFrDMRiM6dBQDteo8ou2goEhQWa
9Lq5SQ==
-----END CERTIFICATE-----
`

var delegatorKeyPEMP256 = `-----BEGIN EC PRIVATE KEY-----
MIGHAgEAMBMGByqGSM49AgEGCCqGSM49AwEHBG0wawIBAQQga+i6tUZxZC1WRj/c
wGYkTQyxBueWzjK7XsOm9kdZuwChRANCAAToiWgTin1LZO5Ncqz7lV+G6rmpFEJz
nHcLgFuQUdLKEO2sBh5gUd9s+4S9SpOUziZpp1CK+A1yziNpRAXh0LZh
-----END EC PRIVATE KEY-----
`

const (
	// In the absence of an application profile standard specifying otherwise,
	// the maximum validity period is set to 7 days.
	testDcMaxTTLSeconds = 60 * 60 * 24 * 7
	testDcMaxTTL        = time.Duration(testDcMaxTTLSeconds * time.Second)
)

func initServer() *tls.Config {
	// The delegation P256 certificate.
	dcCertP256 := new(tls.Certificate)
	var err error
	*dcCertP256, err = tls.X509KeyPair([]byte(delegatorCertPEMP256), []byte(delegatorKeyPEMP256))
	if err != nil {
		panic(err)
	}

	dcCertP256.Leaf, err = x509.ParseCertificate(dcCertP256.Certificate[0])
	if err != nil {
		panic(err)
	}

	cfg := &tls.Config{
		MinVersion:                 tls.VersionTLS10,
		MaxVersion:                 tls.VersionTLS13,
		InsecureSkipVerify:         true,                  // I'm JUST setting this for this test because the root and the leas are the same
		SupportDelegatedCredential: true,                  // for client auth, the server supports delegated credentials
		ClientAuth:                 tls.RequestClientCert, // for client auth
	}

	// The root certificates for the peer: this are invalid so DO NOT REUSE.
	cfg.RootCAs = x509.NewCertPool()

	dcRoot, err := x509.ParseCertificate(dcCertP256.Certificate[0])
	if err != nil {
		panic(err)
	}
	cfg.RootCAs.AddCert(dcRoot)

	cfg.Certificates = make([]tls.Certificate, 1)
	cfg.Certificates[0] = *dcCertP256

	return cfg
}

func initClient() *tls.Config {
	// The delegation P256 certificate.
	dcCertP256 := new(tls.Certificate)
	var err error
	*dcCertP256, err = tls.X509KeyPair([]byte(delegatorCertPEMP256), []byte(delegatorKeyPEMP256))
	if err != nil {
		panic(err)
	}

	dcCertP256.Leaf, err = x509.ParseCertificate(dcCertP256.Certificate[0])
	if err != nil {
		panic(err)
	}

	cfg := &tls.Config{
		MinVersion:                 tls.VersionTLS10,
		MaxVersion:                 tls.VersionTLS13,
		InsecureSkipVerify:         true, // I'm JUST setting this for this test because the root and the leaf are the same
		SupportDelegatedCredential: false,
	}

	// The root certificates for the peer: this are invalid so DO NOT REUSE.
	cfg.RootCAs = x509.NewCertPool()

	dcRoot, err := x509.ParseCertificate(dcCertP256.Certificate[0])
	if err != nil {
		panic(err)
	}
	cfg.RootCAs.AddCert(dcRoot)

	cfg.Certificates = make([]tls.Certificate, 1)
	cfg.Certificates[0] = *dcCertP256

	maxTTL, _ := time.ParseDuration("24h")
	validTime := maxTTL + time.Now().Sub(dcCertP256.Leaf.NotBefore)
	dc, priv, err := tls.NewDelegatedCredential(dcCertP256, tls.Ed25519, validTime, true)
	if err != nil {
		panic(err)
	}

	dcPair := tls.DelegatedCredentialPair{dc, priv}
	cfg.Certificates[0].DelegatedCredentials = make([]tls.DelegatedCredentialPair, 1)
	cfg.Certificates[0].DelegatedCredentials[0] = dcPair

	if tlsKeyLogFile := os.Getenv("SSLKEYLOGFILE"); tlsKeyLogFile != "" {
		kw, err := os.OpenFile(tlsKeyLogFile, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0600)
		if err != nil {
			log.Printf("Cannot open key log file: %s\n", err)
		}
		cfg.KeyLogWriter = kw
	}

	return cfg
}

func newLocalListener() net.Listener {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		ln, err = net.Listen("tcp6", "[::1]:0")
	}
	if err != nil {
		log.Fatal(err)
	}
	return ln
}

type timingInfo struct {
	serverTimingInfo tls.CFEventTLS13ServerHandshakeTimingInfo
	clientTimingInfo tls.CFEventTLS13ClientHandshakeTimingInfo
}

func (ti *timingInfo) eventHandler(event tls.CFEvent) {
	switch e := event.(type) {
	case tls.CFEventTLS13ServerHandshakeTimingInfo:
		ti.serverTimingInfo = e
	case tls.CFEventTLS13ClientHandshakeTimingInfo:
		ti.clientTimingInfo = e
	}
}

func testConnWithDC(clientMsg, serverMsg string, clientConfig, serverConfig *tls.Config, peer string) (timingState timingInfo, isDC bool, err error) {
	clientConfig.CFEventHandler = timingState.eventHandler
	serverConfig.CFEventHandler = timingState.eventHandler

	ln := newLocalListener()
	defer ln.Close()

	serverCh := make(chan *tls.Conn, 1)
	var serverErr error
	go func() {
		serverConn, err := ln.Accept()
		if err != nil {
			serverErr = err
			serverCh <- nil
			return
		}
		server := tls.Server(serverConn, serverConfig)
		if err := server.Handshake(); err != nil {
			serverErr = fmt.Errorf("handshake error: %v", err)
			serverCh <- nil
			return
		}
		serverCh <- server
	}()

	client, err := tls.Dial("tcp", ln.Addr().String(), clientConfig)
	if err != nil {
		return timingState, false, err
	}
	defer client.Close()

	server := <-serverCh
	if server == nil {
		return timingState, false, serverErr
	}

	bufLen := len(clientMsg)
	if len(serverMsg) > len(clientMsg) {
		bufLen = len(serverMsg)
	}
	buf := make([]byte, bufLen)

	client.Write([]byte(clientMsg))
	n, err := server.Read(buf)
	if err != nil || n != len(clientMsg) || string(buf[:n]) != clientMsg {
		return timingState, false, fmt.Errorf("Server read = %d, buf= %q; want %d, %s", n, buf, len(clientMsg), clientMsg)
	}

	server.Write([]byte(serverMsg))
	n, err = client.Read(buf)
	if n != len(serverMsg) || err != nil || string(buf[:n]) != serverMsg {
		return timingState, false, fmt.Errorf("Client read = %d, %v, data %q; want %d, nil, %s", n, err, buf, len(serverMsg), serverMsg)
	}

	if peer == "server" {
		if server.ConnectionState().DidClientAuthentication == true {
			if server.ConnectionState().VerifiedDC == true {
				return timingState, true, nil
			}
		}
	}

	return timingState, false, nil
}

func main() {
	serverMsg := "hello, client"
	clientMsg := "hello, server"

	serverConfig := initServer()
	clientConfig := initClient()

	ts, dc, err := testConnWithDC(clientMsg, serverMsg, clientConfig, serverConfig, "server")

	log.Printf("Write Client Hello %v \n", ts.clientTimingInfo.WriteClientHello)
	log.Printf("Receive Client Hello %v \n", ts.serverTimingInfo.ProcessClientHello)
	log.Printf("Write Server Hello %v \n", ts.serverTimingInfo.WriteServerHello)
	log.Printf("Write Server Encrypted Extensions %v \n", ts.serverTimingInfo.WriteEncryptedExtensions)
	log.Printf("Write Server Certificate%v \n", ts.serverTimingInfo.WriteCertificate)
	log.Printf("Write Server CertificateVerify%v \n", ts.serverTimingInfo.WriteCertificateVerify)
	log.Printf("Write Server Finished%v \n", ts.serverTimingInfo.WriteServerFinished)
	log.Printf("Receive Server Encrypted Extensions %v \n", ts.clientTimingInfo.ReadEncryptedExtensions)
	log.Printf("Receive Server Certificate %v \n", ts.clientTimingInfo.ReadCertificate)
	log.Printf("Receive Server Certificate Verify %v \n", ts.clientTimingInfo.ReadCertificateVerify)
	log.Printf("Receive Server Finished %v \n", ts.clientTimingInfo.ReadServerFinished)
	log.Printf("Write Client Certificate %v \n", ts.clientTimingInfo.WriteCertificate)
	log.Printf("Write Client CertificateVerify %v \n", ts.clientTimingInfo.WriteCertificateVerify)
	log.Printf("Write Client Finished %v \n", ts.clientTimingInfo.WriteClientFinished)
	log.Printf("Receive Client Certificate %v \n", ts.serverTimingInfo.ReadCertificate)
	log.Printf("Receive Client Certificate Verify %v \n", ts.serverTimingInfo.ReadCertificateVerify)
	log.Printf("Receive Client Finished %v \n", ts.serverTimingInfo.ReadClientFinished)

	if err != nil {
		log.Println("")
		log.Println(color.Ize(color.Red, err.Error()))
	} else if !dc {
		log.Println("")
		log.Println(color.Ize(color.Red, "Failure while trying to use tls 1.3  mutual auth with dcs"))
	} else {
		log.Println("")
		log.Println(color.Ize(color.Green, "Success using tls 1.3 (ecdh, sig: ed25519) mutual auth with dc"))
	}
}
