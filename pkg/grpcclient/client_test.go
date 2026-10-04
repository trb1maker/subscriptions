package grpcclient_test

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"io"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/trb1maker/subscriptions/pkg/caller"
	"github.com/trb1maker/subscriptions/pkg/grpcclient"
	"github.com/trb1maker/subscriptions/pkg/grpcserver"
	"github.com/trb1maker/subscriptions/pkg/logger"
	"github.com/trb1maker/subscriptions/pkg/mtls"
)

func TestDialRequiresTLSConfig(t *testing.T) {
	t.Parallel()

	_, err := grpcclient.Dial(context.Background(), "127.0.0.1:1", nil)
	require.Error(t, err)
}

func TestDialRejectsCanceledContext(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := grpcclient.Dial(ctx, "127.0.0.1:1", &tls.Config{MinVersion: tls.VersionTLS12})
	require.Error(t, err)
}

func TestMTLS(t *testing.T) {
	t.Parallel()

	serverFiles, clientFiles := writeCerts(t)
	serverCfg, err := mtls.ServerConfig(serverFiles)
	require.NoError(t, err)
	clientCfg, err := mtls.ClientConfig(clientFiles, "localhost")
	require.NoError(t, err)

	log, err := logger.New(io.Discard, "error")
	require.NoError(t, err)

	server, err := grpcserver.NewTLS(log, serverCfg)
	require.NoError(t, err)

	seen := make(chan seenCall, 1)
	server.RegisterService(&grpc.ServiceDesc{
		ServiceName: "test.Probe",
		HandlerType: (*probeService)(nil),
		Methods: []grpc.MethodDesc{{
			MethodName: "Ping",
			Handler:    pingHandler,
		}},
	}, probe{fn: func(ctx context.Context) {
		got, _ := caller.FromContext(ctx)
		seen <- seenCall{
			subject:   got.SubjectID,
			kind:      got.Kind,
			roles:     append([]string(nil), got.Roles...),
			requestID: logger.RequestID(ctx),
		}
	}})

	addr := freeAddr(t)
	ctx, cancel := context.WithCancel(context.Background())
	serveErr := make(chan error, 1)
	go func() {
		serveErr <- grpcserver.Serve(ctx, log, server, addr, time.Second)
	}()
	t.Cleanup(func() {
		cancel()
		require.NoError(t, <-serveErr)
	})

	require.Eventually(t, func() bool {
		conn, dialErr := net.DialTimeout("tcp", addr, 10*time.Millisecond)
		if dialErr != nil {
			return false
		}

		require.NoError(t, conn.Close())

		return true
	}, time.Second, 10*time.Millisecond)

	conn, err := grpcclient.Dial(context.Background(), addr, clientCfg)
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, conn.Close())
	})

	callCtx, callCancel := context.WithTimeout(context.Background(), time.Second)
	defer callCancel()
	callCtx = logger.WithRequestID(callCtx, "req-9")
	callCtx = caller.NewContext(callCtx, caller.Caller{SubjectID: "user-1", Kind: "user", Roles: []string{"admin"}})

	err = conn.Invoke(callCtx, "/test.Probe/Ping", &emptypb.Empty{}, &emptypb.Empty{})
	require.NoError(t, err)

	select {
	case got := <-seen:
		require.Equal(t, "user-1", got.subject)
		require.Equal(t, "user", got.kind)
		require.Equal(t, []string{"admin"}, got.roles)
		require.Equal(t, "req-9", got.requestID)
	case <-time.After(time.Second):
		t.Fatal("ping was not handled")
	}

	naked := clientCfg.Clone()
	naked.Certificates = nil
	bad, err := grpcclient.Dial(context.Background(), addr, naked)
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, bad.Close())
	})

	badCtx, badCancel := context.WithTimeout(context.Background(), time.Second)
	defer badCancel()
	err = bad.Invoke(badCtx, "/test.Probe/Ping", &emptypb.Empty{}, &emptypb.Empty{})
	require.Error(t, err)
}

type seenCall struct {
	subject   string
	kind      string
	roles     []string
	requestID string
}

type probeService interface {
	Ping(context.Context, *emptypb.Empty) (*emptypb.Empty, error)
}

type probe struct {
	fn func(context.Context)
}

func (p probe) Ping(ctx context.Context, _ *emptypb.Empty) (*emptypb.Empty, error) {
	p.fn(ctx)

	return &emptypb.Empty{}, nil
}

//nolint:revive // сигнатуру метода задаёт gRPC
func pingHandler(srv any, ctx context.Context, dec func(any) error, interceptor grpc.UnaryServerInterceptor) (any, error) {
	in := new(emptypb.Empty)
	if err := dec(in); err != nil {
		return nil, err
	}

	if interceptor == nil {
		return srv.(probeService).Ping(ctx, in)
	}

	info := &grpc.UnaryServerInfo{Server: srv, FullMethod: "/test.Probe/Ping"}

	return interceptor(ctx, in, info, func(ctx context.Context, req any) (any, error) {
		return srv.(probeService).Ping(ctx, req.(*emptypb.Empty))
	})
}

func freeAddr(t *testing.T) string {
	t.Helper()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	addr := ln.Addr().String()
	require.NoError(t, ln.Close())

	return addr
}

func writeCerts(t *testing.T) (serverFiles, clientFiles mtls.Files) {
	t.Helper()

	caKey, caCert := newCA(t)
	serverCert, serverKey := issue(t, caKey, caCert, "localhost", true)
	clientCert, clientKey := issue(t, caKey, caCert, "gateway", false)

	dir := t.TempDir()
	caPath := writePEM(t, dir, "ca.crt", "CERTIFICATE", caCert.Raw)
	serverFiles = mtls.Files{
		CertFile: writePEM(t, dir, "auth.crt", "CERTIFICATE", serverCert),
		KeyFile:  writeKey(t, dir, "auth.key", serverKey),
		CAFile:   caPath,
	}
	clientFiles = mtls.Files{
		CertFile: writePEM(t, dir, "gateway.crt", "CERTIFICATE", clientCert),
		KeyFile:  writeKey(t, dir, "gateway.key", clientKey),
		CAFile:   caPath,
	}

	return serverFiles, clientFiles
}

func newCA(t *testing.T) (*ecdsa.PrivateKey, *x509.Certificate) {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	tmpl := &x509.Certificate{
		SerialNumber:          serial(t),
		Subject:               pkix.Name{CommonName: "subscriptions-dev-ca"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageCertSign,
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	require.NoError(t, err)
	cert, err := x509.ParseCertificate(der)
	require.NoError(t, err)

	return key, cert
}

func issue(t *testing.T, caKey *ecdsa.PrivateKey, caCert *x509.Certificate, name string, server bool) ([]byte, *ecdsa.PrivateKey) {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	usage := x509.ExtKeyUsageClientAuth
	var dns []string
	var ips []net.IP
	if server {
		usage = x509.ExtKeyUsageServerAuth
		dns = []string{"localhost", "auth"}
		ips = []net.IP{net.ParseIP("127.0.0.1")}
	}

	tmpl := &x509.Certificate{
		SerialNumber: serial(t),
		Subject:      pkix.Name{CommonName: name},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{usage},
		DNSNames:     dns,
		IPAddresses:  ips,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, caCert, &key.PublicKey, caKey)
	require.NoError(t, err)

	return der, key
}

func serial(t *testing.T) *big.Int {
	t.Helper()

	limit := new(big.Int).Lsh(big.NewInt(1), 64)
	n, err := rand.Int(rand.Reader, limit)
	require.NoError(t, err)

	return n
}

func writePEM(t *testing.T, dir, name, kind string, der []byte) string {
	t.Helper()

	path := filepath.Join(dir, name)
	require.NoError(t, os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: kind, Bytes: der}), 0o600))

	return path
}

func writeKey(t *testing.T, dir, name string, key *ecdsa.PrivateKey) string {
	t.Helper()

	der, err := x509.MarshalECPrivateKey(key)
	require.NoError(t, err)

	return writePEM(t, dir, name, "EC PRIVATE KEY", der)
}
