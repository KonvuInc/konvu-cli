package auth

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

func SaveCredentials(path string, tokenData map[string]any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}

	creds := map[string]any{
		"access_token": tokenData["access_token"],
	}

	if expiresIn, ok := tokenData["expires_in"].(float64); ok {
		creds["expires_at"] = int(time.Now().Unix()) + int(expiresIn)
	}

	data, err := json.MarshalIndent(creds, "", "  ")
	if err != nil {
		return err
	}

	fd, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	defer fd.Close()

	_, err = fd.Write(data)
	return err
}
