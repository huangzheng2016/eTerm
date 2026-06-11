package sync

import (
	"encoding/base64"
	"encoding/json"
	"time"

	"github.com/huangzheng2016/eTerm/internal/db"
	"github.com/huangzheng2016/eTerm/internal/security"
	"gorm.io/gorm"
)

// CollectDirty gathers locally modified records that need to be pushed.
// Collects records where updated_at > lastSyncTime (or all if lastSyncTime is zero).
func CollectDirty(database *gorm.DB, mk *security.MasterKeyManager, passphrase, deviceID string, lastSyncTime time.Time) ([]SyncRecord, error) {
	var records []SyncRecord

	// SSHKeys (skip file mode)
	var keys []db.SSHKey
	if err := database.Unscoped().Where("(storage_mode = ? OR storage_mode = ?) AND updated_at > ?", "database", "", lastSyncTime).Find(&keys).Error; err != nil {
		return nil, err
	}
	for _, k := range keys {
		r, err := buildKeyRecord(k, mk, passphrase, deviceID)
		if err != nil {
			continue
		}
		records = append(records, r)
	}

	// Hosts
	var hosts []db.Host
	if err := database.Unscoped().Where("updated_at > ?", lastSyncTime).Find(&hosts).Error; err != nil {
		return nil, err
	}
	for _, h := range hosts {
		r, err := buildHostRecord(h, database, mk, passphrase, deviceID)
		if err != nil {
			continue
		}
		records = append(records, r)
	}

	// PortForwards
	var fwds []db.PortForward
	if err := database.Unscoped().Where("updated_at > ?", lastSyncTime).Find(&fwds).Error; err != nil {
		return nil, err
	}
	for _, f := range fwds {
		r, err := buildFwdRecord(f, database, passphrase, deviceID)
		if err != nil {
			continue
		}
		records = append(records, r)
	}

	// Snippets
	var snippets []db.Snippet
	if err := database.Unscoped().Where("updated_at > ?", lastSyncTime).Find(&snippets).Error; err != nil {
		return nil, err
	}
	for _, s := range snippets {
		r, err := buildSnippetRecord(s, passphrase, deviceID)
		if err != nil {
			continue
		}
		records = append(records, r)
	}

	return records, nil
}

func decryptField(encrypted string, mk *security.MasterKeyManager) string {
	if encrypted == "" {
		return ""
	}
	k := mk.GetKey()
	if k == nil {
		return ""
	}
	defer k.Clear()
	plain, err := security.Decrypt(encrypted, k.Bytes())
	if err != nil {
		return ""
	}
	return string(plain)
}

func encryptPayload(dto interface{}, passphrase string) (string, error) {
	data, err := json.Marshal(dto)
	if err != nil {
		return "", err
	}
	enc, err := AgeEncrypt(data, passphrase)
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(enc), nil
}

func buildKeyRecord(k db.SSHKey, mk *security.MasterKeyManager, passphrase, deviceID string) (SyncRecord, error) {
	if k.SyncDel {
		return SyncRecord{
			SyncID:    k.SyncID,
			Type:      TypeSSHKey,
			Deleted:   true,
			DeviceID:  deviceID,
			UpdatedAt: k.UpdatedAt,
		}, nil
	}
	dto := SSHKeyDTO{
		SyncID:          k.SyncID,
		Name:            k.Name,
		Type:            k.Type,
		PrivateKey:      decryptField(k.PrivateKeyData, mk),
		PublicKey:       k.PublicKeyData,
		Fingerprint:     k.Fingerprint,
		Bits:            k.Bits,
		Passphrase:      decryptField(k.Passphrase, mk),
		CertificatePath: k.CertificatePath,
	}
	payload, err := encryptPayload(dto, passphrase)
	if err != nil {
		return SyncRecord{}, err
	}
	return SyncRecord{
		SyncID:    k.SyncID,
		Type:      TypeSSHKey,
		Deleted:   k.SyncDel,
		DeviceID:  deviceID,
		Payload:   payload,
		UpdatedAt: k.UpdatedAt,
	}, nil
}

func buildHostRecord(h db.Host, database *gorm.DB, mk *security.MasterKeyManager, passphrase, deviceID string) (SyncRecord, error) {
	if h.SyncDel {
		return SyncRecord{
			SyncID:    h.SyncID,
			Type:      TypeHost,
			Deleted:   true,
			DeviceID:  deviceID,
			UpdatedAt: h.UpdatedAt,
		}, nil
	}
	// Resolve FK sync IDs
	var keySyncID, jumpSyncID string
	if h.KeyID != nil {
		var key db.SSHKey
		if database.Select("sync_id").First(&key, *h.KeyID).Error == nil {
			keySyncID = key.SyncID
		}
	}
	if h.JumpHostID != nil {
		var jump db.Host
		if database.Select("sync_id").First(&jump, *h.JumpHostID).Error == nil {
			jumpSyncID = jump.SyncID
		}
	}

	dto := HostDTO{
		SyncID:          h.SyncID,
		Alias:           h.Alias,
		Hostname:        h.Hostname,
		Port:            h.Port,
		Username:        h.Username,
		AuthMethod:      h.AuthMethod,
		Password:        decryptField(h.Password, mk),
		KeySyncID:       keySyncID,
		Passphrase:      decryptField(h.Passphrase, mk),
		JumpSyncID:      jumpSyncID,
		Tags:            h.Tags,
		Description:     h.Description,
		Group:           h.Group,
		ProxyType:       h.ProxyType,
		ProxyHost:       h.ProxyHost,
		ProxyPort:       h.ProxyPort,
		ProxyUser:       h.ProxyUser,
		ProxyPassword:   decryptField(h.ProxyPassword, mk),
		GSSAPISource:    h.GSSAPISource,
		GSSAPIKeytab:    h.GSSAPIKeytab,
		KrbPrincipal:    h.KrbPrincipal,
		ProxyCommand:    h.ProxyCommand,
		ForwardAgent:    h.ForwardAgent,
		RemoteCommand:   h.RemoteCommand,
		ExtraSSHOptions: h.ExtraSSHOptions,
	}
	payload, err := encryptPayload(dto, passphrase)
	if err != nil {
		return SyncRecord{}, err
	}
	return SyncRecord{
		SyncID:    h.SyncID,
		Type:      TypeHost,
		Deleted:   h.SyncDel,
		DeviceID:  deviceID,
		Payload:   payload,
		UpdatedAt: h.UpdatedAt,
	}, nil
}

func buildFwdRecord(f db.PortForward, database *gorm.DB, passphrase, deviceID string) (SyncRecord, error) {
	if f.SyncDel {
		return SyncRecord{
			SyncID:    f.SyncID,
			Type:      TypePortFwd,
			Deleted:   true,
			DeviceID:  deviceID,
			UpdatedAt: f.UpdatedAt,
		}, nil
	}
	var hostSyncID string
	if f.HostID > 0 {
		var host db.Host
		if database.Select("sync_id").First(&host, f.HostID).Error == nil {
			hostSyncID = host.SyncID
		}
	}
	dto := PortForwardDTO{
		SyncID:     f.SyncID,
		HostSyncID: hostSyncID,
		LocalPort:  f.LocalPort,
		RemoteHost: f.RemoteHost,
		RemotePort: f.RemotePort,
		Direction:  f.Direction,
	}
	payload, err := encryptPayload(dto, passphrase)
	if err != nil {
		return SyncRecord{}, err
	}
	return SyncRecord{
		SyncID:    f.SyncID,
		Type:      TypePortFwd,
		Deleted:   f.SyncDel,
		DeviceID:  deviceID,
		Payload:   payload,
		UpdatedAt: f.UpdatedAt,
	}, nil
}

func buildSnippetRecord(s db.Snippet, passphrase, deviceID string) (SyncRecord, error) {
	if s.SyncDel {
		return SyncRecord{
			SyncID:    s.SyncID,
			Type:      TypeSnippet,
			Deleted:   true,
			DeviceID:  deviceID,
			UpdatedAt: s.UpdatedAt,
		}, nil
	}
	dto := SnippetDTO{
		SyncID:  s.SyncID,
		Name:    s.Name,
		Command: s.Command,
		Tags:    s.Tags,
	}
	payload, err := encryptPayload(dto, passphrase)
	if err != nil {
		return SyncRecord{}, err
	}
	return SyncRecord{
		SyncID:    s.SyncID,
		Type:      TypeSnippet,
		Deleted:   s.SyncDel,
		DeviceID:  deviceID,
		Payload:   payload,
		UpdatedAt: s.UpdatedAt,
	}, nil
}
