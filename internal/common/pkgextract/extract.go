package pkgextract

import (
	"archive/zip"
	"bytes"
	"compress/zlib"
	"debug/pe"
	"encoding/binary"
	"encoding/xml"
	"fmt"
	"html"
	"io"
	"path/filepath"
	"strings"
	"unicode/utf16"

	"gopkg.in/yaml.v3"
)

// PackageMetadata holds the extracted metadata from an installer file.
type PackageMetadata struct {
	Name            string `json:"name"`
	DisplayName     string `json:"display_name"`
	Version         string `json:"version"`
	Platform        string `json:"platform"`
	InstallerType   string `json:"installer_type"`
	Developer       string `json:"developer"`
	Description     string `json:"description"`
	Category        string `json:"category"`
	Identifier      string `json:"identifier"`
	MinOSVersion    string `json:"min_os_version,omitempty"`
	Architecture    string `json:"architecture,omitempty"`
	InstallLocation string `json:"install_location,omitempty"`
	ProductCode     string `json:"product_code,omitempty"`
	UpgradeCode     string `json:"upgrade_code,omitempty"`
}

// Extract reads the given file bytes and filename and extracts metadata.
func Extract(filename string, data []byte) (*PackageMetadata, error) {
	ext := strings.ToLower(filepath.Ext(filename))
	baseName := strings.TrimSuffix(filename, ext)

	meta := &PackageMetadata{
		Name: baseName,
	}

	switch ext {
	case ".pkg":
		meta.Platform = "darwin"
		meta.InstallerType = "pkg"
		if err := extractPkg(data, meta); err != nil {
			// Non-fatal: we still have basic info from filename
			_ = err
		}
	case ".dmg":
		meta.Platform = "darwin"
		meta.InstallerType = "dmg"
		meta.InstallLocation = "/Applications"
		// DMG requires mounting; just use filename info
		extractFromFilename(baseName, meta)
	case ".msi":
		meta.Platform = "windows"
		meta.InstallerType = "msi"
		if err := extractMSI(data, meta); err != nil {
			_ = err
		}
	case ".exe":
		meta.Platform = "windows"
		meta.InstallerType = "exe"
		if err := extractEXE(data, meta); err != nil {
			_ = err
		}
	case ".msix", ".appx":
		meta.Platform = "windows"
		meta.InstallerType = "msix"
		if err := extractMSIX(data, meta); err != nil {
			_ = err
		}
	default:
		return nil, fmt.Errorf("unsupported file type: %s", ext)
	}

	// If no display name was extracted, use the name
	if meta.DisplayName == "" {
		meta.DisplayName = meta.Name
	}

	return meta, nil
}

// --- .pkg (XAR archive) extractor ---

const xarMagic = 0x78617221 // "xar!"

type xarHeader struct {
	Magic                uint32
	HeaderSize           uint16
	Version              uint16
	TOCLengthCompressed  uint64
	TOCLengthUncompressed uint64
	CksumAlg             uint32
}

// xarTOC represents the parsed TOC XML
type xarTOC struct {
	XMLName xml.Name   `xml:"xar"`
	TOC     xarTOCBody `xml:"toc"`
}

type xarTOCBody struct {
	Files []xarFile `xml:"file"`
}

type xarFile struct {
	ID   string    `xml:"id,attr"`
	Name string    `xml:"name"`
	Type string    `xml:"type"`
	Data xarData   `xml:"data"`
	Files []xarFile `xml:"file"`
}

type xarData struct {
	Offset   int64  `xml:"offset"`
	Size     int64  `xml:"size"`
	Length   int64  `xml:"length"`
	Encoding xarEncoding `xml:"encoding"`
}

type xarEncoding struct {
	Style string `xml:"style,attr"`
}

// pkgInfo represents PackageInfo XML from a .pkg
type pkgInfo struct {
	XMLName         xml.Name `xml:"pkg-info"`
	Identifier      string   `xml:"identifier,attr"`
	Version         string   `xml:"version,attr"`
	InstallLocation string   `xml:"install-location,attr"`
}

// distribution represents Distribution XML from a .pkg
type distribution struct {
	XMLName xml.Name `xml:"installer-gui-script"`
	Title   string   `xml:"title"`
	Options struct {
		Title string `xml:"title,attr"`
	} `xml:"options"`
	OSVersion []struct {
		Min string `xml:"min,attr"`
	} `xml:"os-version"`
	Domains struct {
		EnableAnywhere    bool `xml:"enable_anywhere,attr"`
		EnableCurrentUser bool `xml:"enable_currentUserHome,attr"`
	} `xml:"domains"`
	PkgRef []struct {
		ID      string `xml:"id,attr"`
		Version string `xml:"version,attr"`
	} `xml:"pkg-ref"`
}

func extractPkg(data []byte, meta *PackageMetadata) error {
	if len(data) < 28 {
		return fmt.Errorf("file too small for xar")
	}

	r := bytes.NewReader(data)
	var header xarHeader
	if err := binary.Read(r, binary.BigEndian, &header); err != nil {
		return fmt.Errorf("reading xar header: %w", err)
	}

	if header.Magic != xarMagic {
		return fmt.Errorf("not a valid xar/pkg file")
	}

	// Read compressed TOC
	tocOffset := int64(header.HeaderSize)
	if tocOffset+int64(header.TOCLengthCompressed) > int64(len(data)) {
		return fmt.Errorf("TOC extends beyond file")
	}

	compressedTOC := data[tocOffset : tocOffset+int64(header.TOCLengthCompressed)]
	zlibReader, err := zlib.NewReader(bytes.NewReader(compressedTOC))
	if err != nil {
		return fmt.Errorf("decompressing TOC: %w", err)
	}
	defer zlibReader.Close()

	tocData, err := io.ReadAll(zlibReader)
	if err != nil {
		return fmt.Errorf("reading TOC: %w", err)
	}

	var toc xarTOC
	if err := xml.Unmarshal(tocData, &toc); err != nil {
		return fmt.Errorf("parsing TOC XML: %w", err)
	}

	// Heap starts after header + compressed TOC
	heapOffset := tocOffset + int64(header.TOCLengthCompressed)

	// Look for PackageInfo and Distribution in the TOC
	allFiles := flattenFiles(toc.TOC.Files)

	for _, f := range allFiles {
		name := strings.ToLower(f.Name)
		if name == "packageinfo" || name == "pkg-info" {
			content, err := extractFileFromHeap(data, heapOffset, f)
			if err != nil {
				continue
			}
			var pi pkgInfo
			if err := xml.Unmarshal(content, &pi); err == nil {
				if pi.Identifier != "" {
					meta.Identifier = pi.Identifier
					// Derive a friendly name from the identifier
					parts := strings.Split(pi.Identifier, ".")
					if len(parts) > 0 {
						meta.Name = parts[len(parts)-1]
					}
				}
				if pi.Version != "" {
					meta.Version = pi.Version
				}
				if pi.InstallLocation != "" {
					meta.InstallLocation = pi.InstallLocation
				}
			}
		}
		if name == "distribution" {
			content, err := extractFileFromHeap(data, heapOffset, f)
			if err != nil {
				continue
			}
			var dist distribution
			if err := xml.Unmarshal(content, &dist); err == nil {
				if dist.Title != "" {
					meta.DisplayName = dist.Title
					meta.Name = dist.Title
				}
				if dist.Options.Title != "" {
					meta.DisplayName = dist.Options.Title
				}
				if len(dist.OSVersion) > 0 && dist.OSVersion[0].Min != "" {
					meta.MinOSVersion = dist.OSVersion[0].Min
				}
				// Get version from pkg-ref if not already set
				if meta.Version == "" && len(dist.PkgRef) > 0 {
					for _, ref := range dist.PkgRef {
						if ref.Version != "" {
							meta.Version = ref.Version
							break
						}
					}
				}
			}
		}
	}

	// If still no version, try to extract from filename
	if meta.Version == "" {
		extractFromFilename(meta.Name, meta)
	}

	return nil
}

func flattenFiles(files []xarFile) []xarFile {
	var result []xarFile
	for _, f := range files {
		result = append(result, f)
		if len(f.Files) > 0 {
			result = append(result, flattenFiles(f.Files)...)
		}
	}
	return result
}

func extractFileFromHeap(data []byte, heapOffset int64, f xarFile) ([]byte, error) {
	start := heapOffset + f.Data.Offset
	end := start + f.Data.Length
	if end > int64(len(data)) {
		return nil, fmt.Errorf("file data extends beyond archive")
	}

	fileData := data[start:end]

	// If compressed (gzip/zlib), decompress
	if f.Data.Encoding.Style == "application/x-gzip" || f.Data.Encoding.Style == "application/gzip" {
		zlibReader, err := zlib.NewReader(bytes.NewReader(fileData))
		if err != nil {
			return nil, err
		}
		defer zlibReader.Close()
		return io.ReadAll(zlibReader)
	}

	// If encoding is octet-stream or empty, data might still be zlib compressed
	if f.Data.Size != f.Data.Length && f.Data.Length > 0 {
		zlibReader, err := zlib.NewReader(bytes.NewReader(fileData))
		if err == nil {
			defer zlibReader.Close()
			decompressed, err := io.ReadAll(zlibReader)
			if err == nil {
				return decompressed, nil
			}
		}
	}

	return fileData, nil
}

// --- .msix / .appx extractor ---

type appxIdentity struct {
	Name                  string `xml:"Name,attr"`
	Publisher             string `xml:"Publisher,attr"`
	Version               string `xml:"Version,attr"`
	ProcessorArchitecture string `xml:"ProcessorArchitecture,attr"`
}

type appxProperties struct {
	DisplayName string `xml:"DisplayName"`
	PublisherDisplayName string `xml:"PublisherDisplayName"`
	Description string `xml:"Description"`
}

type appxManifest struct {
	XMLName    xml.Name       `xml:"Package"`
	Identity   appxIdentity   `xml:"Identity"`
	Properties appxProperties `xml:"Properties"`
}

func extractMSIX(data []byte, meta *PackageMetadata) error {
	r, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return fmt.Errorf("opening msix as zip: %w", err)
	}

	for _, f := range r.File {
		if strings.EqualFold(f.Name, "AppxManifest.xml") {
			rc, err := f.Open()
			if err != nil {
				return err
			}
			defer rc.Close()

			content, err := io.ReadAll(rc)
			if err != nil {
				return err
			}

			var manifest appxManifest
			if err := xml.Unmarshal(content, &manifest); err != nil {
				return fmt.Errorf("parsing AppxManifest.xml: %w", err)
			}

			if manifest.Identity.Name != "" {
				meta.Name = manifest.Identity.Name
				meta.Identifier = manifest.Identity.Name
			}
			if manifest.Identity.Version != "" {
				meta.Version = manifest.Identity.Version
			}
			if manifest.Identity.ProcessorArchitecture != "" {
				meta.Architecture = manifest.Identity.ProcessorArchitecture
			}
			if manifest.Properties.DisplayName != "" {
				meta.DisplayName = manifest.Properties.DisplayName
			}
			if manifest.Properties.PublisherDisplayName != "" {
				meta.Developer = manifest.Properties.PublisherDisplayName
			}
			if manifest.Properties.Description != "" {
				meta.Description = manifest.Properties.Description
			}
			// Extract publisher name from CN=... format
			if meta.Developer == "" && manifest.Identity.Publisher != "" {
				meta.Developer = extractCN(manifest.Identity.Publisher)
			}

			return nil
		}
	}

	return fmt.Errorf("AppxManifest.xml not found in msix")
}

func extractCN(publisher string) string {
	for _, part := range strings.Split(publisher, ",") {
		part = strings.TrimSpace(part)
		if strings.HasPrefix(part, "CN=") {
			return strings.TrimPrefix(part, "CN=")
		}
	}
	return publisher
}

// --- .exe (PE) extractor ---

// VS_FIXEDFILEINFO constants
const vsFFISignature = 0xFEEF04BD

func extractEXE(data []byte, meta *PackageMetadata) error {
	f, err := pe.NewFile(bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("parsing PE: %w", err)
	}
	defer f.Close()

	// Find the .rsrc section
	rsrc := f.Section(".rsrc")
	if rsrc == nil {
		return fmt.Errorf("no .rsrc section found")
	}

	rsrcData, err := rsrc.Data()
	if err != nil {
		return fmt.Errorf("reading .rsrc: %w", err)
	}

	// Search for VS_VERSION_INFO in the resource data
	versionInfo := findVersionInfo(rsrcData)
	if versionInfo == nil {
		return fmt.Errorf("VS_VERSION_INFO not found")
	}

	// Parse StringFileInfo for human-readable metadata
	stringValues := parseStringFileInfo(versionInfo)

	if v, ok := stringValues["ProductName"]; ok && v != "" {
		meta.Name = v
		meta.DisplayName = v
	}
	if v, ok := stringValues["FileDescription"]; ok && v != "" {
		if meta.DisplayName == "" {
			meta.DisplayName = v
		}
		meta.Description = v
	}
	if v, ok := stringValues["ProductVersion"]; ok && v != "" {
		meta.Version = v
	} else if v, ok := stringValues["FileVersion"]; ok && v != "" {
		meta.Version = v
	}
	if v, ok := stringValues["CompanyName"]; ok && v != "" {
		meta.Developer = v
	}

	return nil
}

// findVersionInfo searches for the VS_VERSION_INFO structure in resource data.
func findVersionInfo(data []byte) []byte {
	// Look for the UTF-16LE encoded "VS_VERSION_INFO" string
	needle := encodeUTF16LE("VS_VERSION_INFO")
	idx := bytes.Index(data, needle)
	if idx < 0 {
		return nil
	}

	// VS_VERSION_INFO starts 6 bytes before the key string
	// (wLength uint16, wValueLength uint16, wType uint16)
	start := idx - 6
	if start < 0 {
		start = 0
	}

	// Read the total length from the structure
	if start+2 > len(data) {
		return nil
	}
	totalLen := int(binary.LittleEndian.Uint16(data[start : start+2]))
	end := start + totalLen
	if end > len(data) {
		end = len(data)
	}

	return data[start:end]
}

// parseStringFileInfo extracts key-value pairs from VS_VERSION_INFO.
func parseStringFileInfo(data []byte) map[string]string {
	result := make(map[string]string)

	// Known keys to search for
	keys := []string{
		"CompanyName", "FileDescription", "FileVersion",
		"InternalName", "LegalCopyright", "OriginalFilename",
		"ProductName", "ProductVersion",
	}

	for _, key := range keys {
		needle := encodeUTF16LE(key)
		idx := bytes.Index(data, needle)
		if idx < 0 {
			continue
		}

		// After the key string (null-terminated UTF-16), find the value
		keyEnd := idx + len(needle) + 2 // +2 for null terminator
		// Align to 4-byte boundary
		if keyEnd%4 != 0 {
			keyEnd += 4 - (keyEnd % 4)
		}

		if keyEnd >= len(data) {
			continue
		}

		// Read the value as UTF-16LE until null terminator
		value := readUTF16LEString(data[keyEnd:])
		if value != "" {
			result[key] = value
		}
	}

	return result
}

func encodeUTF16LE(s string) []byte {
	runes := []rune(s)
	buf := make([]byte, len(runes)*2)
	for i, r := range runes {
		binary.LittleEndian.PutUint16(buf[i*2:], uint16(r))
	}
	return buf
}

func readUTF16LEString(data []byte) string {
	if len(data) < 2 {
		return ""
	}

	var runes []uint16
	for i := 0; i+1 < len(data) && i < 512; i += 2 {
		ch := binary.LittleEndian.Uint16(data[i : i+2])
		if ch == 0 {
			break
		}
		runes = append(runes, ch)
	}

	return string(utf16.Decode(runes))
}

// --- .msi extractor ---

// MSI files are OLE Compound Documents. We parse them to find the
// summary information stream and string pool/tables.

func extractMSI(data []byte, meta *PackageMetadata) error {
	// MSI files are OLE Compound Documents
	// The summary information stream contains basic metadata
	// We'll look for the string data that contains ProductName, ProductVersion, etc.

	// Simple approach: scan for common MSI property strings in the binary data
	// MSI stores strings as UTF-16LE in the string pool

	// Try to find key properties by searching for UTF-16LE encoded values
	// near known property names

	// Search for "ProductName" property
	if name := findMSIProperty(data, "ProductName"); name != "" {
		meta.Name = name
		meta.DisplayName = name
	}

	// Search for "ProductVersion"
	if version := findMSIProperty(data, "ProductVersion"); version != "" {
		meta.Version = version
	}

	// Search for "Manufacturer"
	if manufacturer := findMSIProperty(data, "Manufacturer"); manufacturer != "" {
		meta.Developer = manufacturer
	}

	// Search for "ProductCode" and "UpgradeCode" GUIDs
	// MSI stores these as UTF-16LE GUIDs in the string pool; findMSIGUID
	// scans near the property name for a {GUID} pattern.
	if productCode := findMSIGUID(data, "ProductCode"); productCode != "" {
		meta.ProductCode = productCode
		meta.Identifier = productCode
	}
	if upgradeCode := findMSIGUID(data, "UpgradeCode"); upgradeCode != "" {
		meta.UpgradeCode = upgradeCode
	}

	// If nothing found via property search, try filename
	if meta.Version == "" {
		extractFromFilename(meta.Name, meta)
	}

	return nil
}

// findMSIProperty searches for a property name in the MSI binary data
// and tries to extract its value. MSI stores properties in a string pool
// as ASCII or UTF-16LE depending on the MSI version.
func findMSIProperty(data []byte, propertyName string) string {
	// Try ASCII first (MSI string pool often stores as ASCII)
	if val := findMSIPropertyASCII(data, propertyName); val != "" {
		return val
	}
	// Fallback: try UTF-16LE
	return findMSIPropertyUTF16(data, propertyName)
}

// msiPropertyNames lists known MSI property names used as terminators
// when reading values from the MSI string pool.
var msiPropertyNames = []string{
	"ProductCode", "ProductName", "ProductVersion", "ProductLanguage",
	"Manufacturer", "UpgradeCode", "ARPCONTACT", "ARPHELPLINK",
	"ARPURLINFOABOUT", "ARPURLUPDATEINFO", "ARPPRODUCTICON",
	"ALLUSERS", "REINSTALLMODE", "MSIINSTALLPERUSER",
}

// findMSIPropertyASCII searches for a property name as ASCII and reads
// the value that follows directly after it in the string pool.
// MSI string pool entries are concatenated without null terminators,
// so we use known property names as boundary markers.
func findMSIPropertyASCII(data []byte, propertyName string) string {
	needle := []byte(propertyName)
	idx := bytes.Index(data, needle)
	if idx < 0 {
		return ""
	}

	// Value starts right after the property name
	valStart := idx + len(needle)
	if valStart >= len(data) {
		return ""
	}

	// Read printable ASCII chars
	var raw strings.Builder
	for i := valStart; i < len(data) && i < valStart+500; i++ {
		c := data[i]
		if c >= 0x20 && c <= 0x7E {
			raw.WriteByte(c)
		} else {
			break
		}
	}

	val := raw.String()
	if len(val) < 1 {
		return ""
	}

	// Truncate at the first known property name boundary
	for _, prop := range msiPropertyNames {
		if prop == propertyName {
			continue
		}
		if i := strings.Index(val, prop); i > 0 {
			val = val[:i]
		}
	}

	// Also truncate at '{' if this isn't a GUID property (GUIDs start with '{')
	if propertyName != "ProductCode" && propertyName != "UpgradeCode" {
		if i := strings.Index(val, "{"); i > 0 {
			val = val[:i]
		}
	}

	return val
}

// findMSIPropertyUTF16 searches for a property name as UTF-16LE.
func findMSIPropertyUTF16(data []byte, propertyName string) string {
	needle := encodeUTF16LE(propertyName)
	idx := bytes.Index(data, needle)
	if idx < 0 {
		return ""
	}

	searchStart := idx + len(needle) + 2
	searchEnd := searchStart + 1024
	if searchEnd > len(data) {
		searchEnd = len(data)
	}

	for offset := searchStart; offset < searchEnd-2; offset += 2 {
		ch := binary.LittleEndian.Uint16(data[offset : offset+2])
		if ch >= 0x20 && ch < 0x7F {
			val := readUTF16LEString(data[offset:])
			if len(val) >= 2 && len(val) < 200 && isPrintable(val) {
				return val
			}
		}
	}

	return ""
}

// findMSIGUID searches for a GUID value associated with a property name in MSI binary data.
// MSI string pools store property names as ASCII (not UTF-16LE) with values nearby.
// This function searches for the property name in ASCII, then scans forward for a {GUID}.
func findMSIGUID(data []byte, propertyName string) string {
	// Try ASCII first (MSI string pool stores names as ASCII)
	needle := []byte(propertyName)
	idx := bytes.Index(data, needle)

	// Fallback to UTF-16LE if not found as ASCII
	if idx < 0 {
		needle = encodeUTF16LE(propertyName)
		idx = bytes.Index(data, needle)
	}
	if idx < 0 {
		return ""
	}

	// Scan forward from the property name for a GUID pattern {xxxxxxxx-xxxx-...}
	searchStart := idx + len(needle)
	searchEnd := searchStart + 512
	if searchEnd > len(data) {
		searchEnd = len(data)
	}

	region := data[searchStart:searchEnd]
	// Look for '{' as ASCII byte
	for i := 0; i < len(region)-38; i++ {
		if region[i] == '{' {
			// Try to read 38 ASCII chars for a GUID
			if i+38 <= len(region) {
				candidate := string(region[i : i+38])
				if isGUID(candidate) {
					return candidate
				}
			}
			// Also try UTF-16LE (76 bytes for 38 chars)
			if i+76 <= len(region) {
				candidate := readUTF16LEString(region[i : i+76])
				if isGUID(candidate) {
					return candidate
				}
			}
		}
	}

	return ""
}

// isGUID checks if a string matches the pattern {xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx}
func isGUID(s string) bool {
	if len(s) != 38 || s[0] != '{' || s[37] != '}' {
		return false
	}
	// Check dash positions: 9, 14, 19, 24
	if s[9] != '-' || s[14] != '-' || s[19] != '-' || s[24] != '-' {
		return false
	}
	for i, c := range s[1:37] {
		pos := i + 1
		if pos == 9 || pos == 14 || pos == 19 || pos == 24 {
			continue // dash positions
		}
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			return false
		}
	}
	return true
}

func isPrintable(s string) bool {
	for _, r := range s {
		if r < 0x20 || r > 0x7E {
			return false
		}
	}
	return true
}

// --- Filename-based extraction fallback ---

func extractFromFilename(baseName string, meta *PackageMetadata) {
	// Try common patterns: AppName-1.2.3, AppName_1.2.3
	for _, sep := range []string{"-", "_"} {
		parts := strings.SplitN(baseName, sep, 2)
		if len(parts) == 2 && looksLikeVersion(parts[1]) {
			if meta.Name == baseName || meta.Name == "" {
				meta.Name = parts[0]
			}
			if meta.Version == "" {
				meta.Version = parts[1]
			}
			return
		}
	}
	// For spaces, find the last space before a version string to preserve
	// spaces in app names (e.g. "Suspicious Package 4.3.1" → "Suspicious Package")
	lastIdx := strings.LastIndex(baseName, " ")
	for lastIdx > 0 {
		candidate := baseName[lastIdx+1:]
		if looksLikeVersion(candidate) {
			if meta.Name == baseName || meta.Name == "" {
				meta.Name = baseName[:lastIdx]
			}
			if meta.Version == "" {
				meta.Version = candidate
			}
			return
		}
		lastIdx = strings.LastIndex(baseName[:lastIdx], " ")
	}
}

func looksLikeVersion(s string) bool {
	if len(s) == 0 {
		return false
	}
	// Version strings typically start with a digit
	if s[0] < '0' || s[0] > '9' {
		return false
	}
	// Should contain at least one dot or be purely numeric
	return strings.Contains(s, ".") || isNumeric(s)
}

func isNumeric(s string) bool {
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

// ToPlist generates a complete Munki-compatible pkgsinfo plist XML from the extracted metadata.
// This becomes the single source of truth stored in pkginfo_data.
func (m *PackageMetadata) ToPlist() string {
	var b strings.Builder
	b.WriteString("<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n")
	b.WriteString("<!DOCTYPE plist PUBLIC \"-//Apple//DTD PLIST 1.0//EN\" \"http://www.apple.com/DTDs/PropertyList-1.0.dtd\">\n")
	b.WriteString("<plist version=\"1.0\">\n<dict>\n")

	plistStr(&b, "name", m.Name)
	plistStr(&b, "version", m.Version)
	if m.DisplayName != "" {
		plistStr(&b, "display_name", m.DisplayName)
	}
	if m.Description != "" {
		plistStr(&b, "description", m.Description)
	}
	if m.Category != "" {
		plistStr(&b, "category", m.Category)
	}
	if m.Developer != "" {
		plistStr(&b, "developer", m.Developer)
	}
	if m.InstallerType != "" {
		// Map internal types to Munki-valid installer_type values
		munkiType := m.InstallerType
		switch munkiType {
		case "dmg":
			munkiType = "copy_from_dmg"
		case "pkg":
			// Munki doesn't need installer_type for .pkg — it's the default
			munkiType = ""
		}
		if munkiType != "" {
			plistStr(&b, "installer_type", munkiType)
		}
	}
	if m.MinOSVersion != "" {
		plistStr(&b, "minimum_os_version", m.MinOSVersion)
	}

	b.WriteString("\t<key>unattended_install</key>\n\t<true/>\n")
	b.WriteString("\t<key>unattended_uninstall</key>\n\t<true/>\n")

	// Generate receipts from package identifier (for .pkg)
	if m.Identifier != "" && m.Platform == "darwin" {
		b.WriteString("\t<key>receipts</key>\n\t<array>\n\t\t<dict>\n")
		plistStrIndent(&b, "packageid", m.Identifier, 3)
		plistStrIndent(&b, "version", m.Version, 3)
		b.WriteString("\t\t</dict>\n\t</array>\n")
	}

	// Generate installs array for macOS apps
	if m.Platform == "darwin" {
		appName := m.DisplayName
		if appName == "" {
			appName = m.Name
		}
		if m.Identifier != "" {
			// .pkg with identifier: use identifier and install location heuristic
			if m.InstallLocation == "/Applications" || strings.Contains(m.Identifier, ".") {
				b.WriteString("\t<key>installs</key>\n\t<array>\n\t\t<dict>\n")
				plistStrIndent(&b, "CFBundleIdentifier", m.Identifier, 3)
				plistStrIndent(&b, "CFBundleShortVersionString", m.Version, 3)
				plistStrIndent(&b, "path", fmt.Sprintf("/Applications/%s.app", appName), 3)
				plistStrIndent(&b, "type", "application", 3)
				b.WriteString("\t\t</dict>\n\t</array>\n")
			}
		} else if m.InstallerType == "dmg" {
			// DMG: generate installs entry based on app name (installed to /Applications)
			b.WriteString("\t<key>installs</key>\n\t<array>\n\t\t<dict>\n")
			plistStrIndent(&b, "CFBundleShortVersionString", m.Version, 3)
			plistStrIndent(&b, "path", fmt.Sprintf("/Applications/%s.app", appName), 3)
			plistStrIndent(&b, "type", "application", 3)
			b.WriteString("\t\t</dict>\n\t</array>\n")
			// items_to_copy tells Munki what to copy from the DMG
			b.WriteString("\t<key>items_to_copy</key>\n\t<array>\n\t\t<dict>\n")
			plistStrIndent(&b, "source_item", fmt.Sprintf("%s.app", appName), 3)
			plistStrIndent(&b, "destination_path", "/Applications", 3)
			b.WriteString("\t\t</dict>\n\t</array>\n")
		}
	}

	// Supported architectures
	if m.Architecture != "" {
		b.WriteString("\t<key>supported_architectures</key>\n\t<array>\n")
		fmt.Fprintf(&b, "\t\t<string>%s</string>\n", html.EscapeString(m.Architecture))
		b.WriteString("\t</array>\n")
	}

	b.WriteString("</dict>\n</plist>\n")
	return b.String()
}

// ToYAML generates a CIMIAN-compatible pkginfo YAML from the extracted metadata.
// This becomes the single source of truth stored in pkginfo_data for Windows packages.
func (m *PackageMetadata) ToYAML() string {
	item := map[string]any{
		"name":    m.Name,
		"version": m.Version,
	}
	if m.DisplayName != "" {
		item["display_name"] = m.DisplayName
	}
	if m.Description != "" {
		item["description"] = m.Description
	}
	if m.Category != "" {
		item["category"] = m.Category
	}
	if m.Developer != "" {
		item["developer"] = m.Developer
	}

	// Top-level installer_type (CIMIAN uses this)
	if m.InstallerType != "" {
		item["installer_type"] = m.InstallerType
	}

	// Nested installer block
	installer := map[string]any{
		"type": m.InstallerType,
	}
	if m.ProductCode != "" {
		installer["product_code"] = m.ProductCode
	}
	if m.UpgradeCode != "" {
		installer["upgrade_code"] = m.UpgradeCode
	}
	item["installer"] = installer

	// Install detection: installs array
	if m.InstallerType == "msi" && m.ProductCode != "" {
		// MSI with ProductCode: use msi-type check for precise detection
		installCheck := map[string]any{
			"type":         "msi",
			"product_code": m.ProductCode,
			"version":      m.Version,
		}
		if m.UpgradeCode != "" {
			installCheck["upgrade_code"] = m.UpgradeCode
		}
		item["installs"] = []map[string]any{installCheck}
	}

	// Uninstaller block: allows CIMIAN to uninstall the package
	if m.InstallerType == "msi" && m.ProductCode != "" {
		uninstaller := map[string]any{
			"type":         "msi",
			"product_code": m.ProductCode,
		}
		item["uninstaller"] = []map[string]any{uninstaller}
	}

	// check.registry: fallback detection via DisplayName in Add/Remove Programs
	displayName := m.DisplayName
	if displayName == "" {
		displayName = m.Name
	}
	item["check"] = map[string]any{
		"registry": map[string]any{
			"name":    displayName,
			"version": m.Version,
		},
	}

	if m.MinOSVersion != "" {
		item["minimum_os_version"] = m.MinOSVersion
	}
	if m.Architecture != "" {
		item["supported_architectures"] = []string{m.Architecture}
	}

	item["unattended_install"] = true
	item["unattended_uninstall"] = true

	data, err := yaml.Marshal(item)
	if err != nil {
		return ""
	}
	return string(data)
}

func plistStr(b *strings.Builder, key, value string) {
	fmt.Fprintf(b, "\t<key>%s</key>\n\t<string>%s</string>\n", html.EscapeString(key), html.EscapeString(value))
}

func plistStrIndent(b *strings.Builder, key, value string, tabs int) {
	indent := strings.Repeat("\t", tabs)
	fmt.Fprintf(b, "%s<key>%s</key>\n%s<string>%s</string>\n", indent, html.EscapeString(key), indent, html.EscapeString(value))
}
