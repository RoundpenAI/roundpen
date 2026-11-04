package imconnect

import (
	"path/filepath"
	"testing"
)

func TestEnvDefaults_IMDataDirUnderDataRoot(t *testing.T) {
	t.Setenv("ROUNDPEN_IM_LANG", "")
	t.Setenv("ROUNDPEN_IM_PROVIDER", "")
	t.Setenv("ROUNDPEN_IM_DATA_DIR", "")
	t.Setenv("ROUNDPEN_DATA_ROOT", "/var/lib/roundpen")

	lang, dataDir, provider := EnvDefaults()
	if lang != "zh" {
		t.Fatalf("lang=%q", lang)
	}
	if provider != "sysadmin" {
		t.Fatalf("provider=%q", provider)
	}
	want := filepath.Join("/var/lib/roundpen", "im")
	if dataDir != want {
		t.Fatalf("dataDir=%q want %q", dataDir, want)
	}
}

func TestEnvDefaults_ExplicitIMDataDirWins(t *testing.T) {
	t.Setenv("ROUNDPEN_IM_DATA_DIR", "/custom/im")
	t.Setenv("ROUNDPEN_DATA_ROOT", "/var/lib/roundpen")
	_, dataDir, _ := EnvDefaults()
	if dataDir != "/custom/im" {
		t.Fatalf("dataDir=%q", dataDir)
	}
}
