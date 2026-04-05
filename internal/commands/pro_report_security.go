// Copyright 2026, Jamf Software LLC

package commands

import (
	"context"
	"fmt"
	"sort"

	"github.com/spf13/cobra"

	"github.com/Jamf-Concepts/jamf-cli/internal/output"
	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
)

// Security status constants used across multiple commands.
const (
	statusGKDisabled    = "DISABLED"
	statusGKDisabledAlt = "Disabled" // Some API versions use mixed case
	statusSIPEnabled    = "ENABLED"
	statusSIPEnabledAlt = "Enabled"
)

// securityReport holds all sections of the security posture report.
type securityReport struct {
	Summary    map[string]any
	Devices    []map[string]any
	OSVersions []map[string]any
}

func newReportSecurityCmd(cliCtx *registry.CLIContext) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "security",
		Short: "Fleet security posture: encryption, Gatekeeper, SIP, firewall, OS currency",
		Long: `Generates a security posture report across all managed computers.

Includes:
  - Summary: encryption rate, Gatekeeper rate, SIP rate, firewall rate
  - Per-device: security status for each computer
  - OS distribution: version counts for currency analysis

Table output shows the summary and flagged devices. JSON/YAML output includes
all three sections.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			report, err := runReportSecurity(cmd.Context(), cliCtx.Client)
			if err != nil {
				return err
			}
			return printSecurityReport(report)
		},
	}
	return cmd
}

func runReportSecurity(ctx context.Context, client registry.HTTPClient) (*securityReport, error) {
	computers, err := FetchAllPaginated(ctx, client,
		"/v3/computers-inventory?section=GENERAL&section=HARDWARE&section=OPERATING_SYSTEM&section=SECURITY&section=DISK_ENCRYPTION", 100)
	if err != nil {
		return nil, fmt.Errorf("fetching computer inventory: %w", err)
	}

	var (
		total       int
		fvEncrypted int
		gkEnabled   int
		sipCount    int
		fwEnabled   int
		osVersions  = make(map[string]int)
		devices     []map[string]any
	)

	for _, c := range computers {
		total++

		general, _ := c["general"].(map[string]any)
		hardware, _ := c["hardware"].(map[string]any)
		osInfo, _ := c["operatingSystem"].(map[string]any)
		security, _ := c["security"].(map[string]any)
		diskEnc, _ := c["diskEncryption"].(map[string]any)

		name := strVal(general, "name")
		if name == "" {
			name = extractID(c)
		}
		serial := strVal(hardware, "serialNumber")
		osVersion := strVal(osInfo, "version")

		// v3 moved FileVault from security to diskEncryption section.
		// Use partition state as ground truth — fileVault2Enabled has narrower
		// semantics (MDM-managed) and under-reports actual encryption.
		fvStatus := fileVaultStatus(diskEnc)
		gkStatus := strVal(security, "gatekeeperStatus")
		sipStatus := strVal(security, "sipStatus")
		firewall := boolVal(security, "firewallEnabled")

		if fvStatus == "ENCRYPTED" {
			fvEncrypted++
		}
		if gkStatus != statusGKDisabled && gkStatus != statusGKDisabledAlt && gkStatus != "" {
			gkEnabled++
		}
		if sipStatus == statusSIPEnabled || sipStatus == statusSIPEnabledAlt {
			sipCount++
		}
		if firewall {
			fwEnabled++
		}

		if osVersion != "" {
			osVersions[osVersion]++
		}

		devices = append(devices, map[string]any{
			"name":       name,
			"serial":     serial,
			"os_version": osVersion,
			"filevault":  fvStatus,
			"gatekeeper": gkStatus,
			"sip":        sipStatus,
			"firewall":   firewall,
		})
	}

	summary := map[string]any{
		"total_devices":           total,
		"filevault_encrypted":     fvEncrypted,
		"filevault_encrypted_pct": pctStr(fvEncrypted, total),
		"gatekeeper_enabled":      gkEnabled,
		"gatekeeper_enabled_pct":  pctStr(gkEnabled, total),
		"sip_enabled":             sipCount,
		"sip_enabled_pct":         pctStr(sipCount, total),
		"firewall_enabled":        fwEnabled,
		"firewall_enabled_pct":    pctStr(fwEnabled, total),
	}

	// Build sorted OS version breakdown (newest first)
	var osRows []map[string]any
	for ver, count := range osVersions {
		osRows = append(osRows, map[string]any{
			"os_version": ver,
			"count":      count,
			"pct":        pctStr(count, total),
		})
	}
	sort.Slice(osRows, func(i, j int) bool {
		return osRows[i]["os_version"].(string) > osRows[j]["os_version"].(string)
	})

	return &securityReport{
		Summary:    summary,
		Devices:    devices,
		OSVersions: osRows,
	}, nil
}

func printSecurityReport(report *securityReport) error {
	formatter := output.New(outputFmt, noColor, wide)

	if outputFmt == "json" || outputFmt == "yaml" {
		// Structured output: combine all sections
		combined := []map[string]any{
			{"section": "summary", "data": report.Summary},
		}
		for _, d := range report.Devices {
			d["section"] = "device"
			combined = append(combined, d)
		}
		for _, o := range report.OSVersions {
			o["section"] = "os_version"
			combined = append(combined, o)
		}
		return formatter.Print(combined)
	}

	// Table output: summary first, then flagged devices only
	fmt.Println("── Security Summary ──")
	summaryRows := []map[string]any{report.Summary}
	if err := formatter.Print(summaryRows); err != nil {
		return err
	}

	// Show only devices with at least one issue
	var flagged []map[string]any
	for _, d := range report.Devices {
		fv, _ := d["filevault"].(string)
		gk, _ := d["gatekeeper"].(string)
		sip, _ := d["sip"].(string)
		fw, _ := d["firewall"].(bool)
		if fv != "ENCRYPTED" ||
			gk == statusGKDisabled || gk == statusGKDisabledAlt ||
			(sip != statusSIPEnabled && sip != statusSIPEnabledAlt && sip != "") ||
			!fw {
			flagged = append(flagged, d)
		}
	}

	if len(flagged) > 0 {
		fmt.Printf("\n── Flagged Devices (%d) ──\n", len(flagged))
		if err := formatter.Print(flagged); err != nil {
			return err
		}
	}

	if len(report.OSVersions) > 0 {
		fmt.Println("\n── OS Version Distribution ──")
		return formatter.Print(report.OSVersions)
	}

	return nil
}

func pctStr(n, total int) string {
	if total == 0 {
		return "N/A"
	}
	return fmt.Sprintf("%.1f%%", float64(n)/float64(total)*100)
}

// --- Small extraction helpers ---

// strVal safely extracts a string value from a map.
func strVal(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	v, _ := m[key].(string)
	return v
}

// boolVal safely extracts a bool value from a map.
func boolVal(m map[string]any, key string) bool {
	if m == nil {
		return false
	}
	v, _ := m[key].(bool)
	return v
}

// fileVaultStatus extracts the boot partition FileVault state from the
// v3 diskEncryption section. Returns the partition state string
// (e.g. "ENCRYPTED", "UNENCRYPTED") or empty string if unavailable.
func fileVaultStatus(diskEnc map[string]any) string {
	if diskEnc == nil {
		return ""
	}
	boot, _ := diskEnc["bootPartitionEncryptionDetails"].(map[string]any)
	if boot == nil {
		return ""
	}
	state, _ := boot["partitionFileVault2State"].(string)
	return state
}
