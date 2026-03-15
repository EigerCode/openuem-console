package softwaredeployment

// AssignmentInfo holds a resolved assignment for manifest building.
type AssignmentInfo struct {
	PackageName    string
	AssignmentType string // managed_install, managed_uninstall, optional_install, managed_update
}

// CatalogPackageInfo is the data needed from the model to build a catalog entry.
type CatalogPackageInfo struct {
	Name                   string
	Version                string
	DisplayName            string
	Description            string
	Category               string
	Developer              string
	InstallerPath          string
	InstallerType          string
	ChecksumSHA256         string
	SizeBytes              int64
	MinOSVersion           string
	MaxOSVersion           string
	RestartAction          string
	UnattendedInstall      bool
	UnattendedUninstall    bool
	IconName               string
	RepoType               string // "global" or "tenant"
	InstallsItems          string // JSON array for Munki installs detection
	Receipts               string // JSON array for Munki receipts detection
	BlockingApps           string // JSON array of blocking applications
	SupportedArchitectures string // JSON array, e.g. ["x86_64", "arm64"]
	PkginfoData            string // Full pkgsinfo plist XML (single source of truth)
	PreInstallScript       string // Script to run before installation
	PostInstallScript      string // Script to run after installation
}
