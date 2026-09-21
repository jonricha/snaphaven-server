package main

import (
	"encoding/hex"
	"encoding/json"
	"math/big"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type PairedDevice struct {
	SerialNumber string     `json:"serial_number"` // Hex format of certificate serial number
	Fingerprint  string     `json:"fingerprint"`   // SHA256 hex fingerprint of client cert
	Name         string     `json:"name"`          // Friendly name or custom user label
	DeviceModel  string     `json:"device_model"`  // e.g. "Pixel 8 Pro", "Samsung SM-S918U"
	Prefix       string     `json:"prefix"`        // Sync folder prefix if reported (e.g. "john_phone")
	PairedAt     time.Time  `json:"paired_at"`
	LastSeenAt   time.Time  `json:"last_seen_at"`
	Revoked      bool       `json:"revoked"`
	RevokedAt    *time.Time `json:"revoked_at,omitempty"`
}

type DeviceManager struct {
	mu       sync.RWMutex
	filePath string
	devices  map[string]*PairedDevice // Keyed by SerialNumber (hex string)
}

func NewDeviceManager(configDirPath string) (*DeviceManager, error) {
	if configDirPath == "" {
		cfgPath, err := GetDefaultConfigPath()
		if err == nil {
			configDirPath = filepath.Dir(cfgPath)
		} else {
			configDirPath = "."
		}
	}

	filePath := filepath.Join(configDirPath, "paired_devices.json")
	dm := &DeviceManager{
		filePath: filePath,
		devices:  make(map[string]*PairedDevice),
	}

	_ = dm.load()
	return dm, nil
}

func (dm *DeviceManager) load() error {
	dm.mu.Lock()
	defer dm.mu.Unlock()

	data, err := os.ReadFile(dm.filePath)
	if err != nil {
		return err
	}

	var list []*PairedDevice
	if err := json.Unmarshal(data, &list); err != nil {
		return err
	}

	dm.devices = make(map[string]*PairedDevice)
	for _, dev := range list {
		if dev != nil && dev.SerialNumber != "" {
			dm.devices[dev.SerialNumber] = dev
		}
	}
	return nil
}

func (dm *DeviceManager) saveLocked() error {
	list := make([]*PairedDevice, 0, len(dm.devices))
	for _, dev := range dm.devices {
		list = append(list, dev)
	}

	data, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return err
	}

	_ = os.MkdirAll(filepath.Dir(dm.filePath), 0755)
	return os.WriteFile(dm.filePath, data, 0644)
}

// NormalizeSerial converts a big.Int serial number to a lowercase hex string
func NormalizeSerial(serial *big.Int) string {
	if serial == nil {
		return ""
	}
	return hex.EncodeToString(serial.Bytes())
}

// RegisterDevice registers or updates a paired device
func (dm *DeviceManager) RegisterDevice(serialHex, fingerprint, name, model string) *PairedDevice {
	dm.mu.Lock()
	defer dm.mu.Unlock()

	key := serialHex
	now := time.Now().UTC()

	dev, exists := dm.devices[key]
	if !exists {
		devName := name
		if devName == "" {
			if model != "" {
				devName = model
			} else {
				devName = "Client (" + truncate(serialHex, 8) + ")"
			}
		}

		dev = &PairedDevice{
			SerialNumber: serialHex,
			Fingerprint:  fingerprint,
			Name:         devName,
			DeviceModel:  model,
			PairedAt:     now,
			LastSeenAt:   now,
			Revoked:      false,
		}
		dm.devices[key] = dev
	} else {
		if name != "" {
			dev.Name = name
		}
		if model != "" {
			dev.DeviceModel = model
		}
		if fingerprint != "" {
			dev.Fingerprint = fingerprint
		}
		dev.LastSeenAt = now
	}

	_ = dm.saveLocked()
	return dev
}

// TouchOrAutoRegister records client connection activity. If the client certificate was paired
// prior to this feature, it auto-registers it so it can be viewed and revoked.
func (dm *DeviceManager) TouchOrAutoRegister(serialHex, fingerprint string) {
	dm.mu.Lock()
	defer dm.mu.Unlock()

	now := time.Now().UTC()
	dev, exists := dm.devices[serialHex]
	if !exists {
		dev = &PairedDevice{
			SerialNumber: serialHex,
			Fingerprint:  fingerprint,
			Name:         "Client (" + truncate(serialHex, 8) + ")",
			DeviceModel:  "Unknown Device",
			PairedAt:     now,
			LastSeenAt:   now,
			Revoked:      false,
		}
		dm.devices[serialHex] = dev
	} else {
		dev.LastSeenAt = now
		if dev.Fingerprint == "" && fingerprint != "" {
			dev.Fingerprint = fingerprint
		}
	}
	_ = dm.saveLocked()
}

// IsRevoked checks whether a certificate serial is revoked
func (dm *DeviceManager) IsRevoked(serialHex string) bool {
	dm.mu.RLock()
	defer dm.mu.RUnlock()

	dev, exists := dm.devices[serialHex]
	if !exists {
		return false
	}
	return dev.Revoked
}

// RevokeDevice revokes a device by certificate serial number
func (dm *DeviceManager) RevokeDevice(serialHex string) bool {
	dm.mu.Lock()
	defer dm.mu.Unlock()

	dev, exists := dm.devices[serialHex]
	if !exists {
		return false
	}

	now := time.Now().UTC()
	dev.Revoked = true
	dev.RevokedAt = &now
	_ = dm.saveLocked()
	return true
}

// UnrevokeDevice restores a previously revoked device
func (dm *DeviceManager) UnrevokeDevice(serialHex string) bool {
	dm.mu.Lock()
	defer dm.mu.Unlock()

	dev, exists := dm.devices[serialHex]
	if !exists {
		return false
	}

	dev.Revoked = false
	dev.RevokedAt = nil
	_ = dm.saveLocked()
	return true
}

// DeleteDevice deletes a device from registry completely
func (dm *DeviceManager) DeleteDevice(serialHex string) bool {
	dm.mu.Lock()
	defer dm.mu.Unlock()

	if _, exists := dm.devices[serialHex]; !exists {
		return false
	}

	delete(dm.devices, serialHex)
	_ = dm.saveLocked()
	return true
}

// RenameDevice renames a device
func (dm *DeviceManager) RenameDevice(serialHex, newName string) bool {
	dm.mu.Lock()
	defer dm.mu.Unlock()

	dev, exists := dm.devices[serialHex]
	if !exists {
		return false
	}

	dev.Name = newName
	_ = dm.saveLocked()
	return true
}

// GetAllDevices returns a snapshot copy of all paired devices
func (dm *DeviceManager) GetAllDevices() []*PairedDevice {
	dm.mu.RLock()
	defer dm.mu.RUnlock()

	res := make([]*PairedDevice, 0, len(dm.devices))
	for _, dev := range dm.devices {
		cp := *dev
		res = append(res, &cp)
	}
	return res
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen]
}
