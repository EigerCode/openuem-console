package softwaredeployment

// AssignmentInfo holds a resolved assignment for manifest building.
type AssignmentInfo struct {
	PackageName    string
	AssignmentType string // managed_install, managed_uninstall, optional_install, managed_update
}

// CatalogPackageInfo is the data needed from the model to build a catalog entry.
type CatalogPackageInfo struct {
	Name                string
	Version             string
	DisplayName         string
	Description         string
	Category            string
	Developer           string
	InstallerPath       string
	InstallerType       string
	ChecksumSHA256      string
	SizeBytes           int64
	MinOSVersion        string
	MaxOSVersion        string
	RestartAction       string
	UnattendedInstall   bool
	UnattendedUninstall bool
	RepoType            string // "global" or "tenant"
}
