// Copyright 2026, Jamf Software LLC

package commands

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"

	"github.com/spf13/cobra"

	"github.com/Jamf-Concepts/jamf-cli/internal/protect"
	"github.com/Jamf-Concepts/jamf-cli/internal/registry"
	"github.com/Jamf-Concepts/jamfprotect-go-sdk/jamfprotect"
)

// --- Data Forwarding ---

func newProtectDataForwardingCmd(cliCtx *registry.CLIContext) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "data-forwarding",
		Short: "Manage data forwarding settings",
	}

	cmd.AddCommand(newProtectDataForwardingGetCmd(cliCtx))
	cmd.AddCommand(newProtectDataForwardingUpdateCmd(cliCtx))

	return cmd
}

func newProtectDataForwardingGetCmd(cliCtx *registry.CLIContext) *cobra.Command {
	return &cobra.Command{
		Use:   "get",
		Short: "Get data forwarding settings",
		RunE: func(cmd *cobra.Command, _ []string) error {
			item, err := cliCtx.ProtectClient.GetDataForwarding(cmd.Context())
			if err != nil {
				return err
			}
			return protect.PrintOne(cliCtx.Output, item)
		},
	}
}

func newProtectDataForwardingUpdateCmd(cliCtx *registry.CLIContext) *cobra.Command {
	var fromFile string

	cmd := &cobra.Command{
		Use:   "update",
		Short: "Update data forwarding settings",
		RunE: func(cmd *cobra.Command, _ []string) error {
			data, err := readInput(fromFile)
			if err != nil {
				return err
			}
			var input jamfprotect.DataForwardingInput
			if err := unmarshalInput(data, &input); err != nil {
				return fmt.Errorf("parsing input file: %w", err)
			}

			item, err := cliCtx.ProtectClient.UpdateDataForwarding(cmd.Context(), input)
			if err != nil {
				return err
			}
			return protect.PrintOne(cliCtx.Output, item)
		},
	}

	cmd.Flags().StringVar(&fromFile, "from-file", "", "Path to JSON input file (or pipe JSON to stdin)")

	return cmd
}

// --- Data Retention ---

func newProtectDataRetentionCmd(cliCtx *registry.CLIContext) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "data-retention",
		Short: "Manage data retention settings",
	}

	cmd.AddCommand(newProtectDataRetentionGetCmd(cliCtx))
	cmd.AddCommand(newProtectDataRetentionUpdateCmd(cliCtx))

	return cmd
}

func newProtectDataRetentionGetCmd(cliCtx *registry.CLIContext) *cobra.Command {
	return &cobra.Command{
		Use:   "get",
		Short: "Get data retention settings",
		RunE: func(cmd *cobra.Command, _ []string) error {
			item, err := cliCtx.ProtectClient.GetDataRetention(cmd.Context())
			if err != nil {
				return err
			}
			return protect.PrintOne(cliCtx.Output, item)
		},
	}
}

func newProtectDataRetentionUpdateCmd(cliCtx *registry.CLIContext) *cobra.Command {
	var fromFile string

	cmd := &cobra.Command{
		Use:   "update",
		Short: "Update data retention settings",
		RunE: func(cmd *cobra.Command, _ []string) error {
			data, err := readInput(fromFile)
			if err != nil {
				return err
			}
			var input jamfprotect.DataRetentionInput
			if err := unmarshalInput(data, &input); err != nil {
				return fmt.Errorf("parsing input file: %w", err)
			}

			item, err := cliCtx.ProtectClient.UpdateDataRetention(cmd.Context(), input)
			if err != nil {
				return err
			}
			return protect.PrintOne(cliCtx.Output, item)
		},
	}

	cmd.Flags().StringVar(&fromFile, "from-file", "", "Path to JSON input file (or pipe JSON to stdin)")

	return cmd
}

// --- Downloads ---

func newProtectDownloadsCmd(cliCtx *registry.CLIContext) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "downloads",
		Short: "Get organization download links",
	}

	cmd.AddCommand(newProtectDownloadsSummaryCmd(cliCtx))
	cmd.AddCommand(newProtectDownloadsInstallerCmd(cliCtx))
	cmd.AddCommand(newProtectDownloadsUninstallerCmd(cliCtx))
	cmd.AddCommand(newProtectDownloadsPPPCCmd(cliCtx))
	cmd.AddCommand(newProtectDownloadsTamperPreventionCmd(cliCtx))
	cmd.AddCommand(newProtectDownloadsRootCACmd(cliCtx))
	cmd.AddCommand(newProtectDownloadsCSRCmd(cliCtx))
	cmd.AddCommand(newProtectDownloadsWebsocketAuthCmd(cliCtx))

	return cmd
}

func newProtectDownloadsSummaryCmd(cliCtx *registry.CLIContext) *cobra.Command {
	return &cobra.Command{
		Use:   "summary",
		Short: "Show download metadata (version, URLs)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			downloads, err := cliCtx.ProtectClient.GetOrganizationDownloads(ctx)
			if err != nil {
				return err
			}

			baseURL := cliCtx.ProtectClient.BaseURL()
			version := ""
			if downloads.VanillaPackage != nil {
				version = downloads.VanillaPackage.Version
			}

			summary := map[string]any{
				"installerVersion":           version,
				"installerURL":               buildProtectPackageURL(baseURL, "installer.pkg", downloads.InstallerUUID),
				"uninstallerURL":             buildProtectPackageURL(baseURL, "uninstaller.pkg", downloads.InstallerUUID),
				"hasPPPC":                    downloads.PPPC != "",
				"hasRootCA":                  downloads.RootCA != "",
				"hasCSR":                     downloads.CSR != "",
				"hasWebsocketAuth":           downloads.WebsocketAuth != "",
				"hasTamperPreventionProfile": downloads.TamperPreventionProfile != "",
			}
			data, err := json.Marshal(summary)
			if err != nil {
				return fmt.Errorf("marshalling output: %w", err)
			}
			return cliCtx.Output.PrintRaw(data)
		},
	}
}

func newProtectDownloadsInstallerCmd(cliCtx *registry.CLIContext) *cobra.Command {
	var outPath string

	cmd := &cobra.Command{
		Use:   "installer",
		Short: "Download the Jamf Protect installer package",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			downloads, err := cliCtx.ProtectClient.GetOrganizationDownloads(ctx)
			if err != nil {
				return err
			}
			pkgURL := buildProtectPackageURL(cliCtx.ProtectClient.BaseURL(), "installer.pkg", downloads.InstallerUUID)
			if pkgURL == "" {
				return fmt.Errorf("installer package not available")
			}
			if outPath == "" {
				version := ""
				if downloads.VanillaPackage != nil {
					version = downloads.VanillaPackage.Version
				}
				outPath = fmt.Sprintf("JamfProtect-%s.pkg", version)
			}
			return downloadProtectFile(ctx, cliCtx.ProtectClient, pkgURL, outPath)
		},
	}

	cmd.Flags().StringVarP(&outPath, "output", "O", "", "Output file path (default: JamfProtect-{version}.pkg)")
	return cmd
}

func newProtectDownloadsUninstallerCmd(cliCtx *registry.CLIContext) *cobra.Command {
	var outPath string

	cmd := &cobra.Command{
		Use:   "uninstaller",
		Short: "Download the Jamf Protect uninstaller package",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			downloads, err := cliCtx.ProtectClient.GetOrganizationDownloads(ctx)
			if err != nil {
				return err
			}
			pkgURL := buildProtectPackageURL(cliCtx.ProtectClient.BaseURL(), "uninstaller.pkg", downloads.InstallerUUID)
			if pkgURL == "" {
				return fmt.Errorf("uninstaller package not available")
			}
			if outPath == "" {
				version := ""
				if downloads.VanillaPackage != nil {
					version = downloads.VanillaPackage.Version
				}
				outPath = fmt.Sprintf("JamfProtectUninstaller-%s.pkg", version)
			}
			return downloadProtectFile(ctx, cliCtx.ProtectClient, pkgURL, outPath)
		},
	}

	cmd.Flags().StringVarP(&outPath, "output", "O", "", "Output file path (default: JamfProtectUninstaller-{version}.pkg)")
	return cmd
}

func newProtectDownloadsPPPCCmd(cliCtx *registry.CLIContext) *cobra.Command {
	var outPath string

	cmd := &cobra.Command{
		Use:   "pppc-profile",
		Short: "Download the PPPC configuration profile",
		RunE: func(cmd *cobra.Command, _ []string) error {
			downloads, err := cliCtx.ProtectClient.GetOrganizationDownloads(cmd.Context())
			if err != nil {
				return err
			}
			if downloads.PPPC == "" {
				return fmt.Errorf("PPPC profile not available")
			}
			if outPath == "" {
				outPath = "JamfProtect-PPPC.mobileconfig"
			}
			return writeBase64File(downloads.PPPC, outPath)
		},
	}

	cmd.Flags().StringVarP(&outPath, "output", "O", "", "Output file path (default: JamfProtect-PPPC.mobileconfig)")
	return cmd
}

func newProtectDownloadsTamperPreventionCmd(cliCtx *registry.CLIContext) *cobra.Command {
	var outPath string

	cmd := &cobra.Command{
		Use:   "tamper-prevention-profile",
		Short: "Download the tamper prevention system extension profile",
		RunE: func(cmd *cobra.Command, _ []string) error {
			downloads, err := cliCtx.ProtectClient.GetOrganizationDownloads(cmd.Context())
			if err != nil {
				return err
			}
			if downloads.TamperPreventionProfile == "" {
				return fmt.Errorf("tamper prevention profile not available")
			}
			if outPath == "" {
				outPath = "JamfProtect-TamperPrevention.mobileconfig"
			}
			return writeBase64File(downloads.TamperPreventionProfile, outPath)
		},
	}

	cmd.Flags().StringVarP(&outPath, "output", "O", "", "Output file path (default: JamfProtect-TamperPrevention.mobileconfig)")
	return cmd
}

func newProtectDownloadsRootCACmd(cliCtx *registry.CLIContext) *cobra.Command {
	var outPath string

	cmd := &cobra.Command{
		Use:   "root-ca",
		Short: "Download the root CA certificate",
		RunE: func(cmd *cobra.Command, _ []string) error {
			downloads, err := cliCtx.ProtectClient.GetOrganizationDownloads(cmd.Context())
			if err != nil {
				return err
			}
			if downloads.RootCA == "" {
				return fmt.Errorf("root CA certificate not available")
			}
			if outPath == "" {
				outPath = "JamfProtect-RootCA.pem"
			}
			return writeBase64File(downloads.RootCA, outPath)
		},
	}

	cmd.Flags().StringVarP(&outPath, "output", "O", "", "Output file path (default: JamfProtect-RootCA.pem)")
	return cmd
}

func newProtectDownloadsCSRCmd(cliCtx *registry.CLIContext) *cobra.Command {
	var outPath string

	cmd := &cobra.Command{
		Use:   "csr",
		Short: "Download the CSR identity",
		RunE: func(cmd *cobra.Command, _ []string) error {
			downloads, err := cliCtx.ProtectClient.GetOrganizationDownloads(cmd.Context())
			if err != nil {
				return err
			}
			if downloads.CSR == "" {
				return fmt.Errorf("CSR identity not available")
			}
			if outPath == "" {
				outPath = "JamfProtect-CSR.p12"
			}
			return writeBase64File(downloads.CSR, outPath)
		},
	}

	cmd.Flags().StringVarP(&outPath, "output", "O", "", "Output file path (default: JamfProtect-CSR.p12)")
	return cmd
}

func newProtectDownloadsWebsocketAuthCmd(cliCtx *registry.CLIContext) *cobra.Command {
	var outPath string

	cmd := &cobra.Command{
		Use:   "websocket-auth",
		Short: "Download the websocket authorizer key",
		RunE: func(cmd *cobra.Command, _ []string) error {
			downloads, err := cliCtx.ProtectClient.GetOrganizationDownloads(cmd.Context())
			if err != nil {
				return err
			}
			if downloads.WebsocketAuth == "" {
				return fmt.Errorf("websocket authorizer key not available")
			}
			if outPath == "" {
				outPath = "JamfProtect-WebsocketAuth.p12"
			}
			return writeBase64File(downloads.WebsocketAuth, outPath)
		},
	}

	cmd.Flags().StringVarP(&outPath, "output", "O", "", "Output file path (default: JamfProtect-WebsocketAuth.p12)")
	return cmd
}

// buildProtectPackageURL constructs a download URL for an installer/uninstaller package.
func buildProtectPackageURL(baseURL, packageName, installerUUID string) string {
	if baseURL == "" || installerUUID == "" {
		return ""
	}
	u, err := url.Parse(baseURL)
	if err != nil {
		return ""
	}
	u = u.JoinPath(packageName)
	u.RawQuery = installerUUID
	return u.String()
}

// writeBase64File decodes a base64 string and writes the result to a file.
func writeBase64File(b64 string, path string) error {
	data, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return fmt.Errorf("decoding base64 data: %w", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("writing file: %w", err)
	}
	fmt.Fprintf(os.Stderr, "Saved to %s (%d bytes)\n", path, len(data))
	return nil
}

// downloadProtectFile downloads a file from a URL using the Protect client's auth token.
func downloadProtectFile(ctx context.Context, client registry.ProtectClient, fileURL, outPath string) error {
	token, err := client.AccessToken(ctx)
	if err != nil {
		return fmt.Errorf("getting access token: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "GET", fileURL, nil)
	if err != nil {
		return fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Authorization", token.TokenType+" "+token.AccessToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("downloading file: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download failed: HTTP %d", resp.StatusCode)
	}

	f, err := os.Create(outPath)
	if err != nil {
		return fmt.Errorf("creating output file: %w", err)
	}
	defer func() { _ = f.Close() }()

	n, err := io.Copy(f, resp.Body)
	if err != nil {
		return fmt.Errorf("writing file: %w", err)
	}

	fmt.Fprintf(os.Stderr, "Saved to %s (%d bytes)\n", outPath, n)
	return nil
}

// --- Config Freeze ---

func newProtectConfigFreezeCmd(cliCtx *registry.CLIContext) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config-freeze",
		Short: "Manage configuration freeze",
	}

	cmd.AddCommand(newProtectConfigFreezeGetCmd(cliCtx))
	cmd.AddCommand(newProtectConfigFreezeEnableCmd(cliCtx))
	cmd.AddCommand(newProtectConfigFreezeDisableCmd(cliCtx))

	return cmd
}

func newProtectConfigFreezeGetCmd(cliCtx *registry.CLIContext) *cobra.Command {
	return &cobra.Command{
		Use:   "get",
		Short: "Get config freeze status",
		RunE: func(cmd *cobra.Command, _ []string) error {
			item, err := cliCtx.ProtectClient.GetConfigFreeze(cmd.Context())
			if err != nil {
				return err
			}
			return protect.PrintOne(cliCtx.Output, item)
		},
	}
}

func newProtectConfigFreezeEnableCmd(cliCtx *registry.CLIContext) *cobra.Command {
	return &cobra.Command{
		Use:   "enable",
		Short: "Enable config freeze",
		RunE: func(cmd *cobra.Command, _ []string) error {
			item, err := cliCtx.ProtectClient.UpdateOrganizationConfigFreeze(cmd.Context(), true)
			if err != nil {
				return err
			}
			return protect.PrintOne(cliCtx.Output, item)
		},
	}
}

func newProtectConfigFreezeDisableCmd(cliCtx *registry.CLIContext) *cobra.Command {
	return &cobra.Command{
		Use:   "disable",
		Short: "Disable config freeze",
		RunE: func(cmd *cobra.Command, _ []string) error {
			item, err := cliCtx.ProtectClient.UpdateOrganizationConfigFreeze(cmd.Context(), false)
			if err != nil {
				return err
			}
			return protect.PrintOne(cliCtx.Output, item)
		},
	}
}

// --- Connections ---

func newProtectConnectionsCmd(cliCtx *registry.CLIContext) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "connections",
		Short: "Manage identity provider connections",
	}

	cmd.AddCommand(newProtectConnectionsListCmd(cliCtx))

	return cmd
}

func newProtectConnectionsListCmd(cliCtx *registry.CLIContext) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List all connections",
		RunE: func(cmd *cobra.Command, _ []string) error {
			items, err := cliCtx.ProtectClient.ListConnections(cmd.Context())
			if err != nil {
				return err
			}
			return protect.PrintList(cliCtx.Output, items)
		},
	}
}
