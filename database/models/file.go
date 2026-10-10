package models

// StoredFile is a persistent file object kept in the primary database.
// Namespace separates test/runtime installations that share a database, while
// Scope and Owner identify the logical directory (theme, plugin, or plugin
// data) that owns the file.
type StoredFile struct {
	ID        string `gorm:"primaryKey;size:64"`
	Namespace string `gorm:"size:191;index"`
	Scope     string `gorm:"size:32;index"`
	Owner     string `gorm:"size:191;index"`
	Path      string `gorm:"size:512"`
	Mode      uint32
	Data      []byte
}

// PluginState stores plugin enablement and permission approvals in SQL.
type PluginState struct {
	Namespace               string `gorm:"primaryKey;size:512"`
	Short                   string `gorm:"primaryKey;size:191"`
	Enabled                 bool
	ApprovedPermissionsHash string `gorm:"size:128"`
	LastError               string `gorm:"type:text"`
	UpdatedAt               int64
}
