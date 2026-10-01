package webpush

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	webpushlib "github.com/SherClockHolmes/webpush-go"
)

var errInvalidSubscription = errors.New("endpoint, p256dh and auth are required")

// VAPID holds the application server key pair used to authenticate pushes.
type VAPID struct {
	PublicKey  string `json:"publicKey"`
	PrivateKey string `json:"privateKey"`
	Subject    string `json:"subject"`
}

// LoadOrGenerateVAPID returns keys from env, an existing file, or a newly
// generated pair written to path (0600).
func LoadOrGenerateVAPID(path, publicEnv, privateEnv, subjectEnv string) (*VAPID, error) {
	subject := strings.TrimSpace(subjectEnv)
	if subject == "" {
		subject = "mailto:admin@localhost"
	}
	pub := strings.TrimSpace(publicEnv)
	priv := strings.TrimSpace(privateEnv)
	if pub != "" && priv != "" {
		return &VAPID{PublicKey: pub, PrivateKey: priv, Subject: subject}, nil
	}
	if pub != "" || priv != "" {
		return nil, fmt.Errorf("both ROUNDPEN_VAPID_PUBLIC_KEY and ROUNDPEN_VAPID_PRIVATE_KEY must be set together")
	}

	if path != "" {
		if raw, err := os.ReadFile(path); err == nil {
			var v VAPID
			if err := json.Unmarshal(raw, &v); err != nil {
				return nil, fmt.Errorf("read vapid file: %w", err)
			}
			if strings.TrimSpace(v.PublicKey) == "" || strings.TrimSpace(v.PrivateKey) == "" {
				return nil, fmt.Errorf("vapid file %s missing keys", path)
			}
			if strings.TrimSpace(v.Subject) == "" {
				v.Subject = subject
			}
			return &v, nil
		} else if !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
	}

	privKey, pubKey, err := webpushlib.GenerateVAPIDKeys()
	if err != nil {
		return nil, fmt.Errorf("generate vapid keys: %w", err)
	}
	v := &VAPID{PublicKey: pubKey, PrivateKey: privKey, Subject: subject}
	if path != "" {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return nil, err
		}
		raw, err := json.MarshalIndent(v, "", "  ")
		if err != nil {
			return nil, err
		}
		if err := os.WriteFile(path, raw, 0o600); err != nil {
			return nil, err
		}
	}
	return v, nil
}
