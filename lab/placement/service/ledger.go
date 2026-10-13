package service

import (
	"encoding/json"
	"errors"
	"os"

	"github.com/julesdavidou/projet_secu_cloud/pkg/placement"
)

func restoreLedger(path string, z *placement.Zone) error {
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if len(raw) > 32*1024*1024 {
		return errors.New("ledger exceeds recovery size limit")
	}
	var c placement.Checkpoint
	if err = json.Unmarshal(raw, &c); err != nil {
		return err
	}
	return z.Restore(c)
}
func saveLedger(path string, c placement.Checkpoint) error {
	raw, err := json.Marshal(c)
	if err != nil {
		return err
	}
	if len(raw) > 32*1024*1024 {
		return errors.New("ledger exceeds recovery size limit")
	}
	f, err := os.OpenFile(path+".tmp", os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	if _, err = f.Write(raw); err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if err = os.Rename(path+".tmp", path); err != nil {
		return err
	}
	dir, err := os.Open(env("LEDGER_DIR", "/data"))
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}
