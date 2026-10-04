package mtls_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/trb1maker/subscriptions/pkg/mtls"
)

func TestServerConfigIncomplete(t *testing.T) {
	t.Parallel()

	_, err := mtls.ServerConfig(mtls.Files{})
	require.Error(t, err)
}

func TestClientConfigRequiresServerName(t *testing.T) {
	t.Parallel()

	_, err := mtls.ClientConfig(mtls.Files{CertFile: "cert", KeyFile: "key", CAFile: "ca"}, "")
	require.Error(t, err)
}

func TestServerConfigMissingFiles(t *testing.T) {
	t.Parallel()

	_, err := mtls.ServerConfig(mtls.Files{CertFile: "missing.crt", KeyFile: "missing.key", CAFile: "missing.crt"})
	require.Error(t, err)
}

func TestServerConfigRejectsCA(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	ca := filepath.Join(dir, "ca.crt")
	require.NoError(t, os.WriteFile(ca, []byte("not a certificate"), 0o600))

	_, err := mtls.ServerConfig(mtls.Files{CertFile: ca, KeyFile: ca, CAFile: ca})
	require.Error(t, err)
}
