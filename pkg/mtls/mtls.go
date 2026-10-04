package mtls

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"os"
)

// Files — пути к сертификату, ключу и CA.
type Files struct {
	CertFile string
	KeyFile  string
	CAFile   string
}

// ServerConfig требует клиентский сертификат, выпущенный тем же CA.
func ServerConfig(files Files) (*tls.Config, error) {
	cert, pool, err := load(files)
	if err != nil {
		return nil, err
	}

	return &tls.Config{
		MinVersion:   tls.VersionTLS12,
		Certificates: []tls.Certificate{cert},
		ClientCAs:    pool,
		ClientAuth:   tls.RequireAndVerifyClientCert,
	}, nil
}

// ClientConfig предъявляет клиентский сертификат и проверяет имя сервера по CA.
func ClientConfig(files Files, serverName string) (*tls.Config, error) {
	if serverName == "" {
		return nil, errors.New("empty server name")
	}

	cert, pool, err := load(files)
	if err != nil {
		return nil, err
	}

	return &tls.Config{
		MinVersion:   tls.VersionTLS12,
		Certificates: []tls.Certificate{cert},
		RootCAs:      pool,
		ServerName:   serverName,
	}, nil
}

func load(files Files) (tls.Certificate, *x509.CertPool, error) {
	if files.CertFile == "" || files.KeyFile == "" || files.CAFile == "" {
		return tls.Certificate{}, nil, errors.New("incomplete tls files")
	}

	cert, err := tls.LoadX509KeyPair(files.CertFile, files.KeyFile)
	if err != nil {
		return tls.Certificate{}, nil, fmt.Errorf("load tls key pair: %w", err)
	}

	pemBytes, err := os.ReadFile(files.CAFile)
	if err != nil {
		return tls.Certificate{}, nil, fmt.Errorf("read tls ca: %w", err)
	}

	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(pemBytes) {
		return tls.Certificate{}, nil, errors.New("parse tls ca")
	}

	return cert, pool, nil
}
